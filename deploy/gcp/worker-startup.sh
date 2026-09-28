#!/bin/bash
# COS startup script of the opencode worker VMs (create-worker.sh); runs as root on every boot
# (https://cloud.google.com/compute/docs/instances/startup-scripts/linux). No swarm here (one-shot containers
# started on boot from the task in the metadata), so COS's default live-restore stays as it is.
#   1. secrets -> /var/lib/cad via fetch-auth in the worker image (list = metadata cad-secrets = worker-auth.list)
#   2. the task: `orchd dispatch` sets metadata worker-task via the Compute API, then starts the VM; this script
#      reads it and `docker run`s the worker once (no ssh). A RUNNING VM is busy for orchd (exit 5).
#   3. docker pull the worker image, cached on the boot disk (/var persists,
#      https://cloud.google.com/container-optimized-os/docs/concepts/disks-and-filesystem). With a cached image the
#      pull runs after the task started: the task runs the cached image at once and the pull (only changed layers)
#      serves the next start. Only the first boot (no cached image) pulls before.
#   4. self-stop, event driven: worker-stop.service follows `docker events` (container die); when an
#      opencode-worker-* container dies and, after stop-grace-seconds (metadata, default 10), none is running,
#      it powers the VM off. Safety net: worker-idle.timer powers
#      it off when no opencode-worker container has run for idle-minutes (metadata, default 30), e.g. a dispatch
#      that never started its container, or the first boot after create-worker.sh.
#      A shutdown from the guest OS is a stop: the instance becomes TERMINATED
#      (https://cloud.google.com/compute/docs/instances/stop-start-instance "stop ... from within its guest OS",
#      https://cloud.google.com/compute/docs/instances/instance-life-cycle), and TERMINATED is not billed for
#      vCPU/memory; the disk and external IP are ("regardless of the compute instance state").
set -euo pipefail
IMAGE=ghcr.io/jsongold/codingagentenv/opencode-worker:main
VOL=/var/lib/cad
MD=http://metadata.google.internal/computeMetadata/v1

md() { curl -fsS --max-time 10 -H 'Metadata-Flavor: Google' "$MD/$1"; }

num() { # <metadata key> <default>: a non-negative integer
  local v
  v=$(md "instance/attributes/$1" 2>/dev/null) || v=
  case $v in '' | *[!0-9]*) v=$2 ;; esac
  echo "$v"
}
grace=$(num stop-grace-seconds 10)
idle=$(num idle-minutes 30)

mkdir -p "$VOL"
chown 10001:10001 "$VOL" # the image's worker user; fetch-auth writes as it
pull() { docker pull -q "$IMAGE" || echo "worker: pull failed; keeping the cached image"; }
cached=1
docker image inspect "$IMAGE" >/dev/null 2>&1 || { cached=0; pull; }
# --network host: reach the metadata server like the VM does (see startup.sh).
docker run --rm --network host -v "$VOL:/data" --entrypoint /app/bin/fetch-auth "$IMAGE" ||
  echo "secrets: fetch failed; continuing"

# /etc is tmpfs on COS (units are recreated each boot) and /run is cleared on boot. The scripts run via
# `bash <file>` so they need no exec bit.
running() { docker ps -q --filter name=opencode-worker- | grep -q .; }
declare -f running >/etc/worker-lib.sh

# docker events streams live events until killed; filters of different keys are ANDed
# (https://docs.docker.com/reference/cli/docker/system/events/). The container filter wants an exact name, so the
# opencode-worker- prefix is matched on the event's name attribute. Events arriving during the grace sleep queue
# in the pipe; each is re-checked, and `running` decides. Restart=always: the stream ends when dockerd restarts.
# --since this boot replays past events, so a worker that died before the stream subscribed is not missed.
cat >/etc/worker-stop.sh <<EOF
. /etc/worker-lib.sh
docker events --since $(date +%s) --filter type=container --filter event=die --format '{{.Actor.Attributes.name}}' |
  while read -r name; do
    case \$name in opencode-worker-*) ;; *) continue ;; esac
    sleep $grace
    running && continue
    echo "worker: \$name exited, none running after ${grace}s; shutting down"
    shutdown -h now
  done
EOF
# The idle clock starts at the first check after boot.
cat >/etc/worker-idle.sh <<EOF
. /etc/worker-lib.sh
since=/run/worker-idle-since
if running || [ ! -f "\$since" ]; then
  date +%s >"\$since"
elif [ \$((\$(date +%s) - \$(cat "\$since"))) -ge $((idle * 60)) ]; then
  echo "worker: no opencode-worker container for $idle min; shutting down"
  shutdown -h now
fi
EOF
cat >/etc/systemd/system/worker-stop.service <<'UNIT'
[Unit]
Description=Power off the worker VM when its last opencode-worker container exits
After=docker.service
Requires=docker.service

[Service]
ExecStart=/bin/bash /etc/worker-stop.sh
Restart=always
RestartSec=5
UNIT
cat >/etc/systemd/system/worker-idle.service <<'UNIT'
[Unit]
Description=Safety net: power off the worker VM after idle-minutes without an opencode-worker container
After=docker.service

[Service]
Type=oneshot
ExecStart=/bin/bash /etc/worker-idle.sh
UNIT
cat >/etc/systemd/system/worker-idle.timer <<'UNIT'
[Unit]
Description=Check worker idleness every minute

[Timer]
OnBootSec=1min
OnUnitActiveSec=1min

[Install]
WantedBy=timers.target
UNIT
systemctl daemon-reload
systemctl enable --now worker-idle.timer
systemctl start worker-stop.service
# The task `orchd dispatch` put in the metadata before starting the VM: "<id> <issue> <repo> <model> <image>".
# The metadata server is read-only for the VM, so the task is marked consumed on the disk (its id), not cleared:
# a reboot (manual start, reset) does not run it again.
task=$(md instance/attributes/worker-task 2>/dev/null) || task=
if [ -z "$task" ]; then
  echo "worker: no task"
else
  read -r id issue repo model image <<<"$task"
  if [ "$id" = "$(cat "$VOL/task-done" 2>/dev/null)" ]; then
    echo "worker: task $id already ran; skipping"
  else
    echo "$id" >"$VOL/task-done"
    echo "worker: task $id: issue $issue repo $repo model $model"
    docker run -d --rm --name "opencode-worker-$issue" -v "$VOL:/data" \
      -e ISSUE="$issue" -e REPO="$repo" -e MODEL="$model" "${image:-$IMAGE}" || {
      # Never started: keep it retryable on the next boot, and stop now (TERMINATED without a PR = re-queue).
      rm -f "$VOL/task-done"
      echo "worker: docker run failed; shutting down"
      shutdown -h now
    }
  fi
fi
echo "worker: ready (stop-grace-seconds=$grace, idle-minutes=$idle)"
[ "$cached" = 0 ] || pull
