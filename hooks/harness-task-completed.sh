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

INPUT=$(cat)
[ -n "${CLAUDE_CODE_TASK_LIST_ID:-}" ] || exit 0

SUBJECT=$(printf '%s' "$INPUT" | jq -r '.task_subject // ""' 2>/dev/null)
DESCRIPTION=$(printf '%s' "$INPUT" | jq -r '.task_description // ""' 2>/dev/null)

# Accept "VERIFIED: <command> -> <result>" or "検証: ..." with a non-empty body.
if printf '%s\n' "$DESCRIPTION" | grep -Eq '^[[:space:]]*(VERIFIED|検証)[:：][[:space:]]*[^[:space:]]'; then
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
