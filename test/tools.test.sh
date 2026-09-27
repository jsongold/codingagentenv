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

# --- capacity-driven slot count ---
UNREACHABLE=http://127.0.0.1:1 # port 1 always refuses; a fast, deterministic "cad is down"

# AGENT_GATE_SLOTS wins even when a slot is already held (a higher capacity would let it through).
mkdir -p "$AGENT_GATE_LOCK_DIR/slot.1"
echo $$ >"$AGENT_GATE_LOCK_DIR/slot.1/pid"
AGENT_GATE_SLOTS=1 CAD_URL=$UNREACHABLE timeout 3 bash "$ROOT/tools/agent-gate" "$TMP/wt" >/dev/null 2>&1
check "AGENT_GATE_SLOTS env overrides cad" 124 $?
rm -rf "${AGENT_GATE_LOCK_DIR:?}"/slot.*

# A fake cad HTTP server reporting slots=2 lets a 2nd slot through while slot.1 is held.
mkdir -p "$AGENT_GATE_LOCK_DIR/slot.1"
echo $$ >"$AGENT_GATE_LOCK_DIR/slot.1/pid"
CAD_PORT=18765
CAD_BODY='{"host":"test","slots":2}'
printf 'HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: %d\r\n\r\n%s' \
  "${#CAD_BODY}" "$CAD_BODY" >"$TMP/cad_resp.http"
nc -l "$CAD_PORT" <"$TMP/cad_resp.http" >/dev/null 2>&1 &
NCPID=$!
sleep 0.3
CAD_URL="http://127.0.0.1:$CAD_PORT" timeout 10 bash "$ROOT/tools/agent-gate" "$TMP/wt" >/dev/null 2>&1
check "cad HTTP capacity.slots is used" 0 $?
kill "$NCPID" 2>/dev/null
rm -rf "${AGENT_GATE_LOCK_DIR:?}"/slot.*

# cad HTTP unreachable falls back to the `cad capacity` one-shot; CAD_SLOTS makes it deterministic.
mkdir -p "$AGENT_GATE_LOCK_DIR/slot.1"
echo $$ >"$AGENT_GATE_LOCK_DIR/slot.1/pid"
CAD_URL=$UNREACHABLE CAD_SLOTS=2 timeout 30 bash "$ROOT/tools/agent-gate" "$TMP/wt" >/dev/null 2>&1
check "cad HTTP unreachable falls back to cad capacity one-shot" 0 $?
rm -rf "${AGENT_GATE_LOCK_DIR:?}"/slot.*

# slots=0 with nothing currently running still lets one gate through (never deadlock).
AGENT_GATE_SLOTS=0 CAD_URL=$UNREACHABLE timeout 10 bash "$ROOT/tools/agent-gate" "$TMP/wt" >/dev/null 2>&1
check "slots=0 with nothing running still allows 1" 0 $?
check "lock dir empty after slots=0 run" 0 "$(ls "$AGENT_GATE_LOCK_DIR" | wc -l | tr -d ' ')"

exit $FAILED
