#!/bin/bash
# TaskCompleted hook: refuse to close a task that carries no verification
# record. Exit 2 keeps the task open and feeds stderr back to Claude.
#
# The hook checks for evidence instead of running a command itself: a command
# taken from the task text would run without a permission prompt.
#
# Enforced only in projects that opted into the harness, which is what a
# CLAUDE_CODE_TASK_LIST_ID in the project's settings signals (ADR-0003).
set -u

# jq filter shared by the main-session and subagent transcript aggregations.
#
# One assistant turn is written as several JSONL lines (one per content
# block), all sharing the same .message.id and carrying the same usage
# figures — counting every line double- and triple-counts that turn's
# tokens (confirmed against a real transcript: 90 usage-bearing lines,
# 41 distinct message ids). So usage-bearing lines are first folded down
# to one record per .message.id, keeping the LAST line seen for a given
# id (streaming writes intermediate usage before the turn's final line).
# A line with no .message.id (older/unexpected shape) is kept as its own
# record instead of being dropped, so a format change degrades gracefully
# rather than silently losing that line's tokens.
#
# Only after that fold does it group by model and sum the four token
# counters plus a message count (of folded records, not raw lines).
# Defensive `?` on every field access so a transcript format change
# degrades to an empty {} instead of erroring the hook.
TOKEN_USAGE_FILTER='
  ( [ .[] | select(type=="object") | select(.message.usage? != null) ] ) as $lines
  | ( reduce $lines[] as $item (
        {byid: {}, list: []};
        ($item.message.id? // null) as $id
        | if $id == null then
            .list += [{model: ($item.message.model? // "unknown"), u: $item.message.usage}]
          else
            .byid[$id] = {model: ($item.message.model? // "unknown"), u: $item.message.usage}
          end
      )
    ) as $folded
  | ( $folded.list + ($folded.byid | [.[]]) ) as $records
  | ( $records
      | group_by(.model)
      | map({
          key: .[0].model,
          value: {
            input: (map(.u.input_tokens? // 0) | add),
            output: (map(.u.output_tokens? // 0) | add),
            cache_read: (map(.u.cache_read_input_tokens? // 0) | add),
            cache_creation: (map(.u.cache_creation_input_tokens? // 0) | add),
            messages: length
          }
        })
      | from_entries
    )
'

# Append one token-usage line for this completion to the project's
# .claude/token-usage.jsonl, opt-in: only when that file already exists
# (a global hook must not scatter untracked files across every project).
# Best-effort only: every failure here (missing jq, missing transcript,
# zero usage lines, an unwritable file) must leave completion unaffected,
# so every step below is guarded and falls back instead of propagating.
log_token_usage() {
  command -v jq >/dev/null 2>&1 || return 0

  local cwd project_dir out_file
  cwd=$(printf '%s' "$INPUT" | jq -r '.cwd // ""' 2>/dev/null)
  project_dir="${CLAUDE_PROJECT_DIR:-}"
  [ -n "$project_dir" ] || project_dir="$cwd"
  [ -n "$project_dir" ] || return 0
  out_file="$project_dir/.claude/token-usage.jsonl"
  [ -f "$out_file" ] || return 0

  local transcript_path session_id task_id task_subject
  transcript_path=$(printf '%s' "$INPUT" | jq -r '.transcript_path // ""' 2>/dev/null)
  session_id=$(printf '%s' "$INPUT" | jq -r '.session_id // ""' 2>/dev/null)
  task_id=$(printf '%s' "$INPUT" | jq -r '.task_id // ""' 2>/dev/null)
  task_subject=$(printf '%s' "$INPUT" | jq -r '.task_subject // ""' 2>/dev/null)

  local main_json sub_json sub_dir
  main_json=$(jq -c -s "$TOKEN_USAGE_FILTER" "$transcript_path" 2>/dev/null)
  [ -n "$main_json" ] || main_json="{}"

  sub_dir="${transcript_path%.jsonl}/subagents"
  sub_json="{}"
  if [ -d "$sub_dir" ]; then
    sub_json=$(cat "$sub_dir"/*.jsonl 2>/dev/null | jq -c -s "$TOKEN_USAGE_FILTER" 2>/dev/null)
    [ -n "$sub_json" ] || sub_json="{}"
  fi

  jq -nc \
    --arg ts "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
    --arg session_id "$session_id" \
    --arg task_id "$task_id" \
    --arg task_subject "$task_subject" \
    --argjson main "$main_json" \
    --argjson subagents "$sub_json" \
    '{ts:$ts, session_id:$session_id, task_id:$task_id, task_subject:$task_subject, main:$main, subagents:$subagents}' \
    >>"$out_file" 2>/dev/null
  return 0
}

INPUT=$(cat)
[ -n "${CLAUDE_CODE_TASK_LIST_ID:-}" ] || { log_token_usage; exit 0; }

SUBJECT=$(printf '%s' "$INPUT" | jq -r '.task_subject // ""' 2>/dev/null)
DESCRIPTION=$(printf '%s' "$INPUT" | jq -r '.task_description // ""' 2>/dev/null)

# Accept "VERIFIED: <command> -> <result>" or "検証: ..." with a non-empty body.
if printf '%s\n' "$DESCRIPTION" | grep -Eq '^[[:space:]]*(VERIFIED|検証)[:：][[:space:]]*[^[:space:]]'; then
  log_token_usage
  exit 0
fi

cat >&2 <<EOF
[harness] 未検証のため completed にできません: $SUBJECT
完了条件のコマンドを main で実行し、その結果をタスクの description に1行追記してから、別の TaskUpdate で completed にしてください。
形式: VERIFIED: <実行したコマンド> -> <結果>
コマンドで確認できないタスクは VERIFIED: manual -> <何をどう確認したか>
検証が通らなかった場合は completed にせず、原因を description に書いて pending に戻してください。
EOF
exit 2
