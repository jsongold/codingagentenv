#!/bin/bash
# Tests for skills/cl/cl.ts.
# Run: bash test/cl.test.sh
set -u

ROOT=$(cd "$(dirname "$0")/.." && pwd)
CL=$ROOT/skills/cl/cl.ts
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT
FAILED=0
check() { # name, expected, actual
  if [ "$2" = "$3" ]; then echo "ok   - $1"; else echo "FAIL - $1 (expected '$2', got '$3')"; FAILED=1; fi
}
has() { # name, haystack, needle
  case "$2" in *"$3"*) echo "ok   - $1" ;; *) echo "FAIL - $1 (missing '$3' in: $2)"; FAILED=1 ;; esac
}

A=$TMP/repo/sessA/scratchpad
B=$TMP/repo/sessB/scratchpad
mkdir -p "$A" "$B"
cl() { node "$CL" "$@" 2>&1; }

out=$(cl "$A" new rel --purpose "ship it" --deadline 2026-10-31)
has "new: header" "$out" "rel — ship it · due 2026-10-31"
check "new: no decisions line" "0" "$(printf '%s' "$out" | grep -c '^Decisions:')"
has "new: legend" "$out" "✓ decided/done · todo – n/a"
check "new: no links footer" "0" "$(printf '%s' "$out" | grep -c '^\[1\]')"
check "new twice fails" "1" "$(node "$CL" "$A" new rel --purpose x >/dev/null 2>&1; echo $?)"

cl "$A" add rel P0 "first" >/dev/null
out=$(cl "$A" add rel P1 "second")
has "add: ids increment" "$out" "|  2 | P1 | second |  ·   |  ·   |"
has "add: P0 sorted before P1" "$(printf '%s' "$out" | grep -n 'first' | cut -d: -f1)" "$(( $(printf '%s' "$out" | grep -n 'second' | cut -d: -f1) - 1 ))"
check "add: bad priority fails" "1" "$(node "$CL" "$A" add rel P9 x >/dev/null 2>&1; echo $?)"

out=$(cl "$A" spec rel 1 decided)
has "spec" "$out" "|  1 | P0 | first  |  ✓   |  ·   |"
out=$(cl "$A" impl rel 1 n/a)
has "impl: n/a mark" "$out" "|  1 | P0 | first  |  ✓   |  –   |"
out=$(cl "$A" impl rel 1 done)
has "impl: done mark" "$out" "|  1 | P0 | first  |  ✓   |  ✓   |"
cl "$A" impl rel 1 n/a >/dev/null
check "impl: bad state fails" "1" "$(node "$CL" "$A" impl rel 1 nope >/dev/null 2>&1; echo $?)"
check "spec: unknown item fails" "1" "$(node "$CL" "$A" spec rel 99 todo >/dev/null 2>&1; echo $?)"

out=$(cl "$A" link rel 2 PR http://x/1)
has "link: number in column" "$out" "| [1]   |"
has "link: footer" "$out" "[1] PR http://x/1"
out=$(cl "$A" decide rel "use plan B")
has "decide" "$out" "Decisions: $(date +%m-%d) use plan B"

# show / list across sessions
cl "$B" new other --purpose "b side" >/dev/null
cl "$B" new rel --purpose "b rel" >/dev/null
has "show: own first" "$(cl "$A" show rel)" "rel — ship it"
out=$(cl "$A" show other)
has "show: other session" "$out" "other — b side"
out=$(cl "$A" show rel)
has "show: notes duplicates" "$out" "1 other checklist(s) named rel"
check "show: missing fails" "1" "$(node "$CL" "$A" show nope >/dev/null 2>&1; echo $?)"

out=$(cl "$A" list)
has "list: header" "$out" "| Name | Purpose | Open | Updated | This session |"
has "list: own row" "$out" "| rel | ship it | 1 |"
has "list: other row" "$out" "| other | b side | 0 |"
check "list: empty" "No checklists." "$(cl "$TMP/none/x/scratchpad" list)"

# store: URL validation, log, sync (gh stubbed), checkpoint
S=$TMP/s/sessS/scratchpad
mkdir -p "$S" "$TMP/bin"
cat > "$TMP/bin/gh" <<'STUB'
#!/bin/bash
a="$*"; echo "${a//$'\n'/ }" >> "$GH_CALLS"
echo '{"id": 555}'
STUB
chmod +x "$TMP/bin/gh"
export GH_CALLS=$TMP/gh_calls
: > "$GH_CALLS"
clg() { PATH="$TMP/bin:$PATH" node "$CL" "$@" 2>&1; }
field() { node -p "require('$S/cl/$1.json').$2"; }

cl "$S" new st --purpose "store test" >/dev/null
check "store: other url fails" "1" "$(node "$CL" "$S" store st https://example.com/x >/dev/null 2>&1; echo $?)"
check "store: missing url fails" "1" "$(node "$CL" "$S" store st >/dev/null 2>&1; echo $?)"
check "sync without store fails" "1" "$(node "$CL" "$S" sync st >/dev/null 2>&1; echo $?)"
check "new --store: bad url fails" "1" "$(node "$CL" "$S" new bad --purpose x --store nope >/dev/null 2>&1; echo $?)"
cl "$S" store st https://github.com/o/r/issues/7 >/dev/null
check "store: github kind" "github" "$(field st store.kind)"
cl "$S" add st P0 "one" >/dev/null
cl "$S" impl st 1 done >/dev/null
check "log: one entry per write" "4" "$(field st log.length)"
has "log: before -> after" "$(field st 'log[3].change')" "#1: todo -> done"
has "list: pending column" "$(cl "$S" list)" "| Pending |"
has "list: pending count" "$(cl "$S" list)" "| yes | 4 |"

cl "$S" checkpoint st >/dev/null
check "checkpoint: entry added" "5" "$(field st log.length)"
check "checkpoint: stays pending" "0" "$(field st synced)"
check "checkpoint: no gh calls" "0" "$(wc -l < "$GH_CALLS" | tr -d ' ')"

out=$(clg "$S" sync st)
has "sync github: reports" "$out" "synced 5 log entries"
check "sync github: synced" "5" "$(field st synced)"
check "sync github: commentId" "555" "$(field st store.commentId)"
has "sync github: managed comment created" "$(sed -n 1p "$GH_CALLS")" "-X POST repos/o/r/issues/7/comments"
check "sync github: 1 managed + 5 entries" "6" "$(wc -l < "$GH_CALLS" | tr -d ' ')"
cl "$S" decide st "again" >/dev/null
: > "$GH_CALLS"
clg "$S" sync st >/dev/null
has "sync github: managed comment edited" "$(sed -n 1p "$GH_CALLS")" "-X PATCH repos/o/r/issues/comments/555"
check "sync github: only pending posted" "2" "$(wc -l < "$GH_CALLS" | tr -d ' ')"
check "sync github: synced again" "6" "$(field st synced)"

# gh failure marks nothing synced
printf '#!/bin/bash\nexit 1\n' > "$TMP/bin/gh"
cl "$S" decide st "fail case" >/dev/null
check "sync github: gh failure exits 1" "1" "$(PATH="$TMP/bin:$PATH" node "$CL" "$S" sync st >/dev/null 2>&1; echo $?)"
check "sync github: failure keeps synced" "6" "$(field st synced)"

# jira: payload only, then synced
cl "$S" new jr --purpose "jira test" --store https://x.atlassian.net/browse/AB-12 >/dev/null
payload=$(node "$CL" "$S" sync jr)
check "sync jira: kind" "jira" "$(node -p "JSON.parse(process.argv[1]).kind" "$payload")"
check "sync jira: pending" "1" "$(node -p "JSON.parse(process.argv[1]).pending.length" "$payload")"
check "sync jira: next" "1" "$(node -p "JSON.parse(process.argv[1]).next" "$payload")"
check "sync jira: changes nothing" "0" "$(field jr synced)"
cl "$S" synced jr 1 c-9 >/dev/null
check "synced: count" "1" "$(field jr synced)"
check "synced: commentId" "c-9" "$(field jr store.commentId)"
check "synced: out of range fails" "1" "$(node "$CL" "$S" synced jr 99 >/dev/null 2>&1; echo $?)"

# old files without log/synced still load
echo '{"name":"old","purpose":"p","decisions":[],"items":[]}' > "$S/cl/old.json"
has "load: old file" "$(cl "$S" show old)" "old — p"

# multi-word args are joined
out=$(cl "$A" add rel P2 drop e2e tests)
has "add: joins title" "$out" "|  3 | P2 | drop e2e tests |"
out=$(cl "$A" link rel 3 my PR label http://x/2)
has "link: joins label" "$out" "[2] my PR label http://x/2"
out=$(cl "$A" decide rel drop the e2e)
has "decide: joins text" "$out" "use plan B · $(date +%m-%d) drop the e2e"

# CJK titles count as width 2 when padding
cl "$A" new cjk --purpose p >/dev/null
cl "$A" add cjk P0 "日本語" >/dev/null
out=$(cl "$A" add cjk P0 "abcdef")
has "pad: cjk width" "$out" "|  1 | P0 | 日本語 |"
has "pad: ascii width" "$out" "|  2 | P0 | abcdef |"

# escaping
out=$(cl "$A" add rel P2 "a|b")
has "escape: pipe in title" "$out" 'a\|b'
out=$(cl "$A" link rel 3 "x]y" http://x/3)
has "link: footer numbering follows table order" "$out" "[3] x]y http://x/3"
cl "$A" new esc --purpose "p|q" >/dev/null
out=$(cl "$A" list)
has "escape: pipe in list purpose" "$out" 'p\|q'

# flag(): a flag-like or empty value counts as missing
out=$(cl "$A" new f1 --purpose --deadline 2026-10-31)
has "flag: flag-like value is missing" "$out" "new requires --purpose"
out=$(cl "$A" new f2 --purpose "")
has "flag: empty value is missing" "$out" "new requires --purpose"

# broken files elsewhere do not break list/show
echo '{bad' > "$B/cl/broken.json"
echo '{"name":"z","purpose":"p"}' > "$B/cl/noitems.json"
echo '{"name":"w","purpose":"p","decisions":[],"items":[{"id":1,"priority":"P0","spec":"todo","impl":"todo"}]}' > "$B/cl/nolinks.json"
out=$(cl "$A" list)
has "scan: list survives broken files" "$out" "| rel | ship it |"
has "scan: warns" "$out" "warning: skipped"
check "scan: show survives" "0" "$(node "$CL" "$A" show rel >/dev/null 2>&1; echo $?)"
check "validate: missing items rejected" "1" "$(node "$CL" "$A" show noitems >/dev/null 2>&1; echo $?)"
check "validate: missing links rejected" "1" "$(node "$CL" "$A" show nolinks >/dev/null 2>&1; echo $?)"
has "validate: clean message" "$(cl "$A" show noitems)" "invalid checklist"

# show: own file is used without scanning; broken same-name file elsewhere is ignored
mv "$B/cl/rel.json" "$TMP/rel.b.json"
echo '{bad' > "$B/cl/rel.json"
out=$(cl "$A" show rel)
has "show: own file wins over broken other" "$out" "rel — ship it"
rm "$B/cl/broken.json" "$B/cl/noitems.json" "$B/cl/nolinks.json"
mv "$TMP/rel.b.json" "$B/cl/rel.json"

# where: local path, plus store URL when set
has "where: own path" "$(cl "$A" where rel)" "local: $A/cl/rel.json"
has "where: other session" "$(cl "$B" where rel)" "local: $B/cl/rel.json"
check "where: missing" "1" "$(node "$CL" "$A" where nope >/dev/null 2>&1; echo $?)"
cl "$A" store rel https://github.com/o/r/issues/1 >/dev/null
has "where: store url" "$(cl "$A" where rel)" "store: https://github.com/o/r/issues/1"

# invalid stored state is rejected
sed -i.bak 's/"decided"/"bogus"/' "$A/cl/rel.json"
check "load: invalid state fails" "1" "$(node "$CL" "$A" show rel >/dev/null 2>&1; echo $?)"

exit $FAILED
