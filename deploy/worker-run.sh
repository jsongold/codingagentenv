#!/bin/bash
# Entrypoint of the opencode worker image (Dockerfile.worker): one GitHub issue -> one PR.
#   docker run --rm -v /var/lib/cad:/data -e ISSUE=7 -e REPO=owner/name -e MODEL=provider/model <image>
# Started by `orchd dispatch` (runner mode vm) over IAP ssh on a worker VM (deploy/gcp/worker-startup.sh).
# Auth comes from files deploy/fetch-auth.sh wrote under /data (never from args or env of the caller):
#   /data/.aienv/.store/<id>/opencode/auth.json   -> ~/.local/share/opencode/auth.json (https://opencode.ai/docs/cli/)
#   /data/.aienv/.store/<id>/github/token         -> GH_TOKEN (fine-grained PAT: contents + pull requests write)
# Clones REPO, checks out task/<ISSUE> (the remote branch if a previous run pushed it), runs
# `opencode run --model $MODEL <prompt>`, then commits, pushes and opens a PR "Closes #<ISSUE>" unless one is open.
# ponytail: pushes once, after opencode finishes. A Spot preemption before that loses the run's edits (the task is
# re-queued, see skills/orchestrate); periodic WIP pushes if runs get long.
# Exit: 0 = PR open; opencode's code when it failed (branch pushed, no PR); 2 = bad env; 3 = auth files missing;
#   1 = anything else (no changes, clone/push/gh failed).
# Never prints the token: no `set -x`, and git errors are masked.
set -euo pipefail
: "${ISSUE:?}" "${REPO:?}" "${MODEL:?}"
case $ISSUE in '' | *[!0-9]*) echo "worker: ISSUE must be a number" >&2; exit 2 ;; esac
case $REPO in */*) ;; *) echo "worker: REPO must be owner/name" >&2; exit 2 ;; esac
STORE=${WORKER_STORE:-/data/.aienv/.store}

# The worker VM gets exactly one of each (its own secret list, deploy/gcp/worker-auth.list).
shopt -s nullglob
oc=("$STORE"/*/opencode/auth.json) tok=("$STORE"/*/github/token)
if [ ${#oc[@]} -ne 1 ] || [ ${#tok[@]} -ne 1 ]; then
  echo "worker: want exactly one */opencode/auth.json and one */github/token under $STORE (found ${#oc[@]}, ${#tok[@]})" >&2
  exit 3
fi
GH_TOKEN=$(<"${tok[0]}")
export GH_TOKEN
mkdir -p "$HOME/.local/share/opencode"
install -m 600 "${oc[0]}" "$HOME/.local/share/opencode/auth.json"

# Same as agent/bootstrap.sh: the helper (`gh auth git-credential`) reads GH_TOKEN at call time, so the token is
# never written to disk.
gh auth setup-git --hostname github.com --force >/dev/null
git config --global user.name "${GIT_NAME:-opencode-worker}"
git config --global user.email "${GIT_EMAIL:-opencode-worker@users.noreply.github.com}"
mask() { sed "s/$GH_TOKEN/***/g" >&2; }

branch=task/$ISSUE
dir=$(mktemp -d)
git clone --quiet "${CLONE_URL:-https://github.com/$REPO.git}" "$dir" 2> >(mask)
cd "$dir"
if git show-ref --verify --quiet "refs/remotes/origin/$branch"; then
  git checkout --quiet -b "$branch" "origin/$branch"
else
  git checkout --quiet -b "$branch" origin/main
fi

title=$(gh issue view "$ISSUE" --repo "$REPO" --json title -q .title)
body=$(gh issue view "$ISSUE" --repo "$REPO" --json body -q .body)
prompt="repo $REPO の Issue #$ISSUE を解決する。
タイトル: $title

## Issue 本文
${body:-（なし）}

## 手順
- 今いるディレクトリがその repo（ブランチ $branch）。変更は Issue の範囲だけ
- repo の CLAUDE.md / README にあるテストを通す
- commit / push / PR 作成はしない（この後 worker が行う）"

rc=0
# --auto: approve permission prompts (no one answers them here); the container is disposable.
opencode run --auto --model "$MODEL" "$prompt" || rc=$?
echo "worker: opencode exit $rc"

git add -A
git diff --cached --quiet || git commit --quiet -m "task #$ISSUE: $title" -m "opencode run --model $MODEL"
if [ "$(git rev-list --count origin/main..HEAD)" -eq 0 ]; then
  echo "worker: no changes against main; no PR" >&2
  exit 1
fi
git push --quiet -u origin "$branch" 2> >(mask)
if [ "$rc" -ne 0 ]; then # keep the work on the branch for a retry (reused above), but no PR
  echo "worker: opencode failed; pushed $branch without a PR" >&2
  exit "$rc"
fi
if [ "$(gh pr list --repo "$REPO" --head "$branch" --state open --json number -q length)" -eq 0 ]; then
  gh pr create --repo "$REPO" --base main --head "$branch" --title "$title" \
    --body "Closes #$ISSUE

opencode worker（model \`$MODEL\`、opencode exit $rc）"
else
  echo "worker: PR for $branch already open; pushed"
fi
exit 0
