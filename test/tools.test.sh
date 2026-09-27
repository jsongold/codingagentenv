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
echo 999999 >"$AGENT_GATE_LOCK_DIR/slot.1"
AGENT_GATE_SLOTS=1 timeout 20 bash "$ROOT/tools/agent-gate" "$TMP/wt" >/dev/null 2>&1
check "stale slot is reclaimed" 0 $?

# --- capacity-driven slot count ---
UNREACHABLE=http://127.0.0.1:1 # port 1 always refuses; a fast, deterministic "cad is down"

# AGENT_GATE_SLOTS wins even when a slot is already held (a higher capacity would let it through).
echo $$ >"$AGENT_GATE_LOCK_DIR/slot.1"
AGENT_GATE_SLOTS=1 CAD_URL=$UNREACHABLE timeout 3 bash "$ROOT/tools/agent-gate" "$TMP/wt" >/dev/null 2>&1
check "AGENT_GATE_SLOTS env overrides cad" 124 $?
rm -rf "${AGENT_GATE_LOCK_DIR:?}"/slot.*

# A fake cad HTTP server reporting slots=2 lets a 2nd slot through while slot.1 is held.
echo $$ >"$AGENT_GATE_LOCK_DIR/slot.1"
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
echo $$ >"$AGENT_GATE_LOCK_DIR/slot.1"
CAD_URL=$UNREACHABLE CAD_SLOTS=2 timeout 30 bash "$ROOT/tools/agent-gate" "$TMP/wt" >/dev/null 2>&1
check "cad HTTP unreachable falls back to cad capacity one-shot" 0 $?
rm -rf "${AGENT_GATE_LOCK_DIR:?}"/slot.*

# slots=0 with nothing currently running still lets one gate through (never deadlock).
AGENT_GATE_SLOTS=0 CAD_URL=$UNREACHABLE timeout 10 bash "$ROOT/tools/agent-gate" "$TMP/wt" >/dev/null 2>&1
check "slots=0 with nothing running still allows 1" 0 $?
check "lock dir empty after slots=0 run" 0 "$(ls "$AGENT_GATE_LOCK_DIR" | wc -l | tr -d ' ')"

# A shrink from 2 to 1 must not let a low-numbered slot through while a
# higher-numbered one from the old, bigger capacity is still held.
echo $$ >"$AGENT_GATE_LOCK_DIR/slot.2"
AGENT_GATE_SLOTS=1 CAD_URL=$UNREACHABLE timeout 3 bash "$ROOT/tools/agent-gate" "$TMP/wt" >/dev/null 2>&1
check "shrink to 1 blocks while a higher-numbered slot is held" 124 $?
rm -rf "${AGENT_GATE_LOCK_DIR:?}"/slot.*

# Concurrent acquisition against a single slot must never let two gates run their
# steps at once (a marker file per running gate; a watcher samples how many exist).
CTMP="$TMP/concurrent"
mkdir -p "$CTMP/scripts" "$CTMP/running"
printf ': > "%s/running/$$"\nsleep 0.3\nrm -f "%s/running/$$"\n' "$CTMP" "$CTMP" >"$CTMP/scripts/gate.steps"
MAXFILE="$TMP/maxrunning"
echo 0 >"$MAXFILE"
STOP="$TMP/stop_watch"
rm -f "$STOP"
(
  while [ ! -e "$STOP" ]; do
    c=$(ls "$CTMP/running" 2>/dev/null | wc -l | tr -d ' ')
    m=$(cat "$MAXFILE" 2>/dev/null || echo 0)
    [ "$c" -gt "$m" ] && echo "$c" >"$MAXFILE"
    sleep 0.05
  done
) &
WATCHER=$!
pids=()
for _ in 1 2 3 4 5; do
  AGENT_GATE_SLOTS=1 CAD_URL=$UNREACHABLE bash "$ROOT/tools/agent-gate" "$CTMP" >/dev/null 2>&1 &
  pids+=("$!")
done
for p in "${pids[@]}"; do wait "$p"; done
touch "$STOP"
wait "$WATCHER" 2>/dev/null
check "concurrent gates never double-book a slot" 1 "$(cat "$MAXFILE")"
rm -rf "${AGENT_GATE_LOCK_DIR:?}"/slot.* "$STOP"

# wait_step wakes on the SSE stream's real 2nd "data:" line, well before its
# --max-time timeout. Source the script for its functions only (the
# BASH_SOURCE guard skips running a gate) and drive wait_step directly against
# a fake SSE server that emits its 2nd event quickly.
(
  # shellcheck disable=SC1091
  source "$ROOT/tools/agent-gate"
  SSE_PORT=18779
  (
    printf 'HTTP/1.1 200 OK\r\nContent-Type: text/event-stream\r\n\r\n'
    printf 'data: {"topic":"capacity"}\n\n'
    sleep 0.3
    printf 'data: {"topic":"capacity"}\n\n'
    sleep 3
  ) | nc -l "$SSE_PORT" >/dev/null 2>&1 &
  NCPID=$!
  sleep 0.2
  export CAD_URL="http://127.0.0.1:$SSE_PORT"
  t0=$SECONDS
  wait_step 5
  echo $((SECONDS - t0)) >"$TMP/wait_elapsed"
  kill "$NCPID" 2>/dev/null
)
check "SSE wait wakes before the full timeout" yes "$([ "$(cat "$TMP/wait_elapsed")" -le 2 ] && echo yes || echo no)"

exit $FAILED
