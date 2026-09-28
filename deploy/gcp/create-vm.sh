#!/usr/bin/env bash
# Create the cad VM (COS + startup-script). Creates only; never deletes or updates.
# Re-running is safe: existing resources are reported and skipped.
# Env overrides: PROJECT ZONE VM MACHINE.
set -euo pipefail
PROJECT=${PROJECT:-suggestorder-dev}
ZONE=${ZONE:-us-central1-a}
VM=${VM:-cad-2}
MACHINE=${MACHINE:-e2-micro}
here=$(cd "$(dirname "$0")" && pwd)

run() { printf '+ %s\n' "$*"; "$@"; }

if gcloud compute firewall-rules describe allow-iap-ssh-cad --project "$PROJECT" >/dev/null 2>&1; then
  echo "firewall rule allow-iap-ssh-cad exists; skipping"
else
  # IAP TCP forwarding range only (https://cloud.google.com/iap/docs/using-tcp-forwarding).
  run gcloud compute firewall-rules create allow-iap-ssh-cad --project "$PROJECT" \
    --network default --direction INGRESS --action allow --rules tcp:22 \
    --source-ranges 35.235.240.0/20 --target-tags cad
fi

if gcloud compute instances describe "$VM" --project "$PROJECT" --zone "$ZONE" >/dev/null 2>&1; then
  echo "instance $VM exists; skipping (to change the startup script: gcloud compute instances add-metadata $VM --zone $ZONE --project $PROJECT --metadata-from-file startup-script=$here/startup.sh)"
else
  # External IP (default ephemeral) because there is no Cloud NAT: the VM pulls from ghcr.io.
  run gcloud compute instances create "$VM" --project "$PROJECT" --zone "$ZONE" \
    --machine-type "$MACHINE" \
    --image-family cos-stable --image-project cos-cloud \
    --boot-disk-size 10GB \
    --tags cad --labels app=cad,managed-by=codingagentenv \
    --metadata-from-file startup-script="$here/startup.sh"
fi
