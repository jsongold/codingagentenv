#!/usr/bin/env bash
# Copy opencode auth.json (Mac aienv store) to the VM volume without printing it, then print how
# to log Claude in. Usage: deploy/gcp/secrets.sh [opencode-id] [claude-id ...]
#   defaults: opencode 996c87ae; claude a12e00a7 b1c8ef41 (see cad/config.json "agents").
# Env overrides: PROJECT ZONE VM.
set -euo pipefail
PROJECT=${PROJECT:-suggestorder-dev}
ZONE=${ZONE:-us-central1-a}
VM=${VM:-cad-2}
OC_ID=${1:-996c87ae}
shift || true
CLAUDE_IDS=("$@")
[ ${#CLAUDE_IDS[@]} -gt 0 ] || CLAUDE_IDS=(a12e00a7 b1c8ef41)
g=(--project "$PROJECT" --zone "$ZONE" --tunnel-through-iap)

src=$HOME/.aienv/.store/$OC_ID/opencode/auth.json
[ -f "$src" ] || { echo "missing $src" >&2; exit 1; }
# On the VM, /var/lib/cad is the container's /data = its $HOME, so cad reads
# ~/.aienv/.store/<id>/opencode/auth.json = /var/lib/cad/.aienv/.store/<id>/opencode/auth.json.
dst=/var/lib/cad/.aienv/.store/$OC_ID/opencode/auth.json
tmp=/tmp/opencode-auth-$OC_ID.json # COS /tmp is tmpfs
echo "+ gcloud compute scp <auth.json> $VM:$tmp"
gcloud compute scp "${g[@]}" "$src" "$VM:$tmp" >/dev/null
echo "+ install -> $dst (uid 10001, mode 600)"
gcloud compute ssh "$VM" "${g[@]}" --command \
  "sudo install -D -o 10001 -g 10001 -m 600 $tmp $dst && rm -f $tmp && sudo chown -R 10001:10001 /var/lib/cad/.aienv"

cat <<MSG

Claude login (per account; interactive, run yourself):
MSG
for id in "${CLAUDE_IDS[@]}"; do
  cat <<MSG
  gcloud compute ssh $VM --project $PROJECT --zone $ZONE --tunnel-through-iap -- -t \\
    sudo docker exec -it -e CLAUDE_CONFIG_DIR=/data/.aienv/.store/$id cad claude
  # then /login, finish in the browser, /exit.  -> agent claude/$id
MSG
done
cat <<MSG

CLAUDE_CONFIG_DIR must be /data/.aienv/.store/<id>: cad's usage collector runs
CLAUDE_CONFIG_DIR=\$HOME/.aienv/.store/<id> claude, and \$HOME=/data (= /var/lib/cad on the VM).
Without it, login lands in /data/.claude (= agent claude/default).
Check: gcloud compute ssh $VM --project $PROJECT --zone $ZONE --tunnel-through-iap -- sudo docker exec cad cad show usage --local
MSG
