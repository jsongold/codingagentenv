#!/bin/bash
# Tests for the worker image (.devcontainer/Dockerfile + agent/bootstrap.sh).
# Builds the image and runs bootstrap inside it: once resource-limited (to
# check cad sees the container's cgroup limits, not the host's), and twice
# with a child command to check bootstrap reports its exit code (ADR-0009)
# instead of exec'ing it away.
# Run: bash test/worker-image.test.sh
set -u

ROOT=$(cd "$(dirname "$0")/.." && pwd)
IMG=codingagentenv-worker-test
FAILED=0

check() { # name, expected, actual
  if [ "$2" = "$3" ]; then echo "ok   - $1"; else echo "FAIL - $1 (expected '$2', got '$3')"; FAILED=1; fi
}

if ! docker info >/dev/null 2>&1; then
  echo "skip - worker-image tests: docker is not running"
  exit 0
fi

if ! docker build -t "$IMG" -f "$ROOT/.devcontainer/Dockerfile" "$ROOT" >"$ROOT"/.worker-image-build.log 2>&1; then
  echo "FAIL - docker build (see .worker-image-build.log)"
  exit 1
fi
rm -f "$ROOT"/.worker-image-build.log

# --- capacity sees the container's cgroup limits, not the host's ---
out=$(docker run --rm -m 1g --cpus 1 "$IMG" bootstrap)
check "bootstrap with no REPO, no cmd exits 0" 0 "$?"

memTotalMB=$(echo "$out" | jq -r '.memTotalMB' 2>/dev/null)
cpus=$(echo "$out" | jq -r '.cpus' 2>/dev/null)
if [ -z "$memTotalMB" ] || [ "$memTotalMB" = "null" ]; then
  echo "FAIL - capacity JSON parses (got: $out)"
  FAILED=1
else
  check "memTotalMB <= 1100 (cgroup limit seen)" 1 "$([ "$memTotalMB" -le 1100 ] && echo 1 || echo 0)"
  check "cpus == 1 (cgroup limit seen)" 1 "$cpus"
fi

# --- bootstrap runs the child and reports its result instead of exec'ing it ---
out=$(docker run --rm "$IMG" bootstrap true)
status=$?
check "bootstrap true: container exits 0" 0 "$status"
check "bootstrap true: result ok" ok "$(echo "$out" | tail -n1 | jq -r '.result' 2>/dev/null)"
check "bootstrap true: exitCode 0" 0 "$(echo "$out" | tail -n1 | jq -r '.exitCode' 2>/dev/null)"

out=$(docker run --rm "$IMG" bootstrap false)
status=$?
check "bootstrap false: container exits 1" 1 "$status"
check "bootstrap false: result fail" fail "$(echo "$out" | tail -n1 | jq -r '.result' 2>/dev/null)"
check "bootstrap false: exitCode 1" 1 "$(echo "$out" | tail -n1 | jq -r '.exitCode' 2>/dev/null)"

docker image rm "$IMG" >/dev/null 2>&1
docker image prune -f >/dev/null 2>&1

exit $FAILED
