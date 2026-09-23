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

# --- SessionStart: .claude/handoff/*.md ---
mk_handoff() { # project dir, name, purpose, updated, [body]
  mkdir -p "$1/.claude/handoff"
  printf '# handoff\n- 最終更新：%s\n## 目的\n\n%s\n## 次の一手\n%s\n' "$4" "$3" "${5:-body of $2}" >"$1/.claude/handoff/$2.md"
}

# two valid handoffs: a list with one line each, no full text, /pickup <name>
mk_handoff "$TMP/h2" alpha "alpha purpose" "2026-09-20 10:00" "alpha secret body"
mk_handoff "$TMP/h2" beta "beta purpose" "2026-09-21 09:00" "beta secret body"
touch -t 202609200000 "$TMP/h2/.claude/handoff/alpha.md"
touch -t 202609210000 "$TMP/h2/.claude/handoff/beta.md"
mkdir -p "$TMP/h2/docs/decisions" && touch "$TMP/h2/docs/decisions/0001-foo.md"
OUT=$(ss startup "$TMP/h2")
check "two handoffs: list heading" 1 "$(printf '%s' "$OUT" | grep -c '^\[harness\] handoff 一覧 (session source: startup)$')"
check "two handoffs: alpha line" 1 "$(printf '%s' "$OUT" | grep -c '^- alpha | 最終更新: 2026-09-20 10:00 | 目的: alpha purpose$')"
check "two handoffs: beta line" 1 "$(printf '%s' "$OUT" | grep -c '^- beta | 最終更新: 2026-09-21 09:00 | 目的: beta purpose$')"
check "two handoffs: newest first" "beta alpha" "$(printf '%s' "$OUT" | grep '^- ' | sed 's/^- \([a-z0-9-]*\) .*/\1/' | tr '\n' ' ' | sed 's/ $//')"
check "two handoffs: body is not injected" 0 "$(printf '%s' "$OUT" | grep -c 'secret body')"
check "two handoffs: asks for /pickup <name> in the list note" 1 "$(printf '%s' "$OUT" | grep -c '^.harness. 複数の handoff がある。/pickup <name>')"
check "two handoffs: closing prompt uses /pickup <name>" 1 "$(printf '%s' "$OUT" | grep -c '作業を始める前に /pickup <name> の手順')"
check "two handoffs: still lists ADRs" 1 "$(printf '%s' "$OUT" | grep -c 'ADR: 0001-foo.md $')"
check "two handoffs: resume is silent" "" "$(ss resume "$TMP/h2")"
check "two handoffs: fork is silent" "" "$(ss fork "$TMP/h2")"

# one valid handoff: full text, name in the heading
mk_handoff "$TMP/h1" solo "solo purpose" "2026-09-21 09:00" "solo next step"
OUT=$(ss clear "$TMP/h1")
check "one handoff: heading has the name" 1 "$(printf '%s' "$OUT" | grep -c '^\[harness\] handoff: solo (session source: clear)$')"
check "one handoff: full text injected" 1 "$(printf '%s' "$OUT" | grep -c 'solo next step')"
check "one handoff: points at plain /pickup" 0 "$(printf '%s' "$OUT" | grep -c '/pickup <name>')"

# a template-only handoff is ignored: silent, and it does not count toward a list
mkdir -p "$TMP/ht/.claude/handoff"
printf '# handoff\n- 最終更新：YYYY-MM-DD HH:MM\n## 目的\n\ntemplate\n' >"$TMP/ht/.claude/handoff/tpl.md"
check "template-only handoff is silent" "" "$(ss startup "$TMP/ht")"
mk_handoff "$TMP/ht" real "real purpose" "2026-09-21 09:00" "real body"
OUT=$(ss startup "$TMP/ht")
check "template + one valid handoff counts as one" 1 "$(printf '%s' "$OUT" | grep -c '^\[harness\] handoff: real ')"
check "template handoff is not listed" 0 "$(printf '%s' "$OUT" | grep -c 'tpl')"

# handoff wins over PROGRESS.md; PROGRESS.md is used only without a valid handoff
mkdir -p "$TMP/hp/.claude/handoff"
printf '# PROGRESS\n- 最終更新：2026-09-20 11:00\n## 次の一手\n1. progress only line\n' >"$TMP/hp/PROGRESS.md"
mk_handoff "$TMP/hp" solo "solo purpose" "2026-09-21 09:00" "handoff line"
OUT=$(ss startup "$TMP/hp")
check "handoff and PROGRESS.md: handoff shown" 1 "$(printf '%s' "$OUT" | grep -c 'handoff line')"
check "handoff and PROGRESS.md: PROGRESS.md not read" 0 "$(printf '%s' "$OUT" | grep -c 'progress only line')"
rm "$TMP/hp/.claude/handoff/solo.md"
printf '# handoff\n- 最終更新：YYYY-MM-DD HH:MM\n' >"$TMP/hp/.claude/handoff/tpl.md"
OUT=$(ss startup "$TMP/hp")
check "template-only handoff + PROGRESS.md: falls back to PROGRESS.md" 1 "$(printf '%s' "$OUT" | grep -c 'progress only line')"

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

# --- TaskCompleted: token usage logging ---
# Fixture transcript matching the real shape confirmed in
# docs/context/token-saving-spec.md #4: JSONL lines with
# .message.model / .message.usage, mixed with unrelated line types.
# The "claude-opus-5" lines model the real-world shape found in a real
# transcript: one assistant turn split across several content-block
# lines that all share .message.id and repeat/refine the same usage
# figures (streaming intermediate values, then a final one) — these
# must fold to a single record keyed by id, keeping the LAST line's
# usage, not the sum of all three. The sonnet/fable lines have no
# .message.id at all (older/plain shape) and must each still count
# as their own record.
mkdir -p "$TMP/proj/.claude" "$TMP/proj/tr/main/subagents"
cat >"$TMP/proj/tr/main.jsonl" <<'JSONL'
{"type":"assistant","message":{"model":"claude-sonnet-5","usage":{"input_tokens":100,"output_tokens":20,"cache_read_input_tokens":5,"cache_creation_input_tokens":0}}}
{"type":"assistant","message":{"model":"claude-sonnet-5","usage":{"input_tokens":50,"output_tokens":10,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}}}
{"type":"assistant","message":{"model":"claude-fable-5-1","usage":{"input_tokens":30,"output_tokens":5,"cache_read_input_tokens":0,"cache_creation_input_tokens":2}}}
{"type":"assistant","message":{"id":"msg-opus-1","model":"claude-opus-5","usage":{"input_tokens":1,"output_tokens":1,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}}}
{"type":"assistant","message":{"id":"msg-opus-1","model":"claude-opus-5","usage":{"input_tokens":40,"output_tokens":9,"cache_read_input_tokens":1,"cache_creation_input_tokens":0}}}
{"type":"assistant","message":{"id":"msg-opus-1","model":"claude-opus-5","usage":{"input_tokens":60,"output_tokens":12,"cache_read_input_tokens":3,"cache_creation_input_tokens":1}}}
{"type":"user","message":{"role":"user"}}
JSONL
cat >"$TMP/proj/tr/main/subagents/agent-1.jsonl" <<'JSONL'
{"type":"assistant","message":{"model":"claude-haiku-4","usage":{"input_tokens":8,"output_tokens":1,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}}}
JSONL

tc_log() { # cwd, transcript_path, description
  jq -nc --arg cwd "$1" --arg tp "$2" --arg desc "$3" \
    '{cwd:$cwd, transcript_path:$tp, session_id:"s1", task_id:"tk1", task_subject:"Add foo", task_description:$desc}' \
    | CLAUDE_CODE_TASK_LIST_ID=test bash "$TC" >/dev/null 2>&1
  echo $?
}
LOGFILE="$TMP/proj/.claude/token-usage.jsonl"

# (a) opt-in file present + a real transcript with a main and a subagents
# bucket: one line is appended with the expected per-model sums.
: >"$LOGFILE"
check "logging: allowed completion exits 0" 0 "$(tc_log "$TMP/proj" "$TMP/proj/tr/main.jsonl" 'ok
VERIFIED: manual -> checked')"
check "logging: one line appended" 1 "$(wc -l <"$LOGFILE" | tr -d ' ')"
LINE=$(cat "$LOGFILE")
check "logging: main sonnet input sum (no message.id, each line counts)" 150 "$(printf '%s' "$LINE" | jq '.main["claude-sonnet-5"].input')"
check "logging: main sonnet message count (no message.id)" 2 "$(printf '%s' "$LINE" | jq '.main["claude-sonnet-5"].messages')"
check "logging: main fable message count (single no-id line)" 1 "$(printf '%s' "$LINE" | jq '.main["claude-fable-5-1"].messages')"
check "logging: main fable cache_creation" 2 "$(printf '%s' "$LINE" | jq '.main["claude-fable-5-1"].cache_creation')"
check "logging: main opus dedup by message.id keeps last line's input" 60 "$(printf '%s' "$LINE" | jq '.main["claude-opus-5"].input')"
check "logging: main opus dedup by message.id keeps last line's cache_read" 3 "$(printf '%s' "$LINE" | jq '.main["claude-opus-5"].cache_read')"
check "logging: main opus dedup by message.id, not summed across lines" 1 "$(printf '%s' "$LINE" | jq '.main["claude-opus-5"].messages')"
check "logging: subagents haiku output" 1 "$(printf '%s' "$LINE" | jq '.subagents["claude-haiku-4"].output')"
check "logging: task_id recorded" '"tk1"' "$(printf '%s' "$LINE" | jq '.task_id')"

# (b) no opt-in file in the project: nothing is created.
mkdir -p "$TMP/proj_noopt/.claude"
check "logging: no opt-in file stays silent" 0 "$(tc_log "$TMP/proj_noopt" "$TMP/proj/tr/main.jsonl" 'ok
VERIFIED: manual -> checked')"
NOOPT_FILE="$TMP/proj_noopt/.claude/token-usage.jsonl"
if [ -f "$NOOPT_FILE" ]; then NOOPT_EXISTS=true; else NOOPT_EXISTS=false; fi
check "logging: no opt-in file, no file created" "false" "$NOOPT_EXISTS"

# (c) transcript_path points at a file that doesn't exist. Design choice
# (see ADR-0006): still append a line, with empty {} aggregates, rather
# than silently dropping the completion event. Exit code is unaffected.
: >"$LOGFILE"
check "logging: missing transcript still exits 0" 0 "$(tc_log "$TMP/proj" "$TMP/proj/tr/does-not-exist.jsonl" 'ok
VERIFIED: manual -> checked')"
check "logging: missing transcript logs empty main" '{}' "$(jq -c '.main' <"$LOGFILE" 2>/dev/null)"
check "logging: missing transcript logs empty subagents" '{}' "$(jq -c '.subagents' <"$LOGFILE" 2>/dev/null)"

# (d) rejected path (no VERIFIED line): exit code unchanged, no log line.
: >"$LOGFILE"
check "logging: rejected completion still exits 2" 2 "$(tc_log "$TMP/proj" "$TMP/proj/tr/main.jsonl" 'ok without verification')"
check "logging: rejected completion, no line appended" 0 "$(wc -l <"$LOGFILE" | tr -d ' ')"

exit $FAILED
