#!/bin/bash
# Secret Manager -> auth files on the volume. Runs inside the cad image as /app/bin/fetch-auth (the image has
# jq/curl/base64; COS has no jq: https://cloud.google.com/container-optimized-os/docs/concepts/features-and-benefits),
# started by deploy/gcp/startup.sh at boot and by cad-update.timer:
#   docker run --rm --network host -v /var/lib/cad:/data --entrypoint /app/bin/fetch-auth <image>
# The list is the file CAD_SECRETS_LIST if set, else the instance metadata key `cad-secrets` (= deploy/gcp/secrets.list,
# set by create-vm.sh): lines "<service> <id>" -> secret cad-<service>-<id>.
# Never prints a secret or the token; on any failure it logs and keeps the existing file. Always exits 0.
# - token: metadata server, attached service account (cad-vm@) with the cloud-platform scope
#   https://cloud.google.com/compute/docs/access/authenticate-workloads
# - GET v1/projects/*/secrets/*/versions/*:access needs secretmanager.versions.access (roles/secretmanager.secretAccessor)
#   and returns {"name":..., "payload": {"data": "<base64>", "dataCrc32c": ...}}
#   https://cloud.google.com/secret-manager/docs/reference/rest/v1/projects.secrets.versions/access
# Files are written as the container user (uid 10001 = cad), which owns the volume.
set -uo pipefail
VOL=${CAD_VOL:-/data}
MD=${CAD_MD:-http://metadata.google.internal/computeMetadata/v1}
SM=${CAD_SM:-https://secretmanager.googleapis.com/v1}

# Bounded (--max-time) so a stalled endpoint cannot block boot or the update timer.
md() { curl -fsS --max-time 10 -H 'Metadata-Flavor: Google' "$MD/$1"; }

if [ -n "${CAD_SECRETS_LIST:-}" ]; then
  list=$(cat "$CAD_SECRETS_LIST") || { echo "secrets: cannot read the list file; skipping"; exit 0; }
else
  list=$(md instance/attributes/cad-secrets 2>/dev/null) || { echo "secrets: no metadata key cad-secrets; skipping"; exit 0; }
fi
project=$(md project/project-id) || { echo "secrets: project-id lookup failed; keeping files"; exit 0; }
token=$(md instance/service-accounts/default/token | jq -r '.access_token // empty' 2>/dev/null) || token=
[ -n "$token" ] || { echo "secrets: no access token; keeping files"; exit 0; }

while read -r service id _; do
  case $service in '' | '#'*) continue ;; esac
  name=cad-$service-$id
  case $id in '' | *[!A-Za-z0-9_-]*) echo "$name: bad id; skipping"; continue ;; esac
  case $service in
    opencode) dir=$VOL/.aienv/.store/$id/opencode file=auth.json ;; # cad/opencode_usage.go: $HOME/.aienv/.store/<id>/opencode/auth.json
    github) dir=$VOL/.aienv/.store/$id/github file=token ;; # deploy/worker-run.sh reads it as GH_TOKEN
    *) echo "$name: unknown service; skipping"; continue ;;
  esac
  # Token via stdin (curl -H @-), not argv, so it does not show up in ps.
  body=$(printf 'Authorization: Bearer %s\n' "$token" |
    curl -fsS --max-time 30 -H @- "$SM/projects/$project/secrets/$name/versions/latest:access") ||
    { echo "$name: access failed; keeping existing file"; continue; }
  mkdir -p "$dir" || { echo "$name: mkdir failed; keeping existing file"; continue; }
  # Same-dir temp (mktemp is 0600) + rename = atomic replace; readers never see a partial file.
  tmp=$(mktemp "$dir/.$file.XXXXXX") || { echo "$name: mktemp failed; keeping existing file"; continue; }
  if printf %s "$body" | jq -er '.payload.data' 2>/dev/null | base64 -d >"$tmp" 2>/dev/null && [ -s "$tmp" ] &&
    chmod 600 "$tmp" && mv -f "$tmp" "$dir/$file"; then
    echo "$name: wrote $dir/$file"
  else
    rm -f "$tmp"
    echo "$name: decode/write failed; keeping existing file"
  fi
done <<<"$list"
exit 0
