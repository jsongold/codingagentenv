#!/bin/bash
# COS startup script: runs as root on every boot
# (https://cloud.google.com/compute/docs/instances/startup-scripts/linux).
# COS facts (https://cloud.google.com/container-optimized-os/docs/concepts/disks-and-filesystem):
#   /etc is tmpfs, stateless -> the systemd units below are re-created on each boot (that is why they live here);
#   /var (so /var/lib/cad and docker's swarm state in /var/lib/docker) persists for the lifetime of the boot disk.
#
# cad runs as a single-node Docker Swarm service so that updates are health-gated and roll back on their own:
#   a task counts as running only after the image HEALTHCHECK passes (Dockerfile), and
#   --update-failure-action rollback reverts to the previous spec when the new task fails within --update-monitor.
#   https://docs.docker.com/reference/cli/docker/service/create/
#   https://docs.docker.com/reference/cli/docker/service/update/
set -euo pipefail
IMAGE=ghcr.io/jsongold/codingagentenv/cad:main # pin: change to :sha-<7 hex> here (see README)
UID_CAD=${CAD_UID:-10001} # the image's `cad` user
VOL=${CAD_VOL:-/var/lib/cad}
MD=${CAD_MD:-http://metadata.google.internal/computeMetadata/v1}

# Bounded (--max-time) so a stalled endpoint cannot block boot or the update timer.
md() { curl -fsS --max-time 10 -H 'Metadata-Flavor: Google' "$MD/$1"; }

# Secrets from Secret Manager -> files on the volume. The fetch runs in the cad image (deploy/fetch-auth.sh ->
# /app/bin/fetch-auth), which has jq/curl/base64; COS has no package manager, so no jq
# (https://cloud.google.com/container-optimized-os/docs/concepts/features-and-benefits).
# --network host: the container shares the VM's network namespace
# (https://docs.docker.com/engine/network/drivers/host/), so it reaches the metadata server (token, cad-secrets,
# project-id) like a process on the VM; GKE documents the same for host-network Pods ("GKE automatically routes
# requests from these Pods to the Compute Engine metadata server",
# https://cloud.google.com/kubernetes-engine/docs/concepts/workload-identity).
# --pull always: the local :main tag is not what the service tracks (the service pins a digest), so pull it here to
# run the current fetch-auth; if the registry is unreachable the fetch is skipped and the files are kept.
# The container runs as uid 10001 (the image's `cad` user), which owns $VOL, so written files are cad's.
# Keep in sync with ExecStartPre in cad-update.service below (there "$VOL:/data" is quoted for systemd).
FETCH=(docker run --rm --pull always --network host -v "$VOL:/data" --entrypoint /app/bin/fetch-auth "$IMAGE")

# Swarm refuses to init while live-restore is on ("--live-restore daemon configuration is incompatible with swarm
# mode"), and COS ships /etc/docker/daemon.json with "live-restore": true. /etc is tmpfs; COS's docker.service has
# `ExecStartPre=... cp -f /var/lib/docker/daemon.json /etc/docker/daemon.json` when that file exists (observed in
# the unit on the VM, not documented), so the persistent override goes to /var/lib/docker/daemon.json. Editing the
# default daemon.json rather than adding flags follows
# https://cloud.google.com/container-optimized-os/docs/how-to/run-container-instance (daemon.json vs flags).
live_restore_off() { # <src daemon.json> <dst>
  sed 's/"live-restore": *true/"live-restore": false/' "$1" >"$2.tmp" &&
    grep -q '"live-restore": *false' "$2.tmp" && mv -f "$2.tmp" "$2"
}

[ "${CAD_STARTUP_LIB:-}" = 1 ] && return 0 # sourced by the local simulation: functions only

mkdir -p "$VOL"
chown "$UID_CAD:$UID_CAD" "$VOL"
"${FETCH[@]}" || echo "secrets: fetch failed; continuing"

if [ "$(docker info --format '{{.LiveRestoreEnabled}}')" = true ]; then
  live_restore_off /etc/docker/daemon.json /var/lib/docker/daemon.json
  systemctl restart docker
  for _ in $(seq 60); do docker info >/dev/null 2>&1 && break; sleep 1; done
fi

# Swarm state survives reboots (/var/lib/docker), so init only once. Advertise the VM's internal IP from the
# metadata server: nothing is published on it (no ports, IAP-only firewall); it just avoids the
# "multiple addresses" ambiguity (docker0 + eth0). https://docs.docker.com/reference/cli/docker/swarm/init/
if [ "$(docker info --format '{{.Swarm.LocalNodeState}}')" != active ]; then
  docker swarm init --advertise-addr "$(md instance/network-interfaces/0/ip)"
fi

# --network host + cad's default 127.0.0.1:7878: the API is reachable only on the VM's loopback (use an IAP
# SSH tunnel); cad refuses non-loopback binds without CAD_TOKEN anyway.
# stop-first (update and rollback): with host networking only one container can bind 127.0.0.1:7878
# (https://docs.docker.com/engine/network/drivers/host/), so start-first would leave the new task unable to
# listen -> never healthy -> a spurious rollback. The cost is a short gap while the new task starts.
# --update-monitor 90s: a task that exits or turns unhealthy within 90s fails the update -> rollback.
# Health flags come from the image HEALTHCHECK.
if ! docker service inspect cad >/dev/null 2>&1; then
  docker service create --detach --name cad --replicas 1 \
    --network host \
    --mount type=bind,src="$VOL",dst=/data \
    --restart-condition any --restart-delay 10s \
    --update-order stop-first --update-failure-action rollback --update-monitor 90s \
    --rollback-order stop-first \
    "$IMAGE"
fi

# The timer re-fetches secrets on each run, so a new secret version reaches the volume within 5 minutes.

# Follow the tag: on `service update --image <tag>` the manager re-resolves the tag to its current digest
# ("updates the service tasks to use that digest"), so an unchanged digest is a no-op and a new one rolls out.
# https://docs.docker.com/engine/swarm/services/#update-a-services-image-after-creation
# Without --detach it waits for convergence (or the rollback). It exits 0 even after an automatic rollback
# (observed on Docker 29.8), so the ExecStartPost line is what records the outcome in the journal.
# Serialized with orchd-sleep.service (below): the update replaces the cad container stop-first, which would kill
# an orchd dispatch running in it after it has claimed an issue. So the second ExecStartPre waits (every 5s, at
# most 600s) while orchd-sleep.service is running, and orchd-sleep.service skips its run while this unit runs
# (its ExecCondition). Both units are Type=oneshot, whose state while running is "activating" (systemctl
# is-active then exits non-zero), so the state string is compared. The wait is prefixed with "-": after 600s the
# update goes ahead anyway rather than never updating. In the unit, $$ is systemd's literal $ and \$ keeps this
# heredoc from expanding it (as in orchd-sleep.service).
cat >/etc/systemd/system/cad-update.service <<UNIT
[Unit]
Description=Refresh secrets and update service cad to the current digest of $IMAGE (auto-rollback on failure)
After=docker.service

[Service]
Type=oneshot
ExecStartPre=-/usr/bin/docker run --rm --pull always --network host -v "$VOL:/data" --entrypoint /app/bin/fetch-auth $IMAGE
ExecStartPre=-/bin/sh -c 'n=0; while case "\$\$(systemctl is-active orchd-sleep.service)" in activating|deactivating) true;; *) false;; esac; do [ \$\$n -lt 120 ] || { echo "cad-update: orchd-sleep still running after 600s; updating anyway"; exit 1; }; n=\$\$((n+1)); sleep 5; done'
ExecStart=/usr/bin/docker service update --quiet --image $IMAGE cad
ExecStartPost=/usr/bin/docker service inspect cad --format 'cad image={{.Spec.TaskTemplate.ContainerSpec.Image}} update={{if .UpdateStatus}}{{.UpdateStatus.State}}: {{.UpdateStatus.Message}}{{end}}'
ExecStartPost=-/usr/bin/docker image prune -f
UNIT

cat >/etc/systemd/system/cad-update.timer <<UNIT
[Unit]
Description=Check for new secrets and a new cad image every 5 minutes

[Timer]
OnBootSec=2min
OnCalendar=*:00/5

[Install]
WantedBy=timers.target
UNIT

# Sleep loop (ADR-0015): every hour (:03) run `orchd place --mode sleep | orchd dispatch --placement -` inside
# the cad container. The timer itself is the sleep switch (README: `systemctl start`/`stop orchd-sleep.timer`);
# it is (re)created here on every boot but never enabled/started, so a reboot leaves sleep off until started
# again. place's own exit codes (3 deferred, 4 no rule fits) do not reach systemd through the pipe -- dispatch's
# exit is what the unit sees -- so SuccessExitStatus=3 below only covers this script's own container-lookup
# defer, not a defer from place. The task's container is cad.1.<task id>, so it is looked up by the swarm
# service label (as in the README); no running task (mid update/rollback) is also a defer (exit 3). docker exec
# runs as the image's user (cad) with its env (HOME=/data, ORCHD_STATE_DIR, ...). ExecCondition skips the run
# (exit 1 = condition not met, not a failure; systemd.service "ExecCondition=") while cad-update.service is
# running, since that replaces the container; see cad-update.service for the other half. In the unit, $$ is
# systemd's literal $ (systemd.service "Command lines"); \$ keeps this heredoc from expanding it.
cat >/etc/systemd/system/orchd-sleep.service <<UNIT
[Unit]
Description=Sleep loop: orchd place --mode sleep | orchd dispatch --placement - in the cad container
After=docker.service

[Service]
Type=oneshot
SuccessExitStatus=3
ExecCondition=/bin/sh -c 'case "\$\$(systemctl is-active cad-update.service)" in activating|deactivating) echo "orchd-sleep: cad-update running; skipped"; exit 1;; esac'
ExecStart=/bin/sh -c 'c=\$\$(/usr/bin/docker ps -q -f label=com.docker.swarm.service.name=cad -f status=running | head -n 1); [ -n "\$\$c" ] || { echo "orchd-sleep: no running cad container; deferred"; exit 3; }; exec /usr/bin/docker exec "\$\$c" /bin/sh -c "/app/orchd/bin/orchd place --mode sleep | /app/orchd/bin/orchd dispatch --placement -"'
UNIT

cat >/etc/systemd/system/orchd-sleep.timer <<UNIT
[Unit]
Description=Run orchd place --mode sleep | orchd dispatch every hour while started (sleep loop, ADR-0015)

[Timer]
# hourly at :03, between cad-update runs (*:00/5, ~1.5 min each) so ExecCondition does not skip it
OnCalendar=*:03

[Install]
WantedBy=timers.target
UNIT

systemctl daemon-reload
systemctl enable --now cad-update.timer
# orchd-sleep.timer is intentionally not enabled/started here: the timer itself is the sleep on/off switch
# (README "sleep loop"), started by the owner before sleeping and stopped on waking.
