#!/bin/bash
# Tests for apps/datadoc (unit tests, then e2e through tools/datadoc).
# Run: bash test/datadoc.test.sh
set -u

ROOT=$(cd "$(dirname "$0")/.." && pwd)
DD=$ROOT/tools/datadoc
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT
FAILED=0
check() { # name, expected, actual
  if [ "$2" = "$3" ]; then echo "ok   - $1"; else echo "FAIL - $1 (expected '$2', got '$3')"; FAILED=1; fi
}
has() { # name, haystack, needle
  case "$2" in *"$3"*) echo "ok   - $1" ;; *) echo "FAIL - $1 (missing '$3' in: $2)"; FAILED=1 ;; esac
}

if (cd "$ROOT" && node --test apps/datadoc/*.test.ts >"$TMP/unit.log" 2>&1); then
  echo "ok   - unit tests"
else
  cat "$TMP/unit.log"; echo "FAIL - unit tests"; FAILED=1
fi

printf 'id\tname\n1\tfoo\n' > "$TMP/a.tsv"
printf 'k\tv\nx\ty\n' > "$TMP/b.tsv"
printf 'id\tname\n1\n' > "$TMP/bad.tsv"

out=$("$DD" "$TMP/a.tsv" --md)
has "md: header" "$out" "| id | name |"
has "md: row" "$out" "| 1 | foo |"
out=$("$DD" "$TMP/a.tsv" --html)
has "html: th" "$out" "<th>id</th><th>name</th>"
has "html: td" "$out" "<td>1</td><td>foo</td>"

out=$("$DD" "$TMP/a.tsv" "$TMP/b.tsv" --md)
has "md multi: first heading" "$out" "## a.tsv"
has "md multi: second heading" "$out" "## b.tsv"
out=$("$DD" "$TMP/a.tsv" "$TMP/b.tsv" --html)
has "html multi: first heading" "$out" "<h2>a.tsv</h2>"
has "html multi: second heading" "$out" "<h2>b.tsv</h2>"

out=$(printf 'p\tq\n3\t4\n' | "$DD" - "$TMP/a.tsv" --md)
has "stdin: named stdin" "$out" "## stdin"
has "stdin: row" "$out" "| 3 | 4 |"

out=$("$DD" "$TMP/a.tsv" --md -o "$TMP/out.md")
check "-o: nothing on stdout" "" "$out"
has "-o: file written" "$(cat "$TMP/out.md")" "| 1 | foo |"

err=$("$DD" "$TMP/bad.tsv" --md 2>&1 >/dev/null); rc=$?
check "bad format: exit 1" "1" "$rc"
has "bad format: line number" "$err" "bad.tsv:2:"
check "missing file: exit 1" "1" "$("$DD" "$TMP/none.tsv" --md >/dev/null 2>&1; echo $?)"

err=$("$DD" "$TMP/a.tsv" 2>&1 >/dev/null); rc=$?
check "no format: exit 1" "1" "$rc"
has "no format: usage" "$err" "usage:"
check "both formats: exit 1" "1" "$("$DD" "$TMP/a.tsv" --md --html >/dev/null 2>&1; echo $?)"
check "unknown option: exit 1" "1" "$("$DD" "$TMP/a.tsv" --md --x >/dev/null 2>&1; echo $?)"
check "no files: exit 1" "1" "$("$DD" --md >/dev/null 2>&1; echo $?)"
check "-o without value: exit 1" "1" "$("$DD" "$TMP/a.tsv" --md -o >/dev/null 2>&1; echo $?)"
check "stdin twice: exit 1" "1" "$(echo | "$DD" - - --md >/dev/null 2>&1; echo $?)"

exit $FAILED
