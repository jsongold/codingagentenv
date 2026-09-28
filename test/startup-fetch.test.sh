#!/usr/bin/env bash
# Simulates deploy/gcp/startup.sh's Secret Manager fetch against a fake metadata + Secret Manager server
# (node), and checks decode, atomic 0600 write, keep-on-failure, no secret in the log, and live_restore_off.
# Run: bash test/startup-fetch.test.sh   (needs node, curl, base64)
set -euo pipefail
root=$(cd "$(dirname "$0")/.." && pwd)
tmp=$(mktemp -d)
trap 'kill "$srv" 2>/dev/null; rm -rf "$tmp"' EXIT
fail() { echo "FAIL: $*"; exit 1; }

secret='{"opencode-go":{"type":"api","key":"sk-FAKE-do-not-log"}}'
b64=$(printf %s "$secret" | base64 | tr -d '\n')
port=$((20000 + RANDOM % 20000))
# shellcheck disable=SC2016 # JS template literal, not shell
B64=$b64 PORT=$port node -e '
const http = require("http");
const list = "# comment\nopencode 996c87ae\nopencode gone1\nopencode ../x\nfoo 1\n";
http.createServer((q, r) => {
  const md = q.url.startsWith("/computeMetadata/v1/");
  if (md && q.headers["metadata-flavor"] !== "Google") { r.writeHead(403); return r.end(); }
  const p = q.url.replace("/computeMetadata/v1/", "");
  if (p === "instance/attributes/cad-secrets") return r.end(list);
  if (p === "project/project-id") return r.end("proj");
  if (p === "instance/service-accounts/default/token")
    return r.end(JSON.stringify({access_token: "ya29.tok", expires_in: 3599, token_type: "Bearer"}));
  if (q.url === "/v1/projects/proj/secrets/cad-opencode-996c87ae/versions/latest:access") {
    if (q.headers.authorization !== "Bearer ya29.tok") { r.writeHead(401); return r.end(); }
    // Pretty-printed like the real API: multi-line, "data" before "dataCrc32c".
    return r.end(`{\n  "name": "projects/1/secrets/cad-opencode-996c87ae/versions/3",\n  "payload": {\n    "data": "${process.env.B64}",\n    "dataCrc32c": "123"\n  }\n}\n`);
  }
  r.writeHead(404); r.end("{\"error\":{\"code\":404}}");
}).listen(+process.env.PORT, "127.0.0.1");
' &
srv=$!
for _ in $(seq 50); do curl -s "127.0.0.1:$port" >/dev/null && break; sleep 0.1; done

vol=$tmp/vol
mkdir -p "$vol/.aienv/.store/gone1/opencode"
echo old >"$vol/.aienv/.store/gone1/opencode/auth.json"

# chown to uid 10001 needs root; stub it (ownership is not checked here, only content/mode/atomicity).
mkdir "$tmp/bin" && printf '#!/bin/sh\nexit 0\n' >"$tmp/bin/chown" && chmod +x "$tmp/bin/chown"
run() { # sources startup.sh in lib mode (functions only) and runs $1
  PATH="$tmp/bin:$PATH" CAD_STARTUP_LIB=1 CAD_VOL=$vol CAD_UID=$(id -u) CAD_MD="http://127.0.0.1:$port/computeMetadata/v1" \
    CAD_SM="http://127.0.0.1:$port/v1" bash -c ". '$root/deploy/gcp/startup.sh'; $1" >"$tmp/log" 2>&1
}
run fetch_secrets
dir=$vol/.aienv/.store/996c87ae/opencode
f=$dir/auth.json
[ "$(cat "$f")" = "$secret" ] || fail "decoded content differs"
perm=$(stat -f %Lp "$f" 2>/dev/null || stat -c %a "$f")
[ "$perm" = 600 ] || fail "mode $perm, want 600"
[ "$(cat "$vol/.aienv/.store/gone1/opencode/auth.json")" = old ] || fail "existing file not kept on 404"
[ ! -e "$vol/.aienv/x" ] || fail "path traversal id accepted"
[ -z "$(find "$dir" -name '.auth.json.*')" ] || fail "temp file left behind"
grep -q "sk-FAKE\|$b64\|ya29.tok" "$tmp/log" && fail "secret or token in log"
grep -q 'cad-opencode-gone1: access failed' "$tmp/log" || fail "no failure log for gone1"
grep -q 'cad-opencode-...x: bad id' "$tmp/log" || fail "no bad-id log"
grep -q 'cad-foo-1: unknown service' "$tmp/log" || fail "no unknown-service log"

# The timer's script is generated from the same functions (declare -p/-f); it must work standalone too.
rm "$f"
run 'declare -p UID_CAD VOL MD SM; declare -f md fetch_secrets; echo fetch_secrets' && cp "$tmp/log" "$tmp/gen.sh"
PATH="$tmp/bin:$PATH" bash "$tmp/gen.sh" >"$tmp/log" 2>&1
[ "$(cat "$f")" = "$secret" ] || fail "generated timer script did not write the file"

# live-restore override for swarm.
printf '{\n  "live-restore": true,\n  "storage-driver": "overlay2"\n}\n' >"$tmp/daemon.json"
run "live_restore_off '$tmp/daemon.json' '$tmp/out.json'"
grep -q '"live-restore": false' "$tmp/out.json" || fail "live-restore not false"
grep -q overlay2 "$tmp/out.json" || fail "other daemon.json options lost"
echo '{}' >"$tmp/none.json"
run "live_restore_off '$tmp/none.json' '$tmp/out2.json'" && fail "live_restore_off accepted a file without the key"
echo "PASS: startup fetch (decode, 0600 atomic write, keep-on-failure, no secret in log, timer script, live-restore)"
