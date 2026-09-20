#!/bin/bash
# Tests for hooks/*.sh. Run: bash test/hooks.test.sh
set -u

ROOT=$(cd "$(dirname "$0")/.." && pwd)
SS="$ROOT/hooks/harness-session-start.sh"
TC="$ROOT/hooks/harness-task-completed.sh"
TMP=$(mktemp -d)
trap 'rm -r "$TMP"' EXIT
FAILED=0

check() { # name, expected, actual
  if [ "$2" = "$3" ]; then
    echo "ok   - $1"
  else
    echo "FAIL - $1 (expected '$2', got '$3')"
    FAILED=1
  fi
}

# --- SessionStart ---
mkdir -p "$TMP/filled/docs/decisions" "$TMP/empty" "$TMP/template"
printf '# PROGRESS\n- 最終更新：2026-09-20 11:00\n## 次の一手\n1. do the thing\n' >"$TMP/filled/PROGRESS.md"
touch "$TMP/filled/docs/decisions/0000-template.md" "$TMP/filled/docs/decisions/0001-foo.md"
printf '# PROGRESS\n- 最終更新：YYYY-MM-DD HH:MM\n' >"$TMP/template/PROGRESS.md"

ss() { # source, project dir
  printf '{"source":"%s","cwd":"%s"}' "$1" "$2" | CLAUDE_PROJECT_DIR="$2" bash "$SS"
}

OUT=$(ss startup "$TMP/filled")
check "startup injects PROGRESS.md" 1 "$(printf '%s' "$OUT" | grep -c 'do the thing')"
check "startup lists ADRs without the template" 1 "$(printf '%s' "$OUT" | grep -c 'ADR: 0001-foo.md $')"
check "startup points at /pickup" 1 "$(printf '%s' "$OUT" | grep -c '/pickup')"
check "clear injects" 1 "$(ss clear "$TMP/filled" | grep -c 'do the thing')"
check "compact injects" 1 "$(ss compact "$TMP/filled" | grep -c 'do the thing')"
check "resume is silent" "" "$(ss resume "$TMP/filled")"
check "fork is silent" "" "$(ss fork "$TMP/filled")"
check "no PROGRESS.md is silent" "" "$(ss startup "$TMP/empty")"
check "unfilled template is silent" "" "$(ss startup "$TMP/template")"
OUT=$(printf '{"source":"startup","cwd":"%s"}' "$TMP/filled" | env -u CLAUDE_PROJECT_DIR bash "$SS")
check "falls back to cwd without CLAUDE_PROJECT_DIR" 1 "$(printf '%s' "$OUT" | grep -c 'do the thing')"
ss startup "$TMP/filled" >/dev/null
check "exits 0" 0 $?

# --- TaskCompleted ---
tc() { # description JSON value (already quoted) or empty for an absent field
  local payload='{"task_subject":"Add foo"}'
  [ -n "$1" ] && payload='{"task_subject":"Add foo","task_description":'"$1"'}'
  printf '%s' "$payload" | CLAUDE_CODE_TASK_LIST_ID=test bash "$TC" 2>/dev/null
  echo $?
}

check "evidence line passes" 0 "$(tc '"do foo\nVERIFIED: npm test -> 12 pass"')"
check "Japanese marker with fullwidth colon passes" 0 "$(tc '"検証：manual -> 目視で確認"')"
check "no evidence blocks" 2 "$(tc '"do foo"')"
check "empty marker blocks" 2 "$(tc '"VERIFIED:   "')"
check "marker in mid-sentence blocks" 2 "$(tc '"will be VERIFIED: later"')"
check "absent description blocks" 2 "$(tc '')"

printf '{"task_subject":"Add foo","task_description":"do foo"}' | env -u CLAUDE_CODE_TASK_LIST_ID bash "$TC" 2>/dev/null
check "not enforced without CLAUDE_CODE_TASK_LIST_ID" 0 $?

ERR=$(printf '{"task_subject":"Add foo"}' | CLAUDE_CODE_TASK_LIST_ID=test bash "$TC" 2>&1 >/dev/null)
check "feedback names the task" 1 "$(printf '%s' "$ERR" | grep -c 'Add foo')"
check "feedback explains the format" 1 "$(printf '%s' "$ERR" | grep -c '^形式: VERIFIED:')"

exit $FAILED
