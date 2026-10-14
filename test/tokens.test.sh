#!/bin/bash
# Tests for agent/tokens (tools/tokens).
# Run: bash test/tokens.test.sh
set -u

ROOT=$(cd "$(dirname "$0")/.." && pwd)
FIX=$ROOT/test/fixtures/tokens
FAILED=0
check() { # name, expected, actual
  if [ "$2" = "$3" ]; then echo "ok   - $1"; else echo "FAIL - $1 (expected '$2', got '$3')"; FAILED=1; fi
}
has() { # name, haystack, needle
  case "$2" in *"$3"*) echo "ok   - $1" ;; *) echo "FAIL - $1 (missing '$3' in: $2)"; FAILED=1 ;; esac
}
tokens() { "$ROOT/tools/tokens" --root "$FIX" "$@" 2>&1; }
field() { node -p "JSON.parse(process.argv[1]).$1" "$json"; }

# --json: parses, one unit per transcript, totals per unit and overall
json=$(tokens sess-a --json)
check "json: parses, 2 units" "2" "$(field units.length)"
check "session: unit" "session" "$(field 'units[0].unit')"
check "session: calls" "2" "$(field 'units[0].calls')"
check "session: input" "15" "$(field 'units[0].input')"
check "session: cacheRead" "100" "$(field 'units[0].cacheRead')"
check "session: cacheCreation" "20" "$(field 'units[0].cacheCreation')"
check "session: total" "192" "$(field 'units[0].total')"
check "session: models" "claude-opus-5-5" "$(field 'units[0].models.join()')"
# msg_1/req_1 is written twice (output_tokens 1 -> 50): 50 wins, plus 7 from msg_2
check "dedup: largest output_tokens wins" "57" "$(field 'units[0].output')"

# the subagent is its own unit; msg_2/req_2 also appears there but stays with the session
check "subagent: unit" "subagent" "$(field 'units[1].unit')"
check "subagent: agent" "x" "$(field 'units[1].agent')"
check "subagent: description" "count things" "$(field 'units[1].description')"
check "subagent: calls" "1" "$(field 'units[1].calls')"
check "subagent: total" "39" "$(field 'units[1].total')"
check "total: calls" "3" "$(field total.calls)"
check "total: input" "18" "$(field total.input)"
check "total: output" "61" "$(field total.output)"
check "total: total" "231" "$(field total.total)"

# table: one row per unit + a total row, no transcript body
out=$(tokens sess-a)
check "table: header + 2 units + total" "4" "$(printf '%s\n' "$out" | wc -l | tr -d ' ')"
has "table: session row" "$out" "session   sess-a"
has "table: subagent row" "$out" "subagent  sess-a   x"
has "table: subagent description" "$out" "count things"
has "table: total row" "$(printf '%s\n' "$out" | tail -1)" "231"
check "table: no transcript body" "0" "$(printf '%s\n' "$out$json" | grep -c hello-body)"

# a directory target gives the same result as the session id
check "dir target: same as session id" "$out" "$(tokens "$FIX/proj")"

# nothing to count
check "unknown session id exits 1" "1" "$("$ROOT/tools/tokens" --root "$FIX" nope >/dev/null 2>&1; echo $?)"
has "unknown session id: usage" "$(tokens nope)" "usage: tokens"
check "no arguments exits 1" "1" "$("$ROOT/tools/tokens" >/dev/null 2>&1; echo $?)"

# --by group: one row per group. A call is attributed by its first tool call; its blocks may
# be split over several rows, so all rows of the call count for the tool list.
GFIX=$ROOT/test/fixtures/tokens-groups
gjson=$("$ROOT/tools/tokens" --root "$GFIX" --by group sess-g --json 2>&1)
gfield() { node -p "JSON.parse(process.argv[1]).$1" "$gjson"; }
check "group: 4 groups" "4" "$(gfield groups.length)"
check "group: largest first" "Read:code" "$(gfield 'groups[0].group')"
check "group: Bash row merges the call's blocks, first tool wins" "Bash:git" "$(gfield 'groups[1].group')"
check "group: Bash:git total" "162" "$(gfield 'groups[1].total')"
check "group: MCP atlassian is jira" "jira" "$(gfield 'groups[2].group')"
check "group: text-only call" "text" "$(gfield 'groups[3].group')"
check "group: calls total" "4" "$(gfield total.calls)"
check "group: total" "438" "$(gfield total.total)"

exit $FAILED
