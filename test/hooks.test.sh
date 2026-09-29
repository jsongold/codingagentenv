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
# Fake gh: prints what `gh issue list ... --jq` would, or fails / hangs.
printf '#!/bin/sh\necho "#92 spec: global harness"\necho "#93 spec: deploy"\n' >"$TMP/bin/gh-ok"
printf '#!/bin/sh\necho partial; exit 1\n' >"$TMP/bin/gh-fail"
printf '#!/bin/sh\nsleep 5; echo "#1 late"\n' >"$TMP/bin/gh-slow"
chmod +x "$TMP/bin/"gh-*
printf '#!/bin/sh\necho "#7 label=$4"\n' >"$TMP/bin/gh-label"
chmod +x "$TMP/bin/gh-label"
export HARNESS_GH="$TMP/bin/gh-ok"
# Global harness.json lives under a throwaway HOME, never the real ~/.claude.
export HOME="$TMP/home"
mkdir -p "$HOME/.claude"
echo '{"spec":{"store":"issues","label":"doc:spec"}}' >"$HOME/.claude/harness.json"
# The spec list runs only in a git repo with a github.com remote.
gh_repo() { git init -q "$1" && git -C "$1" remote add origin "${2:-https://github.com/example/repo.git}"; }
gh_repo "$TMP/filled"
printf '# PROGRESS\n- 最終更新：YYYY-MM-DD HH:MM\n' >"$TMP/template/PROGRESS.md"

ss() { # source, project dir
  printf '{"source":"%s","cwd":"%s"}' "$1" "$2" | CLAUDE_PROJECT_DIR="$2" bash "$SS"
}

OUT=$(ss startup "$TMP/filled")
check "startup injects PROGRESS.md" 1 "$(printf '%s' "$OUT" | grep -c 'do the thing')"
check "startup lists doc:spec Issues" 2 "$(printf '%s' "$OUT" | grep -c '^- #9[23] spec: ')"
check "no gh: spec list is silent" 0 "$(HARNESS_GH="$TMP/bin/none" ss startup "$TMP/filled" | grep -c '^\[harness\] spec')"
check "gh failing: spec list is silent" 0 "$(HARNESS_GH="$TMP/bin/gh-fail" ss startup "$TMP/filled" | grep -c -e 'partial' -e '^\[harness\] spec')"
START=$(date +%s)
OUT=$(HARNESS_GH="$TMP/bin/gh-slow" HARNESS_SPEC_TIMEOUT=1 ss startup "$TMP/filled")
check "gh hanging: exits 0" 0 $?
check "gh hanging: gives up within the timeout" yes "$([ $(($(date +%s) - START)) -le 3 ] && echo yes || echo no)"
check "gh hanging: spec list is silent" 0 "$(printf '%s' "$OUT" | grep -c 'late')"
check "gh hanging: handoff still injected" 1 "$(printf '%s' "$OUT" | grep -c 'do the thing')"
check "startup points at /pickup" 1 "$(printf '%s' "$OUT" | grep -c '/pickup')"
check "clear injects" 1 "$(ss clear "$TMP/filled" | grep -c 'do the thing')"
check "compact injects" 1 "$(ss compact "$TMP/filled" | grep -c 'do the thing')"
check "clear lists specs" 2 "$(ss clear "$TMP/filled" | grep -c '^- #9[23] spec: ')"
check "compact skips the spec list" 0 "$(ss compact "$TMP/filled" | grep -c '^\[harness\] spec')"
mkdir -p "$TMP/nogit" "$TMP/gitlab"
cp "$TMP/filled/PROGRESS.md" "$TMP/nogit/" && cp "$TMP/filled/PROGRESS.md" "$TMP/gitlab/"
gh_repo "$TMP/gitlab" "git@gitlab.com:example/repo.git"
printf '#!/bin/sh\ntouch "%s/gh-called"\n' "$TMP" >"$TMP/bin/gh-spy" && chmod +x "$TMP/bin/gh-spy"
OUT=$(HARNESS_GH="$TMP/bin/gh-spy" ss startup "$TMP/nogit")
check "not a git repo: still injects" 1 "$(printf '%s' "$OUT" | grep -c 'do the thing')"
HARNESS_GH="$TMP/bin/gh-spy" ss startup "$TMP/gitlab" >/dev/null
HARNESS_GH="$TMP/bin/gh-spy" ss compact "$TMP/filled" >/dev/null
check "no git / non-github remote / compact: gh is not called" no "$([ -e "$TMP/gh-called" ] && echo yes || echo no)"
gh_repo "$TMP/sshgh" "git@github.com:example/repo.git" && cp "$TMP/filled/PROGRESS.md" "$TMP/sshgh/"
check "ssh github.com remote lists specs" 2 "$(ss startup "$TMP/sshgh" | grep -c '^- #9[23] spec: ')"
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
check "two handoffs: still lists specs" 2 "$(printf '%s' "$OUT" | grep -c '^- #9[23] spec: ')"
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

# --- SessionStart: spec store from harness.json (project > global > unset) ---
mkdir -p "$TMP/sp/.claude" "$TMP/sp/docs/decisions"
gh_repo "$TMP/sp"
printf '# PROGRESS\n- 最終更新：2026-09-20 11:00\n## 次の一手\n1. sp work\n' >"$TMP/sp/PROGRESS.md"
touch "$TMP/sp/docs/decisions/0001-a.md" "$TMP/sp/docs/decisions/0002-b.md"
OUT=$(HARNESS_GH="$TMP/bin/gh-label" ss startup "$TMP/sp")
check "global issues: passes the label to gh" 1 "$(printf '%s' "$OUT" | grep -c '^- #7 label=doc:spec$')"
check "global issues: no-repropose note" 1 "$(printf '%s' "$OUT" | grep -c '再提案しない')"
echo '{"spec":{"store":"issues","label":"proj:spec"}}' >"$TMP/sp/.claude/harness.json"
check "project issues label wins over global" 1 "$(HARNESS_GH="$TMP/bin/gh-label" ss startup "$TMP/sp" | grep -c '^- #7 label=proj:spec$')"
echo '{"spec":{"store":"files","dir":"docs/decisions"}}' >"$TMP/sp/.claude/harness.json"
OUT=$(ss startup "$TMP/sp")
check "project files: lists the files in dir" "- docs/decisions/0001-a.md - docs/decisions/0002-b.md" "$(printf '%s' "$OUT" | grep '^- docs/' | tr '\n' ' ' | sed 's/ $//')"
check "project files: heading" 1 "$(printf '%s' "$OUT" | grep -c '^\[harness\] spec（docs/decisions のファイル）:$')"
check "project files: gh is not called" 0 "$(printf '%s' "$OUT" | grep -c '#9[23]')"
echo '{"spec":{"store":null}}' >"$TMP/sp/.claude/harness.json"
OUT=$(ss startup "$TMP/sp")
check "project store null overrides global: no spec" 0 "$(printf '%s' "$OUT" | grep -c -e '^\[harness\] spec' -e '#9[23]' -e '再提案しない')"
check "project store null: context still injected" 1 "$(printf '%s' "$OUT" | grep -c 'sp work')"
echo '{"other":1}' >"$TMP/sp/.claude/harness.json"
check "project file without spec falls back to global" 2 "$(ss startup "$TMP/sp" | grep -c '^- #9[23] spec: ')"
rm "$TMP/sp/.claude/harness.json" "$HOME/.claude/harness.json"
OUT=$(ss startup "$TMP/sp")
check "unset: no spec list" 0 "$(printf '%s' "$OUT" | grep -c -e '^\[harness\] spec' -e '#9[23]' -e '再提案しない')"
check "unset: context still injected" 1 "$(printf '%s' "$OUT" | grep -c 'sp work')"
echo '{"spec":{"store":"files","dir":"docs/decisions"}}' >"$HOME/.claude/harness.json"
check "global files: lists the files" 2 "$(ss startup "$TMP/sp" | grep -c '^- docs/decisions/')"
mkdir -p "$TMP/cdir"
echo '{"spec":{"store":"issues","label":"cdir:spec"}}' >"$TMP/cdir/harness.json"
check "CLAUDE_DIR: global harness.json is read from there" 1 "$(CLAUDE_DIR="$TMP/cdir" HARNESS_GH="$TMP/bin/gh-label" ss startup "$TMP/sp" | grep -c '^- #7 label=cdir:spec$')"
echo 'not json' >"$HOME/.claude/harness.json"
check "invalid json: treated as unset" 0 "$(ss startup "$TMP/sp" | grep -c '^\[harness\] spec')"

exit $FAILED
