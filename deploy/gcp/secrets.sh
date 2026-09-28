#!/usr/bin/env bash
# Owner runs this on the Mac. Sets up Secret Manager for the cad VM; idempotent; never prints a secret value.
#   1. enables secretmanager.googleapis.com
#   2. creates the VM's service account cad-vm@ if missing (it gets no project-wide role)
#   3. per line "<service> <id>" of secrets.list: creates secret cad-<service>-<id> if missing, adds a version
#      from the local file when the secret has no enabled version (or ROTATE=1), and grants
#      roles/secretmanager.secretAccessor on that secret only to cad-vm@
# Env overrides: PROJECT, ROTATE=1 (always add a new version from the local file).
# https://cloud.google.com/secret-manager/docs/access-control
# https://cloud.google.com/sdk/gcloud/reference/secrets/create
# https://cloud.google.com/sdk/gcloud/reference/secrets/versions/add
# https://cloud.google.com/sdk/gcloud/reference/secrets/add-iam-policy-binding  (--condition None = unconditional)
# https://cloud.google.com/sdk/gcloud/reference/iam/service-accounts/create
set -euo pipefail
PROJECT=${PROJECT:-suggestorder-dev}
SA=cad-vm@$PROJECT.iam.gserviceaccount.com
here=$(cd "$(dirname "$0")" && pwd)

run() { printf '+ %s\n' "$*"; "$@"; }

run gcloud services enable secretmanager.googleapis.com --project "$PROJECT"

if gcloud iam service-accounts describe "$SA" --project "$PROJECT" >/dev/null 2>&1; then
  echo "service account $SA exists; skipping"
else
  run gcloud iam service-accounts create cad-vm --project "$PROJECT" --display-name "cad VM (Secret Manager access)"
fi

grep -v '^[[:space:]]*\(#\|$\)' "$here/secrets.list" | while read -r service id _; do
  name=cad-$service-$id
  case $service in
    opencode) src=$HOME/.aienv/.store/$id/opencode/auth.json ;;
    *) echo "$name: unknown service $service" >&2; exit 1 ;;
  esac
  if gcloud secrets describe "$name" --project "$PROJECT" >/dev/null 2>&1; then
    echo "secret $name exists; skipping create"
  else
    run gcloud secrets create "$name" --project "$PROJECT" --replication-policy automatic --labels app=cad
  fi
  if [ "${ROTATE:-}" = 1 ] || [ -z "$(gcloud secrets versions list "$name" --project "$PROJECT" \
    --filter state=ENABLED --limit 1 --format 'value(name)')" ]; then
    [ -s "$src" ] || { echo "$name: $src missing or empty" >&2; exit 1; }
    run gcloud secrets versions add "$name" --project "$PROJECT" --data-file "$src" --format 'value(name)'
  else
    echo "secret $name has an enabled version; skipping upload (ROTATE=1 to add one)"
  fi
  run gcloud secrets add-iam-policy-binding "$name" --project "$PROJECT" \
    --member "serviceAccount:$SA" --role roles/secretmanager.secretAccessor --condition None --format none
done
