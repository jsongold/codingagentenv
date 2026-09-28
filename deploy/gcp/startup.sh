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
cat >/etc/systemd/system/cad-update.service <<UNIT
[Unit]
Description=Refresh secrets and update service cad to the current digest of $IMAGE (auto-rollback on failure)
After=docker.service

[Service]
Type=oneshot
ExecStartPre=-/usr/bin/docker run --rm --pull always --network host -v "$VOL:/data" --entrypoint /app/bin/fetch-auth $IMAGE
ExecStart=/usr/bin/docker service update --quiet --image $IMAGE cad
ExecStartPost=/usr/bin/docker service inspect cad --format 'cad image={{.Spec.TaskTemplate.ContainerSpec.Image}} update={{if .UpdateStatus}}{{.UpdateStatus.State}}: {{.UpdateStatus.Message}}{{end}}'
ExecStartPost=-/usr/bin/docker image prune -f
UNIT

cat >/etc/systemd/system/cad-update.timer <<UNIT
[Unit]
Description=Check for new secrets and a new cad image every 5 minutes

[Timer]
OnBootSec=2min
OnUnitActiveSec=5min

[Install]
WantedBy=timers.target
UNIT

systemctl daemon-reload
systemctl enable --now cad-update.timer
