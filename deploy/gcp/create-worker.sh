#!/usr/bin/env bash
# Create the two opencode worker VMs (run once): worker-spot (Spot) and worker-std (standard), both e2-medium COS.
# orchd starts one on demand (runner mode vm); each powers itself off stop-grace-seconds after its last worker
# container exits, or after idle-minutes without one (worker-startup.sh). The first boot fetches secrets and pulls
# the worker image onto the disk; stop the VMs once it is done (printed below) or the idle net does it, so later
# starts only boot + `docker run`. Creates only; never deletes or updates. Existing VMs are reported and skipped.
# Needs the firewall rule allow-iap-ssh-cad (create-vm.sh) and the SA cad-vm@ with secretAccessor on the
# secrets in worker-auth.list (README "worker").
# Env overrides: PROJECT ZONE MACHINE IDLE_MINUTES STOP_GRACE_SECONDS.
set -euo pipefail
PROJECT=${PROJECT:-suggestorder-dev}
ZONE=${ZONE:-us-central1-a}
MACHINE=${MACHINE:-e2-medium}
IDLE_MINUTES=${IDLE_MINUTES:-30}
STOP_GRACE_SECONDS=${STOP_GRACE_SECONDS:-60}
here=$(cd "$(dirname "$0")" && pwd)

run() { printf '+ %s\n' "$*"; "$@"; }

# https://cloud.google.com/compute/docs/instances/create-use-spot : --provisioning-model=SPOT, and
# --instance-termination-action=STOP ("Stop the VM without preserving memory. The VM can be restarted later",
# `gcloud compute instances create --help`) so a preempted worker keeps its disk (cached image) instead of being deleted.
for vm in worker-spot worker-std; do
  if gcloud compute instances describe "$vm" --project "$PROJECT" --zone "$ZONE" >/dev/null 2>&1; then
    echo "instance $vm exists; skipping (startup script: gcloud compute instances add-metadata $vm --zone $ZONE --project $PROJECT --metadata-from-file startup-script=$here/worker-startup.sh)"
    continue
  fi
  spot=()
  [ "$vm" = worker-spot ] && spot=(--provisioning-model=SPOT --instance-termination-action=STOP)
  # pd-balanced: steadier boot/image reads than pd-standard (image ~420MB).
  # Same network/SA/scope setup as create-vm.sh (external IP: no Cloud NAT, pulls from ghcr.io; tag cad = IAP ssh).
  run gcloud compute instances create "$vm" --project "$PROJECT" --zone "$ZONE" \
    --machine-type "$MACHINE" "${spot[@]}" \
    --image-family cos-stable --image-project cos-cloud \
    --boot-disk-size 10GB --boot-disk-type pd-balanced \
    --tags cad --labels app=cad-worker,managed-by=codingagentenv \
    --service-account "cad-vm@$PROJECT.iam.gserviceaccount.com" --scopes cloud-platform \
    --metadata "idle-minutes=$IDLE_MINUTES,stop-grace-seconds=$STOP_GRACE_SECONDS" \
    --metadata-from-file "startup-script=$here/worker-startup.sh,cad-secrets=$here/worker-auth.list"
done
S="--project $PROJECT --zone $ZONE"
cat <<MSG
The first boot pulls the image. When both show "worker: ready" (or after ${IDLE_MINUTES} min the idle net stops them):
  gcloud compute instances get-serial-port-output worker-spot $S | grep 'worker: ready'
  gcloud compute instances get-serial-port-output worker-std $S | grep 'worker: ready'
  gcloud compute instances stop worker-spot worker-std $S
  gcloud compute instances list --project $PROJECT --filter=labels.app=cad-worker --format='table(name,status)'
MSG
