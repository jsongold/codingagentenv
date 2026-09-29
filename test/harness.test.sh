#!/bin/bash
# Tests for bin/codingenv against a throwaway CLAUDE_DIR. Run: bash test/harness.test.sh
set -u

ROOT=$(cd "$(dirname "$0")/.." && pwd)
TMP=$(mktemp -d)
trap 'rm -r "$TMP"' EXIT
FAILED=0
mkdir -p "$TMP/bin"
PATH="$TMP/bin:$PATH" # install warns when BIN_DIR is not on PATH

check() { # name, expected, actual
  if [ "$2" = "$3" ]; then
    echo "ok   - $1"
  else
    echo "FAIL - $1 (expected '$2', got '$3')"
    FAILED=1
  fi
}

harness() { CLAUDE_DIR="$FAKE" BIN_DIR="$TMP/bin" bash "$ROOT/bin/codingenv" "$@"; }

# A machine that already has its own settings, hooks and CLAUDE.md, including a
# harness section written by hand before markers existed.
FAKE="$TMP/claude"
mkdir -p "$FAKE/skills/mine"
# It also carries what an older install registered (#103 retired them).
mkdir -p "$FAKE/hooks"
ln -s "$ROOT/hooks/harness-task-completed.sh" "$FAKE/hooks/harness-task-completed.sh"
echo '{"model":"x","env":{"KEEP":"1","CLAUDE_CODE_ENABLE_TODO_TOOLS":"1"},"hooks":{"SessionStart":[{"hooks":[{"type":"command","command":"existing"}]}],"TaskCompleted":[{"hooks":[{"type":"command","command":"bash \"$HOME/.claude/hooks/harness-task-completed.sh\"","timeout":10}]},{"hooks":[{"type":"command","command":"mine"}]},{"hooks":[{"type":"command","command":"bash \"$HOME/.claude/hooks/harness-task-completed.sh\""},{"type":"command","command":"keep-me"}]}]}}' >"$FAKE/settings.json"
printf '# Rules\n\n## 行動ルール\n- a\n\n## ハーネス（全プロジェクト共通）\n\n- stale line\n\n## レスポンススタイル\n- b\n' >"$FAKE/CLAUDE.md"

harness status >/dev/null 2>&1
check "status fails before install" 1 $?

harness install >/dev/null
harness install >/dev/null
harness status >/dev/null
check "status passes after install" 0 $?

check "command is linked" "$ROOT/bin/codingenv" "$(readlink "$TMP/bin/codingenv")"
CLAUDE_DIR="$FAKE" BIN_DIR="$TMP/bin" "$TMP/bin/codingenv" status >/dev/null 2>&1
check "linked command finds the repo" 0 $?
check "tools are linked" "$ROOT/tools/codex-probe" "$(readlink "$TMP/bin/codex-probe")"
check "skills are symlinked" "$ROOT/skills/dispatch" "$(readlink "$FAKE/skills/dispatch")"
check "other skills are untouched" yes "$([ -d "$FAKE/skills/mine" ] && echo yes)"
check "hook scripts are symlinked" "$ROOT/hooks/harness-session-start.sh" "$(readlink "$FAKE/hooks/harness-session-start.sh")"
check "harness.json is symlinked" "$ROOT/global/harness.json" "$(readlink "$FAKE/harness.json")"
check "retired hook link is removed" no "$([ -L "$FAKE/hooks/harness-task-completed.sh" ] && echo yes || echo no)"
check "existing hooks are kept" existing "$(jq -r '.hooks.SessionStart[0].hooks[0].command' "$FAKE/settings.json")"
check "SessionStart registered once" 2 "$(jq '.hooks.SessionStart | length' "$FAKE/settings.json")"
check "retired TaskCompleted entry is unregistered" '[{"hooks":[{"type":"command","command":"mine"}]},{"hooks":[{"type":"command","command":"keep-me"}]}]' "$(jq -c '.hooks.TaskCompleted' "$FAKE/settings.json")"
check "retired env is removed" null "$(jq -r '.env.CLAUDE_CODE_ENABLE_TODO_TOOLS' "$FAKE/settings.json")"
check "other env is kept" 1 "$(jq -r '.env.KEEP' "$FAKE/settings.json")"
check "unrelated keys are kept" x "$(jq -r '.model' "$FAKE/settings.json")"
check "hand-written section is replaced" 0 "$(grep -c 'stale line' "$FAKE/CLAUDE.md")"
check "section appears once" 1 "$(grep -c '^## ハーネス' "$FAKE/CLAUDE.md")"
check "section comes from the repo" 1 "$(grep -c 'bin/codingenv install' "$FAKE/CLAUDE.md")"
check "other sections are kept" 2 "$(grep -c -e '^- a$' -e '^- b$' "$FAKE/CLAUDE.md")"
check "backups are written" yes "$(ls "$FAKE" | grep -q 'settings.json.bak-.*-harness' && echo yes)"

# status reports leftovers of the retired TaskCompleted hook as drift.
cp "$FAKE/settings.json" "$TMP/settings.clean"
jq '.hooks.TaskCompleted += [{hooks: [{type: "command", command: "bash \"$HOME/.claude/hooks/harness-task-completed.sh\""}]}]' "$TMP/settings.clean" >"$FAKE/settings.json"
harness status >/dev/null 2>&1
check "status detects a retired hook registration" 1 $?
cp "$TMP/settings.clean" "$FAKE/settings.json"
ln -s "$ROOT/hooks/harness-task-completed.sh" "$FAKE/hooks/harness-task-completed.sh"
harness status >/dev/null 2>&1
check "status detects a retired hook link" 1 $?
rm "$FAKE/hooks/harness-task-completed.sh"
harness status >/dev/null
check "status passes once leftovers are gone" 0 $?

# status reports a missing harness.json link as drift.
rm "$FAKE/harness.json"
harness status >/dev/null 2>&1
check "status detects a missing harness.json link" 1 $?
harness install >/dev/null

sed -i.orig 's/^- 1ファイル.*$/- edited by hand/' "$FAKE/CLAUDE.md"
harness status >/dev/null 2>&1
check "status detects a hand-edited section" 1 $?
harness install >/dev/null
check "install repairs it" 0 "$(grep -c 'edited by hand' "$FAKE/CLAUDE.md")"

harness uninstall >/dev/null
check "uninstall removes the command link" no "$([ -e "$TMP/bin/codingenv" ] && echo yes || echo no)"
check "uninstall removes tool links" no "$([ -e "$TMP/bin/codex-probe" ] && echo yes || echo no)"
check "uninstall removes skill links" no "$([ -e "$FAKE/skills/dispatch" ] && echo yes || echo no)"
check "uninstall removes hook links" no "$([ -e "$FAKE/hooks/harness-session-start.sh" ] && echo yes || echo no)"
check "uninstall removes the harness.json link" no "$([ -e "$FAKE/harness.json" ] || [ -L "$FAKE/harness.json" ] && echo yes || echo no)"
check "uninstall keeps existing hooks" existing "$(jq -r '.hooks.SessionStart[0].hooks[0].command' "$FAKE/settings.json")"
check "uninstall drops its SessionStart group" 1 "$(jq '.hooks.SessionStart | length' "$FAKE/settings.json")"
check "uninstall keeps others' TaskCompleted" 2 "$(jq '.hooks.TaskCompleted | length' "$FAKE/settings.json")"
check "uninstall drops its env only" '{"KEEP":"1"}' "$(jq -c '.env' "$FAKE/settings.json")"
check "uninstall removes the section" 0 "$(grep -c '^## ハーネス' "$FAKE/CLAUDE.md")"
check "uninstall keeps other sections" 2 "$(grep -c -e '^- a$' -e '^- b$' "$FAKE/CLAUDE.md")"

# A fresh machine with nothing in place.
FAKE="$TMP/fresh"
harness install >/dev/null
harness status >/dev/null
check "install works on an empty directory" 0 $?
check "fresh install registers no TaskCompleted" null "$(jq '.hooks.TaskCompleted' "$FAKE/settings.json")"
check "fresh install adds no env" null "$(jq '.env' "$FAKE/settings.json")"

# uninstall still unlinks hook scripts when settings.json is gone.
rm "$FAKE/settings.json"
ln -s "$ROOT/hooks/harness-task-completed.sh" "$FAKE/hooks/harness-task-completed.sh"
harness uninstall >/dev/null
check "uninstall without settings unlinks hooks" no "$([ -e "$FAKE/hooks/harness-session-start.sh" ] && echo yes || echo no)"
check "uninstall without settings unlinks retired hooks" no "$([ -L "$FAKE/hooks/harness-task-completed.sh" ] && echo yes || echo no)"
check "uninstall without settings creates none" no "$([ -e "$FAKE/settings.json" ] && echo yes || echo no)"

# A hand-written harness.json is kept: install skips it, uninstall leaves it.
FAKE="$TMP/ownconf"
mkdir -p "$FAKE"
echo '{"spec":{"store":"files","dir":"d"}}' >"$FAKE/harness.json"
harness install >/dev/null 2>&1
check "install keeps a regular harness.json" files "$(jq -r .spec.store "$FAKE/harness.json")"
harness status >/dev/null 2>&1
check "status reports a regular harness.json as drift" 1 $?
harness uninstall >/dev/null
check "uninstall keeps a regular harness.json" yes "$([ -f "$FAKE/harness.json" ] && [ ! -L "$FAKE/harness.json" ] && echo yes)"

# An unrelated file already named harness must not be overwritten.
FAKE="$TMP/blocked"
rm -f "$TMP/bin/codingenv"
echo mine >"$TMP/bin/codingenv"
harness install >/dev/null 2>&1
check "install refuses to clobber a regular file" 1 $?
check "the regular file is untouched" mine "$(cat "$TMP/bin/codingenv")"
check "nothing else was installed" no "$([ -e "$FAKE/skills" ] && echo yes || echo no)"

harness bogus >/dev/null 2>&1
check "unknown subcommand fails" 1 $?

exit $FAILED
