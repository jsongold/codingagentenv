#!/bin/bash
# SessionStart hook: inject PROGRESS.md so a new or cleared session starts with
# the handoff context even when the user forgets /pickup. Silent in projects
# that have no PROGRESS.md. Stdout becomes context for Claude (exit 0).
set -u

INPUT=$(cat)
SOURCE=$(printf '%s' "$INPUT" | jq -r '.source // "startup"' 2>/dev/null)

# A resumed or forked session already carries its conversation.
case "$SOURCE" in
  startup | clear | compact) ;;
  *) exit 0 ;;
esac

DIR=${CLAUDE_PROJECT_DIR:-$(printf '%s' "$INPUT" | jq -r '.cwd // empty' 2>/dev/null)}
PROGRESS="$DIR/PROGRESS.md"
[ -f "$PROGRESS" ] || exit 0

# An unfilled template carries no context worth injecting.
grep -q 'YYYY-MM-DD' "$PROGRESS" && exit 0

echo "[harness] PROGRESS.md (session source: $SOURCE)"
echo '-----'
head -n 60 "$PROGRESS"
echo '-----'
if [ -d "$DIR/docs/decisions" ]; then
  echo "[harness] ADR: $(ls "$DIR/docs/decisions" | grep -v '^0000-' | tr '\n' ' ')"
fi
echo '[harness] 作業を始める前に /pickup の手順に従うこと：TaskList で残タスクを確認し、「目的・完了条件・次の一手」を3行で復唱する。置き換え済みの ADR と却下した案は再提案しない。'
exit 0
