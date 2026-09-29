#!/bin/bash
# Tests for hooks/*.sh. Run: bash test/hooks.test.sh
set -u

ROOT=$(cd "$(dirname "$0")/.." && pwd)
SS="$ROOT/hooks/harness-session-start.sh"
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
mkdir -p "$TMP/filled" "$TMP/empty" "$TMP/template" "$TMP/bin"
printf '# PROGRESS\n- 最終更新：2026-09-20 11:00\n## 次の一手\n1. do the thing\n' >"$TMP/filled/PROGRESS.md"
# A gh on PATH that only records being called: the hook must never call it.
printf '#!/bin/sh\ntouch "%s/gh-called"\n' "$TMP" >"$TMP/bin/gh" && chmod +x "$TMP/bin/gh"
export PATH="$TMP/bin:$PATH"
gh_repo() { git init -q "$1" && git -C "$1" remote add origin "${2:-https://github.com/example/repo.git}"; }
gh_repo "$TMP/filled"
printf '# PROGRESS\n- 最終更新：YYYY-MM-DD HH:MM\n' >"$TMP/template/PROGRESS.md"

ss() { # source, project dir
  printf '{"source":"%s","cwd":"%s"}' "$1" "$2" | CLAUDE_PROJECT_DIR="$2" bash "$SS"
}

OUT=$(ss startup "$TMP/filled")
check "startup injects PROGRESS.md" 1 "$(printf '%s' "$OUT" | grep -c 'do the thing')"
check "startup points at /pickup" 1 "$(printf '%s' "$OUT" | grep -c '/pickup')"
check "startup lists no specs" 0 "$(printf '%s' "$OUT" | grep -c -e '^\[harness\] spec' -e '再提案しない')"
check "clear injects" 1 "$(ss clear "$TMP/filled" | grep -c 'do the thing')"
check "compact injects" 1 "$(ss compact "$TMP/filled" | grep -c 'do the thing')"
mkdir -p "$TMP/nogit"
cp "$TMP/filled/PROGRESS.md" "$TMP/nogit/"
OUT=$(ss startup "$TMP/nogit")
check "not a git repo: still injects" 1 "$(printf '%s' "$OUT" | grep -c 'do the thing')"
ss clear "$TMP/filled" >/dev/null
check "github repo on startup / clear / compact: gh is not called" no "$([ -e "$TMP/gh-called" ] && echo yes || echo no)"
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
gh_repo "$TMP/h2"
OUT=$(ss startup "$TMP/h2")
check "two handoffs: list heading" 1 "$(printf '%s' "$OUT" | grep -c '^\[harness\] handoff 一覧 (session source: startup)$')"
check "two handoffs: alpha line" 1 "$(printf '%s' "$OUT" | grep -c '^- alpha | 最終更新: 2026-09-20 10:00 | 目的: alpha purpose$')"
check "two handoffs: beta line" 1 "$(printf '%s' "$OUT" | grep -c '^- beta | 最終更新: 2026-09-21 09:00 | 目的: beta purpose$')"
check "two handoffs: newest first" "beta alpha" "$(printf '%s' "$OUT" | grep '^- [a-z0-9-]* | ' | sed 's/^- \([a-z0-9-]*\) .*/\1/' | tr '\n' ' ' | sed 's/ $//')"
check "two handoffs: body is not injected" 0 "$(printf '%s' "$OUT" | grep -c 'secret body')"
check "two handoffs: asks for /pickup <name> in the list note" 1 "$(printf '%s' "$OUT" | grep -c '^.harness. 複数の handoff がある。/pickup <name>')"
check "two handoffs: closing prompt uses /pickup <name>" 1 "$(printf '%s' "$OUT" | grep -c '作業を始める前に /pickup <name> の手順')"
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

exit $FAILED
