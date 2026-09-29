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

# Valid handoffs: .claude/handoff/*.md that are not an unfilled template
# (an unfilled template still contains the YYYY-MM-DD placeholder), newest first.
HDIR="$DIR/.claude/handoff"
VALID=''
COUNT=0
if [ -d "$HDIR" ]; then
  while IFS= read -r n; do
    case "$n" in *.md) ;; *) continue ;; esac
    [ -f "$HDIR/$n" ] || continue
    grep -q 'YYYY-MM-DD' "$HDIR/$n" && continue
    VALID="$VALID$n
"
    COUNT=$((COUNT + 1))
  done <<EOF_LS
$(ls -t "$HDIR" 2>/dev/null)
EOF_LS
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

# Ticket system from harness.json (where tickets live, and how a spec is told
# apart among them). The project's .claude/harness.json wins over
# ${CLAUDE_DIR:-~/.claude}/harness.json (CLAUDE_DIR as in bin/codingenv, the
# global default); the first file with a "tickets" key decides. Nothing is
# validated, since each environment writes it its own way; values are used as
# read. Prints "github<TAB><spec label or empty>", "other<TAB><system><TAB>
# <project or empty>" for any other system, or nothing when unset (no
# "tickets", system null, or a file jq cannot read). Unknown keys are ignored.
TICKETS_JQ='
  (if type == "object" and has("tickets") then .tickets else empty end) as $t
  | ($t | if type == "object" then .system else null end) as $s
  | if $s == null then "off"
    elif $s == "github" then "github\t\(($t.spec | objects | .label | strings) // "")"
    else "other\t\($s | tostring)\t\(($t.project // "") | tostring)" end'
tickets_conf() {
  local f r
  for f in "$DIR/.claude/harness.json" "${CLAUDE_DIR:-$HOME/.claude}/harness.json"; do
    [ -f "$f" ] || continue
    r=$(jq -r "$TICKETS_JQ" "$f" 2>/dev/null) || return 0
    [ -n "$r" ] || continue
    [ "$r" = off ] || echo "$r"
    return 0
  done
  return 0
}

# Open spec Issues (#103). Best effort: no gh, no network, no label or a slow
# answer all print nothing. HARNESS_GH and HARNESS_SPEC_TIMEOUT (seconds)
# exist for the tests.
# gh goes over the network, so it runs only on startup / clear (compact keeps
# the session going) and only in a git repo with a github.com remote.
spec_issues() { # label
  local gh=${HARNESS_GH:-gh} limit=$((${HARNESS_SPEC_TIMEOUT:-3} * 10)) out pid i=0
  [ "$SOURCE" = compact ] && return 0
  git -C "$DIR" remote -v 2>/dev/null | grep -q 'github\.com[:/]' || return 0
  command -v "$gh" >/dev/null 2>&1 || return 0
  out=$(mktemp 2>/dev/null) || return 0
  (cd "$DIR" 2>/dev/null || exit 1; exec "$gh" issue list --label "$1" --state open --limit 50 \
    --json number,title --jq '.[] | "#\(.number) \(.title)"' >"$out" 2>/dev/null) &
  pid=$!
  while kill -0 "$pid" 2>/dev/null; do
    [ "$i" -ge "$limit" ] && { kill "$pid" 2>/dev/null; : >"$out"; break; }
    sleep 0.1
    i=$((i + 1))
  done
  wait "$pid" 2>/dev/null || : >"$out"
  if [ -s "$out" ]; then
    echo "[harness] spec（label $1 の open Issue。gh issue view <番号> で読む）:"
    sed 's/^/- /' "$out"
  fi
  rm -f "$out"
}

CONF=$(tickets_conf)
TICKETS='GitHub Issues' SPEC_NOTE=''
case "$CONF" in
  github*)
    LABEL=${CONF#github	}
    if [ -n "$LABEL" ]; then
      spec_issues "$LABEL"
      SPEC_NOTE=' spec の置き換え済みの決定と却下した案は再提案しない。'
    fi
    ;;
  other*)
    REST=${CONF#other	}
    SYSTEM=${REST%%	*} PROJECT=${REST#*	}
    TICKETS=$SYSTEM
    [ -n "$PROJECT" ] && TICKETS="$SYSTEM ($PROJECT)"
    echo "[harness] チケット管理: ${TICKETS}。一覧取得は未対応"
    ;;
esac
echo "[harness] 作業を始める前に $PICKUP の手順に従うこと：handoff と $TICKETS で残タスクを確認し、「目的・完了条件・次の一手」を3行で復唱する。$SPEC_NOTE"
exit 0
