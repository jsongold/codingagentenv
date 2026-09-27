#!/usr/bin/env bash
# bootstrap [cmd...] — worker entrypoint (ADR-0008). Clones REPO@BRANCH (from
# origin/BASE if BRANCH doesn't exist yet), starts cad on loopback, waits for
# /healthz, then runs cmd as a child (ADR-0009: not exec'd, so its exit code
# can be captured and reported). With no cmd, prints cad capacity and exits 0.
# With a cmd, writes {"exitCode","result":"ok"|"fail","reason"} to
# RESULT_FILE, also prints it as the last stdout line, and exits with the
# child's exit code. Providers will expose RESULT_FILE via their own
# mechanism later.
#
# Env: REPO (owner/name; clone is skipped when unset), BRANCH (required if
# REPO is set), BASE (default main), GH_TOKEN (optional; authenticates git
# against github.com for bootstrap's own clone/fetch AND any later fetch/push
# the child agent command runs, via a `gh` credential helper that reads
# GH_TOKEN from the environment at call time — the token itself is never
# written to .git/config or ~/.gitconfig, never echoed), CLONE_URL (override
# the derived https://github.com/<REPO>.git, e.g. for a local file:// repo in
# tests), CAD_ADDR (default 127.0.0.1:7878, must be loopback), CAD_TOKEN
# (bearer token cad requires beyond /healthz), WORKDIR (default
# /home/agent/workspace), RESULT_FILE (default /tmp/agent-result.json).
set -euo pipefail

CAD_ADDR=${CAD_ADDR:-127.0.0.1:7878}
export WORKDIR=${WORKDIR:-/home/agent/workspace}
BASE=${BASE:-main}

case "$CAD_ADDR" in
  127.0.0.1:*|localhost:*) ;;
  *) echo "bootstrap: refusing to bind cad to non-loopback CAD_ADDR=$CAD_ADDR" >&2; exit 1 ;;
esac

if [ -n "${REPO:-}" ]; then
  : "${BRANCH:?bootstrap: BRANCH is required when REPO is set}"
  url=${CLONE_URL:-https://github.com/${REPO}.git}
  if [ -n "${GH_TOKEN:-}" ]; then
    # Global, not per-repo: this authenticates bootstrap's own clone/fetch
    # below AND any later fetch/push the child agent command runs. `gh auth
    # git-credential` reads GH_TOKEN from its own process environment each
    # time git invokes it, so the token is never written to disk here --
    # only the helper command itself is (`git config --global --get
    # credential.https://github.com.helper`) -- and it stays live for as
    # long as GH_TOKEN stays set.
    gh auth setup-git --hostname github.com --force >/dev/null
  fi
  # A CLONE_URL override (used for local/file:// testing) commonly points at
  # a bind-mounted path this container doesn't own; trust it explicitly
  # rather than failing on git's dubious-ownership check. (safe.directory is
  # read before command-line config is applied, so `-c` can't set it here;
  # it must go through a config file.)
  [ -n "${CLONE_URL:-}" ] && git config --global --add safe.directory '*'
  # Mask the token in any error output git prints, as defense in depth.
  if ! git clone --quiet "$url" "$WORKDIR" 2> >(sed "s/${GH_TOKEN:-x-no-token-set-x}/***/g" >&2); then
    echo "bootstrap: git clone failed" >&2
    exit 1
  fi
  cd "$WORKDIR"
  git fetch --quiet origin "$BASE"
  if git show-ref --verify --quiet "refs/heads/$BRANCH"; then
    # clone already checked this out locally (BRANCH is the remote's default branch).
    git checkout --quiet "$BRANCH"
  elif git show-ref --verify --quiet "refs/remotes/origin/$BRANCH"; then
    git checkout --quiet -b "$BRANCH" "origin/$BRANCH"
  else
    git checkout --quiet -b "$BRANCH" "origin/$BASE"
  fi
fi

CAD_ADDR="$CAD_ADDR" cad &
cad_pid=$!
trap 'kill "$cad_pid" 2>/dev/null || true' EXIT

for _ in $(seq 1 30); do
  curl -fsS "http://${CAD_ADDR}/healthz" >/dev/null 2>&1 && break
  sleep 0.5
done
curl -fsS "http://${CAD_ADDR}/healthz" >/dev/null 2>&1 || {
  echo "bootstrap: cad did not become healthy" >&2
  exit 1
}

cad_auth=()
[ -n "${CAD_TOKEN:-}" ] && cad_auth=(-H "Authorization: Bearer $CAD_TOKEN")

if [ "$#" -eq 0 ]; then
  curl -fsS "${cad_auth[@]}" "http://${CAD_ADDR}/v1/capacity"
  echo
  exit 0
fi

RESULT_FILE=${RESULT_FILE:-/tmp/agent-result.json}
errfile=$(mktemp)
set +e
"$@" 2>"$errfile"
code=$?
set -e
cat "$errfile" >&2

reason=$(tail -n1 "$errfile")
rm -f "$errfile"
[ -n "$reason" ] || reason="exit $code"
result=ok
[ "$code" -eq 0 ] || result=fail
json=$(jq -nc --argjson exitCode "$code" --arg result "$result" --arg reason "$reason" \
  '{exitCode:$exitCode, result:$result, reason:$reason}')
printf '%s\n' "$json" >"$RESULT_FILE"
printf '%s\n' "$json"
exit "$code"
