#!/usr/bin/env bash
# Create 1-3 opencode worker VMs (run once), e2-medium COS each. Names rotate spot/std/spot so the default (N=2)
# reproduces the original worker-spot + worker-std unchanged; a 3rd worker adds a second Spot VM (cheaper) as
# worker-spot-2 rather than a second standard one. orchd (runner mode vm) picks any TERMINATED VM of a kind from
# runner.instance, a comma-separated list (orchd/README.md "vm runner"); each VM powers itself off
# stop-grace-seconds after its last worker container exits, or after idle-minutes without one (worker-startup.sh).
# The first boot fetches secrets and pulls the worker image onto the disk; stop the VMs once it is done (printed
# below) or the idle net does it, so later starts only boot + `docker run`. Creates only; never deletes or updates.
# Existing VMs are reported and skipped, so raising N later (up to 3) only creates the new ones.
# Needs the firewall rule allow-iap-ssh-cad (create-vm.sh) and the SA cad-vm@ with secretAccessor on the
# secrets in worker-auth.list (README "worker").
# Usage: create-worker.sh [N]   (N = 1, 2 or 3; default 2, or env N)
# Env overrides: PROJECT ZONE MACHINE IDLE_MINUTES STOP_GRACE_SECONDS N.
set -euo pipefail
PROJECT=${PROJECT:-suggestorder-dev}
ZONE=${ZONE:-us-central1-a}
MACHINE=${MACHINE:-e2-medium}
IDLE_MINUTES=${IDLE_MINUTES:-30}
STOP_GRACE_SECONDS=${STOP_GRACE_SECONDS:-10}
N=${1:-${N:-2}}
case $N in
1 | 2 | 3) ;;
*)
  echo "N must be 1, 2 or 3 (got $N)" >&2
  exit 2
  ;;
esac
here=$(cd "$(dirname "$0")" && pwd)

run() { printf '+ %s\n' "$*"; "$@"; }

# https://cloud.google.com/compute/docs/instances/create-use-spot : --provisioning-model=SPOT, and
# --instance-termination-action=STOP ("Stop the VM without preserving memory. The VM can be restarted later",
# `gcloud compute instances create --help`) so a preempted worker keeps its disk (cached image) instead of being deleted.
names=(worker-spot worker-std worker-spot-2)
created=()
for ((i = 0; i < N; i++)); do
  vm=${names[i]}
  created+=("$vm")
  if gcloud compute instances describe "$vm" --project "$PROJECT" --zone "$ZONE" >/dev/null 2>&1; then
    echo "instance $vm exists; skipping (startup script: gcloud compute instances add-metadata $vm --zone $ZONE --project $PROJECT --metadata-from-file startup-script=$here/worker-startup.sh)"
    continue
  fi
  spot=()
  [[ $vm == worker-spot* ]] && spot=(--provisioning-model=SPOT --instance-termination-action=STOP)
  # pd-balanced: steadier boot/image reads than pd-standard (image ~420MB).
  # Same network/SA/scope setup as create-vm.sh (external IP: no Cloud NAT, pulls from ghcr.io; tag cad = IAP ssh).
  # google-logging-enabled: Cloud Logging via COS's built-in agent (Ops Agent is not supported on COS).
  run gcloud compute instances create "$vm" --project "$PROJECT" --zone "$ZONE" \
    --machine-type "$MACHINE" "${spot[@]}" \
    --image-family cos-stable --image-project cos-cloud \
    --boot-disk-size 10GB --boot-disk-type pd-balanced \
    --tags cad --labels app=cad-worker,managed-by=codingagentenv \
    --service-account "cad-vm@$PROJECT.iam.gserviceaccount.com" --scopes cloud-platform \
    --metadata "idle-minutes=$IDLE_MINUTES,stop-grace-seconds=$STOP_GRACE_SECONDS,google-logging-enabled=true" \
    --metadata-from-file "startup-script=$here/worker-startup.sh,cad-secrets=$here/worker-auth.list"
done
S="--project $PROJECT --zone $ZONE"
echo
echo "The first boot pulls the image. When all show \"worker: ready\" (or after ${IDLE_MINUTES} min the idle net stops them):"
for vm in "${created[@]}"; do
  echo "  gcloud compute instances get-serial-port-output $vm $S | grep 'worker: ready'"
done
echo "  gcloud compute instances stop ${created[*]} $S"
echo "  gcloud compute instances list --project $PROJECT --filter=labels.app=cad-worker --format='table(name,status)'"
if [ "$N" -gt 2 ]; then
  echo
  echo "orchd/policy.json's opencode@gce-spot runner.instance is a comma-separated list of same-kind VMs;" \
    "add ${names[2]} to it (e.g. \"worker-spot,worker-spot-2\") so orchd can pick either."
fi
