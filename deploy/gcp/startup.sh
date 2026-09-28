#!/bin/bash
# COS startup script: runs as root on every boot
# (https://cloud.google.com/compute/docs/instances/startup-scripts/linux).
# COS facts (https://cloud.google.com/container-optimized-os/docs/concepts/disks-and-filesystem):
#   /etc is tmpfs, stateless -> the systemd units below are re-created on each boot (that is why they live here);
#   /var (so /var/lib/cad) is on the stateful partition and persists for the lifetime of the boot disk.
# Running containers from a startup script / systemd unit:
#   https://cloud.google.com/container-optimized-os/docs/how-to/run-container-instance
set -euo pipefail
IMAGE=ghcr.io/jsongold/codingagentenv/cad:main
UID_CAD=10001 # the image's `cad` user

mkdir -p /var/lib/cad
chown "$UID_CAD:$UID_CAD" /var/lib/cad

# --network host + cad's default 127.0.0.1:7878: the API is reachable only on the VM's loopback
# (use an IAP SSH tunnel), and cad refuses non-loopback binds without CAD_TOKEN anyway.
cat > /etc/systemd/system/cad.service <<UNIT
[Unit]
Description=cad (codingagentenv)
Wants=network-online.target docker.service
After=network-online.target docker.service

[Service]
ExecStartPre=-/usr/bin/docker rm -f cad
ExecStart=/usr/bin/docker run --rm --name cad --network host -v /var/lib/cad:/data $IMAGE
ExecStop=/usr/bin/docker stop cad
Restart=always
RestartSec=10

[Install]
WantedBy=multi-user.target
UNIT

# Follow main: pull every 5 min, restart cad only when the image ID changed.
cat > /etc/systemd/system/cad-update.service <<UNIT
[Unit]
Description=Pull $IMAGE and restart cad if it changed
After=docker.service

[Service]
Type=oneshot
ExecStart=/bin/bash -c 'old=\$(docker image inspect -f "{{.Id}}" $IMAGE 2>/dev/null || true); docker pull -q $IMAGE >/dev/null; new=\$(docker image inspect -f "{{.Id}}" $IMAGE); if [ "\$old" != "\$new" ]; then echo "cad image \$old -> \$new"; systemctl restart cad; docker image prune -f >/dev/null; fi'
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
docker pull -q "$IMAGE" || true # cad.service retries if ghcr.io is not reachable yet
systemctl enable --now cad.service cad-update.timer
