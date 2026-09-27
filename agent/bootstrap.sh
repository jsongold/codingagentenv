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
# REPO is set), BASE (default main), GH_TOKEN (optional clone auth, never
# echoed), CAD_ADDR (default 127.0.0.1:7878, must be loopback), WORKDIR
# (default /home/agent/workspace), RESULT_FILE (default /tmp/agent-result.json).
set -euo pipefail

CAD_ADDR=${CAD_ADDR:-127.0.0.1:7878}
WORKDIR=${WORKDIR:-/home/agent/workspace}
BASE=${BASE:-main}

case "$CAD_ADDR" in
  127.0.0.1:*|localhost:*) ;;
  *) echo "bootstrap: refusing to bind cad to non-loopback CAD_ADDR=$CAD_ADDR" >&2; exit 1 ;;
esac

if [ -n "${REPO:-}" ]; then
  : "${BRANCH:?bootstrap: BRANCH is required when REPO is set}"
  if [ -n "${GH_TOKEN:-}" ]; then
    url="https://x-access-token:${GH_TOKEN}@github.com/${REPO}.git"
  else
    url="https://github.com/${REPO}.git"
  fi
  # Mask the token in any error output git prints (e.g. a failed-clone URL).
  if ! git clone --quiet "$url" "$WORKDIR" 2> >(sed "s/${GH_TOKEN:-x-no-token-set-x}/***/g" >&2); then
    echo "bootstrap: git clone failed" >&2
    exit 1
  fi
  cd "$WORKDIR"
  git fetch --quiet origin "$BASE"
  if git show-ref --verify --quiet "refs/remotes/origin/$BRANCH"; then
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

if [ "$#" -eq 0 ]; then
  curl -fsS "http://${CAD_ADDR}/v1/capacity"
  echo
  exit 0
fi

RESULT_FILE=${RESULT_FILE:-/tmp/agent-result.json}
errfile=$(mktemp)
set +e
"$@" 2> >(tee "$errfile" >&2)
code=$?
set -e

reason=$(tail -n1 "$errfile" 2>/dev/null)
rm -f "$errfile"
[ -n "$reason" ] || reason="exit $code"
result=ok
[ "$code" -eq 0 ] || result=fail
# Minimal JSON string escaping: backslash and double quote only.
esc=$(printf '%s' "$reason" | sed 's/\\/\\\\/g; s/"/\\"/g')
json=$(printf '{"exitCode":%d,"result":"%s","reason":"%s"}' "$code" "$result" "$esc")
printf '%s\n' "$json" >"$RESULT_FILE"
printf '%s\n' "$json"
exit "$code"
