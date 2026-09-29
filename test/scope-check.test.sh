#!/bin/bash
# Tests for hooks/harness-scope-check.ts (PreToolUse Bash hook).
# Run: bash test/scope-check.test.sh
set -u

ROOT=$(cd "$(dirname "$0")/.." && pwd)
HOOK=$ROOT/hooks/harness-scope-check.ts
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT
FAILED=0
check() { # name, expected, actual
  if [ "$2" = "$3" ]; then echo "ok   - $1"; else echo "FAIL - $1 (expected '$2', got '$3')"; FAILED=1; fi
}
# run <command string>: feed a PreToolUse payload, print "<exit>|<stderr>"
run() {
  local err code
  err=$(jq -n --arg c "$1" --arg d "$TMP" \
    '{hook_event_name:"PreToolUse",tool_name:"Bash",cwd:$d,tool_input:{command:$c}}' |
    node "$HOOK" 2>&1 >/dev/null)
  code=$?
  echo "$code|$err"
}
BLOCK='2|本文に「追加・変更するもの」（例: `- 追加: skill /foo`）を書いてから作る'

# Pass
check "pr create with list in --body" "0|" "$(run 'gh pr create --title t --body "目的：x
## 追加・変更するもの
- 追加: hook `harness-scope-check`"')"
check "pr create with list as first line of --body" "0|" "$(run 'gh pr create -b "- 変更: skill `/dispatch`"')"
check "issue create with list in heredoc" "0|" "$(run "gh issue create --title t --body \"\$(cat <<'EOF'
目的：x
- 削除：hook \`old\`
EOF
)\"")"
printf '目的：x\n- 追加: skill `/foo`\n' >"$TMP/body.md"
check "--body-file with list" "0|" "$(run 'gh pr create --title t --body-file body.md')"
check "-F absolute path with list" "0|" "$(run "gh issue create -F $TMP/body.md")"
check "non-gh command" "0|" "$(run 'ls -la')"
check "gh pr view is not a target" "0|" "$(run 'gh pr view 1')"
check "unparseable stdin" "0|" "$(echo 'not json' | node "$HOOK" 2>&1; echo "$?|")"

# Block
check "pr create without list" "$BLOCK" "$(run 'gh pr create --title t --body "目的：x"')"
check "issue create with list missing backticks" "$BLOCK" "$(run 'gh issue create --body "- 追加: skill foo"')"
printf '目的：x\n' >"$TMP/nolist.md"
check "--body-file without list" "$BLOCK" "$(run 'gh pr create --body-file nolist.md --body "- 追加: skill `x`"')"
check "--body-file missing file" "$BLOCK" "$(run 'gh pr create --body-file nope.md')"
check "--body-file - (stdin)" "$BLOCK" "$(run 'gh pr create --body-file -')"

exit $FAILED
