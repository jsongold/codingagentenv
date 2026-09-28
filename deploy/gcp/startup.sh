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
UID_CAD=10001 # the image's `cad` user

mkdir -p /var/lib/cad
chown "$UID_CAD:$UID_CAD" /var/lib/cad

# Swarm state survives reboots (/var/lib/docker), so init only once. Advertise the VM's internal IP from the
# metadata server: nothing is published on it (no ports, IAP-only firewall); it just avoids the
# "multiple addresses" ambiguity (docker0 + eth0). https://docs.docker.com/reference/cli/docker/swarm/init/
if [ "$(docker info --format '{{.Swarm.LocalNodeState}}')" != active ]; then
  ip=$(curl -fsS -H 'Metadata-Flavor: Google' \
    http://metadata.google.internal/computeMetadata/v1/instance/network-interfaces/0/ip)
  docker swarm init --advertise-addr "$ip"
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
    --mount type=bind,src=/var/lib/cad,dst=/data \
    --restart-condition any --restart-delay 10s \
    --update-order stop-first --update-failure-action rollback --update-monitor 90s \
    --rollback-order stop-first \
    "$IMAGE"
fi

# Follow the tag: on `service update --image <tag>` the manager re-resolves the tag to its current digest
# ("updates the service tasks to use that digest"), so an unchanged digest is a no-op and a new one rolls out.
# https://docs.docker.com/engine/swarm/services/#update-a-services-image-after-creation
# Without --detach it waits for convergence (or the rollback). It exits 0 even after an automatic rollback
# (observed on Docker 29.8), so the ExecStartPost line is what records the outcome in the journal.
cat > /etc/systemd/system/cad-update.service <<UNIT
[Unit]
Description=Update service cad to the current digest of $IMAGE (auto-rollback on failure)
After=docker.service

[Service]
Type=oneshot
ExecStart=/usr/bin/docker service update --quiet --image $IMAGE cad
ExecStartPost=/usr/bin/docker service inspect cad --format 'cad image={{.Spec.TaskTemplate.ContainerSpec.Image}} update={{if .UpdateStatus}}{{.UpdateStatus.State}}: {{.UpdateStatus.Message}}{{end}}'
ExecStartPost=-/usr/bin/docker image prune -f
UNIT

cat > /etc/systemd/system/cad-update.timer <<UNIT
[Unit]
Description=Check for a new cad image every 5 minutes

[Timer]
OnBootSec=2min
OnUnitActiveSec=5min

[Install]
WantedBy=timers.target
UNIT

systemctl daemon-reload
systemctl enable --now cad-update.timer
