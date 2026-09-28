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
SM=${CAD_SM:-https://secretmanager.googleapis.com/v1}

# Bounded (--max-time) so a stalled endpoint cannot block boot or the update timer.
md() { curl -fsS --max-time 10 -H 'Metadata-Flavor: Google' "$MD/$1"; }

# Secrets from Secret Manager -> files on the volume. The list is the instance metadata key `cad-secrets`
# (= deploy/gcp/secrets.list, set by create-vm.sh): lines "<service> <id>" -> secret cad-<service>-<id>.
# Never prints a secret or the token; on any failure it logs and keeps the existing file.
# - token: metadata server, attached service account (cad-vm@) with the cloud-platform scope
#   https://cloud.google.com/compute/docs/access/authenticate-workloads
# - GET v1/projects/*/secrets/*/versions/*:access needs secretmanager.versions.access (roles/secretmanager.secretAccessor)
#   and returns {"name":..., "payload": {"data": "<base64>", "dataCrc32c": ...}}
#   https://cloud.google.com/secret-manager/docs/reference/rest/v1/projects.secrets.versions/access
#   https://cloud.google.com/secret-manager/docs/reference/rest/v1/SecretPayload
# Parsing: COS has no package manager (https://cloud.google.com/container-optimized-os/docs/concepts/features-and-benefits),
# so no jq. sed is enough here because both values have fixed alphabets that cannot contain `"` or escapes:
# the OAuth token and base64 ([A-Za-z0-9+/=]); anything else fails the match and the file is kept.
fetch_secrets() {
  local list project token service id name dir file body data tmp
  list=$(md instance/attributes/cad-secrets 2>/dev/null) || { echo "secrets: no metadata key cad-secrets; skipping"; return 0; }
  project=$(md project/project-id) || { echo "secrets: project-id lookup failed; keeping files"; return 0; }
  token=$(md instance/service-accounts/default/token | sed -n 's/.*"access_token" *: *"\([^"]*\)".*/\1/p') || true
  [ -n "$token" ] || { echo "secrets: no access token; keeping files"; return 0; }
  while read -r service id _; do
    case $service in '' | '#'*) continue ;; esac
    name=cad-$service-$id
    case $id in '' | *[!A-Za-z0-9_-]*) echo "$name: bad id; skipping"; continue ;; esac
    case $service in
      opencode) dir=$VOL/.aienv/.store/$id/opencode file=auth.json ;; # cad/opencode_usage.go: $HOME/.aienv/.store/<id>/opencode/auth.json
      *) echo "$name: unknown service; skipping"; continue ;;
    esac
    # Token via stdin (curl -H @-), not argv, so it does not show up in ps.
    body=$(printf 'Authorization: Bearer %s\n' "$token" |
      curl -fsS --max-time 30 -H @- "$SM/projects/$project/secrets/$name/versions/latest:access") ||
      { echo "$name: access failed; keeping existing file"; continue; }
    data=$(printf %s "$body" | tr -d '\n' | sed -n 's|.*"data" *: *"\([A-Za-z0-9+/=]*\)".*|\1|p')
    [ -n "$data" ] || { echo "$name: no payload.data; keeping existing file"; continue; }
    { mkdir -p "$dir" && chown "$UID_CAD:$UID_CAD" "$VOL/.aienv" "$VOL/.aienv/.store" "$dir/.." "$dir"; } || continue
    # Same-dir temp (mktemp is 0600) + rename = atomic replace; readers never see a partial file.
    tmp=$(mktemp "$dir/.$file.XXXXXX") || continue
    if printf %s "$data" | base64 -d >"$tmp" 2>/dev/null && [ -s "$tmp" ] &&
      chmod 600 "$tmp" && chown "$UID_CAD:$UID_CAD" "$tmp" && mv -f "$tmp" "$dir/$file"; then
      echo "$name: wrote $dir/$file"
    else
      rm -f "$tmp"
      echo "$name: decode/write failed; keeping existing file"
    fi
  done <<<"$list"
}

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
fetch_secrets || echo "secrets: fetch failed; continuing"

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
# The script is regenerated from the functions above on every boot (/etc is tmpfs).
{
  echo '#!/bin/bash'
  declare -p UID_CAD VOL MD SM
  declare -f md fetch_secrets
  echo fetch_secrets
} >/etc/cad-fetch-secrets.sh
chmod 700 /etc/cad-fetch-secrets.sh

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
ExecStartPre=-/bin/bash /etc/cad-fetch-secrets.sh
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
