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
has "new: purpose" "$out" "Purpose: ship it"
has "new: deadline" "$out" "Deadline: 2026-10-31"
check "new twice fails" "1" "$(node "$CL" "$A" new rel --purpose x >/dev/null 2>&1; echo $?)"

cl "$A" add rel P0 "first" >/dev/null
out=$(cl "$A" add rel P1 "second")
has "add: ids increment" "$out" "| 2 | second | todo | todo |"
check "add: bad priority fails" "1" "$(node "$CL" "$A" add rel P9 x >/dev/null 2>&1; echo $?)"

out=$(cl "$A" spec rel 1 decided)
has "spec" "$out" "| 1 | first | decided | todo |"
out=$(cl "$A" impl rel 1 n/a)
has "impl" "$out" "| 1 | first | decided | n/a |"
check "impl: bad state fails" "1" "$(node "$CL" "$A" impl rel 1 nope >/dev/null 2>&1; echo $?)"
check "spec: unknown item fails" "1" "$(node "$CL" "$A" spec rel 99 todo >/dev/null 2>&1; echo $?)"

out=$(cl "$A" link rel 2 PR http://x/1)
has "link" "$out" "[PR](http://x/1)"
out=$(cl "$A" decide rel "use plan B")
has "decide" "$out" "$(date +%F): use plan B"

# show / list across sessions
cl "$B" new other --purpose "b side" >/dev/null
cl "$B" new rel --purpose "b rel" >/dev/null
has "show: own first" "$(cl "$A" show rel)" "Purpose: ship it"
out=$(cl "$A" show other)
has "show: other session" "$out" "Purpose: b side"
out=$(cl "$A" show rel)
has "show: notes duplicates" "$out" "1 other checklist(s) named rel"
check "show: missing fails" "1" "$(node "$CL" "$A" show nope >/dev/null 2>&1; echo $?)"

out=$(cl "$A" list)
has "list: header" "$out" "| Name | Purpose | Open | Updated | This session |"
has "list: own row" "$out" "| rel | ship it | 1 |"
has "list: other row" "$out" "| other | b side | 0 |"
check "list: empty" "No checklists." "$(cl "$TMP/none/x/scratchpad" list)"

# multi-word args are joined
out=$(cl "$A" add rel P2 drop e2e tests)
has "add: joins title" "$out" "| 3 | drop e2e tests |"
out=$(cl "$A" link rel 3 my PR label http://x/2)
has "link: joins label" "$out" "[my PR label](http://x/2)"
out=$(cl "$A" decide rel drop the e2e)
has "decide: joins text" "$out" ": drop the e2e"

# escaping
out=$(cl "$A" add rel P2 "a|b")
has "escape: pipe in title" "$out" 'a\|b'
out=$(cl "$A" link rel 3 "x]y" http://x/3)
has "escape: bracket in label" "$out" '[x\]y](http://x/3)'
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
has "show: own file wins over broken other" "$out" "Purpose: ship it"
rm "$B/cl/broken.json" "$B/cl/noitems.json" "$B/cl/nolinks.json"
mv "$TMP/rel.b.json" "$B/cl/rel.json"

# invalid stored state is rejected
sed -i.bak 's/"decided"/"bogus"/' "$A/cl/rel.json"
check "load: invalid state fails" "1" "$(node "$CL" "$A" show rel >/dev/null 2>&1; echo $?)"

exit $FAILED
