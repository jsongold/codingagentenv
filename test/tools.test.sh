#!/bin/bash
# Tests for tools/agent-gate (no Docker needed). Run: bash test/tools.test.sh
set -u

ROOT=$(cd "$(dirname "$0")/.." && pwd)
TMP=$(mktemp -d)
trap 'rm -r "$TMP"' EXIT
FAILED=0
export AGENT_GATE_LOCK_DIR="$TMP/locks"

check() { # name, expected, actual
  if [ "$2" = "$3" ]; then echo "ok   - $1"; else echo "FAIL - $1 (expected '$2', got '$3')"; FAILED=1; fi
}
gate() { bash "$ROOT/tools/agent-gate" "$@" >"$TMP/out" 2>&1; }

mkdir -p "$TMP/wt/scripts"
printf '# comment\n\necho one\ntest "$PWD" = "$WT"\ncommand -v testdb\n' >"$TMP/wt/scripts/gate.steps"
gate "$TMP/wt"
check "passing steps exit 0" 0 $?
check "steps run in the worktree with tools on PATH" 3 "$(grep -c '^\[0\]' "$TMP/out")"
check "slot is released" 0 "$(ls "$AGENT_GATE_LOCK_DIR" | wc -l | tr -d ' ')"

printf 'false\necho after\n' >"$TMP/fail.steps"
gate "$TMP/wt" "$TMP/fail.steps"
check "a failing step exits 1" 1 $?
check "later steps still run" 1 "$(grep -c '^\[0\] echo after' "$TMP/out")"

gate "$TMP/wt" "$TMP/missing.steps"
check "missing steps file exits 2" 2 $?

# A slot held by a dead PID is reclaimed; with 1 slot the gate would otherwise wait forever.
mkdir -p "$AGENT_GATE_LOCK_DIR/slot.1"
echo 999999 >"$AGENT_GATE_LOCK_DIR/slot.1/pid"
AGENT_GATE_SLOTS=1 timeout 20 bash "$ROOT/tools/agent-gate" "$TMP/wt" >/dev/null 2>&1
check "stale slot is reclaimed" 0 $?

exit $FAILED
