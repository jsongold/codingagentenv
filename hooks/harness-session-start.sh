#!/bin/bash
# SessionStart hook: inject the handoff context so a new or cleared session
# starts with it even when the user forgets /pickup. Source is
# .claude/handoff/<name>.md (one per session): a single valid handoff is
# injected in full, several are listed (one line each) so /pickup <name> can
# choose. Falls back to PROGRESS.md when there is no handoff. Silent otherwise.
# Stdout becomes context for Claude (exit 0).
set -u

INPUT=$(cat)
SOURCE=$(printf '%s' "$INPUT" | jq -r '.source // "startup"' 2>/dev/null)

# A resumed or forked session already carries its conversation.
case "$SOURCE" in
  startup | clear | compact) ;;
  *) exit 0 ;;
esac

DIR=${CLAUDE_PROJECT_DIR:-$(printf '%s' "$INPUT" | jq -r '.cwd // empty' 2>/dev/null)}

# A handoff whose 最終更新 is older than STALE_DAYS describes work that has moved
# on: it is named in one line instead of injected. HARNESS_NOW pins "now" for tests.
STALE_DAYS=7
day_epoch() { date -j -f %Y-%m-%d "$1" +%s 2>/dev/null || date -d "$1" +%s 2>/dev/null; }
is_stale() { # handoff file; an unreadable date is not stale
  d=$(grep -m1 '^- 最終更新' "$1" | grep -o '[0-9]\{4\}-[0-9][0-9]-[0-9][0-9]' | head -n 1)
  [ -n "$d" ] || return 1
  ts=$(day_epoch "$d") && [ -n "$ts" ] || return 1
  [ $(( (${HARNESS_NOW:-$(date +%s)} - ts) / 86400 )) -gt "$STALE_DAYS" ]
}

# Valid handoffs: .claude/handoff/*.md that are not an unfilled template
# (an unfilled template still contains the YYYY-MM-DD placeholder), newest first.
HDIR="$DIR/.claude/handoff"
VALID=''
COUNT=0
STALE=''
if [ -d "$HDIR" ]; then
  while IFS= read -r n; do
    case "$n" in *.md) ;; *) continue ;; esac
    [ -f "$HDIR/$n" ] || continue
    grep -q 'YYYY-MM-DD' "$HDIR/$n" && continue
    if is_stale "$HDIR/$n"; then
      STALE="$STALE ${n%.md}"
      continue
    fi
    VALID="$VALID$n
"
    COUNT=$((COUNT + 1))
  done <<EOF_LS
$(ls -t "$HDIR" 2>/dev/null)
EOF_LS
fi
if [ -n "$STALE" ]; then
  echo "[harness] 古い handoff は注入しない（最終更新から ${STALE_DAYS} 日超）：${STALE}。必要なら /pickup <name> で確認する"
fi

PICKUP='/pickup'
if [ "$COUNT" -ge 2 ]; then
  echo "[harness] handoff 一覧 (session source: $SOURCE)"
  while IFS= read -r n; do
    [ -n "$n" ] || continue
    f="$HDIR/$n"
    updated=$(grep -m1 '^- 最終更新' "$f" | sed -e 's/^- 最終更新：[[:space:]]*//' -e 's/^- 最終更新:[[:space:]]*//')
    purpose=$(awk '/^## 目的/ { found = 1; next } found && NF { print; exit }' "$f")
    echo "- ${n%.md} | 最終更新: $updated | 目的: $purpose"
  done <<EOF_LIST
$VALID
EOF_LIST
  echo '[harness] 複数の handoff がある。/pickup <name> で読む対象を選ぶこと。ユーザーが指定するまで、どれか1つを自分のものと決めない。'
  PICKUP='/pickup <name>'
elif [ "$COUNT" -eq 1 ]; then
  n=${VALID%%
*}
  echo "[harness] handoff: ${n%.md} (session source: $SOURCE)"
  echo '-----'
  head -n 60 "$HDIR/$n"
  echo '-----'
else
  # Backward compatibility: no handoff, fall back to PROGRESS.md.
  PROGRESS="$DIR/PROGRESS.md"
  [ -f "$PROGRESS" ] || exit 0
  # An unfilled template carries no context worth injecting.
  grep -q 'YYYY-MM-DD' "$PROGRESS" && exit 0
  echo "[harness] PROGRESS.md (session source: $SOURCE)"
  echo '-----'
  head -n 60 "$PROGRESS"
  echo '-----'
fi

echo "[harness] 作業を始める前に $PICKUP の手順に従うこと：handoff と GitHub Issues で残タスクを確認し、「目的・完了条件・次の一手」を3行で復唱する。"
exit 0
