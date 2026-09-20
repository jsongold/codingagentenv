#!/bin/bash
# Registers the harness hooks globally. Idempotent. Run: bash hooks/install.sh
# CLAUDE_DIR overrides the target directory (used by the tests).
set -eu

ROOT=$(cd "$(dirname "$0")/.." && pwd)
CLAUDE_DIR=${CLAUDE_DIR:-$HOME/.claude}
SETTINGS="$CLAUDE_DIR/settings.json"

command -v jq >/dev/null || { echo "jq is required" >&2; exit 1; }

mkdir -p "$CLAUDE_DIR/hooks"
for name in harness-session-start.sh harness-task-completed.sh; do
  ln -sfn "$ROOT/hooks/$name" "$CLAUDE_DIR/hooks/$name"
done

[ -f "$SETTINGS" ] || echo '{}' >"$SETTINGS"
cp "$SETTINGS" "$SETTINGS.bak-$(date +%Y%m%d-%H%M%S)-pre-harness-hooks"

# Append one group per event unless a group already runs the same script.
add_hook() { # event, script name, timeout
  local command="bash \"\$HOME/.claude/hooks/$2\""
  jq --arg event "$1" --arg command "$command" --argjson timeout "$3" '
    .hooks[$event] //= []
    | if any(.hooks[$event][]?.hooks[]?; .command == $command) then .
      else .hooks[$event] += [{hooks: [{type: "command", command: $command, timeout: $timeout}]}]
      end
  ' "$SETTINGS" >"$SETTINGS.tmp"
  mv "$SETTINGS.tmp" "$SETTINGS"
}

add_hook SessionStart harness-session-start.sh 10
add_hook TaskCompleted harness-task-completed.sh 10

echo "installed: SessionStart, TaskCompleted -> $SETTINGS"
