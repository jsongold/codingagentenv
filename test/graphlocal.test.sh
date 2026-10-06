#!/bin/bash
# Tests for tools/graphlocal/graphlocal.ts.
# Run: bash test/graphlocal.test.sh
set -u

ROOT=$(cd "$(dirname "$0")/.." && pwd)
GL="$ROOT/tools/graphlocal/graphlocal.ts"
# Isolate state: the tool keeps its files under tmpdir(), so point TMPDIR at a private
# directory. Other graphlocal servers (live diagrams) are never touched.
TMP=$(mktemp -d)
mkdir "$TMP/tmp"
export TMPDIR="$TMP/tmp"
DIR="$(node -p 'require("node:os").tmpdir()')/graphlocal"
own_pids() { # pids of the servers this test started (from its own lock files)
  for f in "$DIR"/*.lock; do
    [ -f "$f" ] && node -p "JSON.parse(require('node:fs').readFileSync('$f','utf8')).pid" 2>/dev/null
  done
}
stop_own() { for p in $(own_pids); do kill "$p" 2>/dev/null; done; }
trap 'stop_own; rm -rf "$TMP"' EXIT
FAILED=0
check() { # name, expected, actual
  if [ "$2" = "$3" ]; then echo "ok   - $1"; else echo "FAIL - $1 (expected '$2', got '$3')"; FAILED=1; fi
}
has() { # name, haystack, needle
  case "$2" in *"$3"*) echo "ok   - $1" ;; *) echo "FAIL - $1 (missing '$3' in: $2)"; FAILED=1 ;; esac
}

printf 'flowchart LR\n  A --> B\n' > "$TMP/g1.mmd"
printf 'flowchart TD\n  C --> D\n' > "$TMP/g2.mmd"

# usage errors
check "no args fails" "1" "$(node "$GL" >/dev/null 2>&1; echo $?)"
check "unknown cmd fails" "1" "$(node "$GL" frobnicate >/dev/null 2>&1; echo $?)"
check "missing file fails" "1" "$(node "$GL" serve /nope.mmd >/dev/null 2>&1; echo $?)"
check "empty stdin fails" "1" "$(printf '' | node "$GL" serve >/dev/null 2>&1; echo $?)"
check "kill without slug fails" "1" "$(node "$GL" kill >/dev/null 2>&1; echo $?)"
check "url without slug fails" "1" "$(node "$GL" url >/dev/null 2>&1; echo $?)"

# serve from file: prints URL, lock exists, worker runs
out=$(node "$GL" serve "$TMP/g1.mmd")
has "serve: prints url" "$out" "http://127.0.0.1:"
slug1=$(echo "$out" | sed -n 's/.*kill \([^ ]*\)$/\1/p' | xargs basename)
check "serve: lock written" "0" "$(test -f "$DIR/$slug1.lock"; echo $?)"
port1=$(node -p "JSON.parse(require('node:fs').readFileSync('$DIR/$slug1.lock','utf8')).port")
pid1=$(node -p "JSON.parse(require('node:fs').readFileSync('$DIR/$slug1.lock','utf8')).pid")
check "serve: worker alive" "0" "$(kill -0 "$pid1" 2>/dev/null; echo $?)"

# http: GET / serves the diagram, other paths 404
has "http: mermaid source" "$(curl -s "http://127.0.0.1:$port1/")" "flowchart LR"
check "http: unknown path is 404" "404" "$(curl -s -o /dev/null -w '%{http_code}' "http://127.0.0.1:$port1/nope")"
has "http: mermaid CDN" "$(curl -s "http://127.0.0.1:$port1/")" "cdn.jsdelivr.net/npm/mermaid"

# /edit replaces the page body
curl -s -X POST --data '<pre class="mermaid">EDITED</pre>' "http://127.0.0.1:$port1/edit" >/dev/null
has "edit: new body served" "$(curl -s "http://127.0.0.1:$port1/")" "EDITED"

# url: prints the running URL, fails for unknown slug
check "url: running" "http://127.0.0.1:$port1/" "$(node "$GL" url "$slug1")"
check "url: unknown fails" "1" "$(node "$GL" url nope-00000000 >/dev/null 2>&1; echo $?)"

# serve same content again: replaces the old server on a new port
out2=$(node "$GL" serve "$TMP/g1.mmd")
port1b=$(node -p "JSON.parse(require('node:fs').readFileSync('$DIR/$slug1.lock','utf8')).port")
check "serve again: new port" "1" "$([ "$port1" != "$port1b" ] && echo 1)"
sleep 1
check "serve again: old port dead" "7" "$(curl -s -o /dev/null -m 2 "http://127.0.0.1:$port1/" >/dev/null 2>&1; echo $?)" # curl exit 7 = connection refused
has "serve again: serves diagram" "$(curl -s "http://127.0.0.1:$port1b/")" "flowchart LR"

# serve different content: separate slug, both run
out3=$(node "$GL" serve "$TMP/g2.mmd")
slug2=$(echo "$out3" | sed -n 's/.*kill \([^ ]*\)$/\1/p' | xargs basename)
check "two slugs: different" "1" "$([ "$slug1" != "$slug2" ] && echo 1)"
check "two slugs: first alive" "0" "$(kill -0 "$(node -p "JSON.parse(require('node:fs').readFileSync('$DIR/$slug1.lock','utf8')).pid")" 2>/dev/null; echo $?)"
check "two slugs: second alive" "0" "$(kill -0 "$(node -p "JSON.parse(require('node:fs').readFileSync('$DIR/$slug2.lock','utf8')).pid")" 2>/dev/null; echo $?)"

# kill: stops the worker and cleans lock+sock
node "$GL" kill "$slug1" >/dev/null
sleep 1
pid1=$(node -p "JSON.parse(require('node:fs').readFileSync('$DIR/$slug1.lock','utf8')).pid" 2>/dev/null || echo gone)
if [ "$pid1" = gone ]; then
  echo "ok   - kill: lock removed"
else
  check "kill: worker gone" "1" "$(kill -0 "$pid1" 2>/dev/null; echo $?)"
  check "kill: lock removed" "1" "$(test -f "$DIR/$slug1.lock"; echo $?)"
fi
check "kill: other slug untouched" "0" "$(kill -0 "$(node -p "JSON.parse(require('node:fs').readFileSync('$DIR/$slug2.lock','utf8')).pid")" 2>/dev/null; echo $?)"

# kill unknown slug is a no-op, stale lock is cleaned
check "kill: unknown no-op" "0" "$(node "$GL" kill nope-00000000 >/dev/null 2>&1; echo $?)"
echo '{"pid": 999999999, "port": 1, "url": "http://x/", "created": "x"}' > "$DIR/stale-abc.lock"
check "kill: stale lock cleaned" "0" "$(node "$GL" kill stale-abc >/dev/null 2>&1; echo $?)"
check "kill: stale gone" "1" "$(test -f "$DIR/stale-abc.lock"; echo $?)"

# stdin input
out4=$(printf 'sequenceDiagram\n  A->>B: hello\n' | node "$GL" serve)
has "stdin: serves" "$out4" "http://127.0.0.1:"

# cleanup all
pids=$(own_pids)
stop_own
sleep 0.5
left=0
for p in $pids; do kill -0 "$p" 2>/dev/null && left=1; done
check "cleanup: no own workers left" "0" "$left"

exit $FAILED
