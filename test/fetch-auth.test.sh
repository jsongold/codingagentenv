#!/usr/bin/env bash
# Runs deploy/fetch-auth.sh against a fake metadata + Secret Manager server (node) and checks decode, atomic 0600
# write (opencode auth.json, github token), keep-on-failure, no secret in the log, the CAD_SECRETS_LIST file; also startup.sh's live_restore_off.
# Run: bash test/fetch-auth.test.sh                           (host; needs node, curl, jq, base64)
#      CAD_TEST_IMAGE=cad-local bash test/fetch-auth.test.sh  (runs /app/bin/fetch-auth inside the image with
#      --network host, like startup.sh). Docker Desktop's host network is its VM, not the Mac, so there add
#      CAD_TEST_DOCKER_HOST=host.docker.internal (bridge network; the fake server then listens on 0.0.0.0).
set -euo pipefail
root=$(cd "$(dirname "$0")/.." && pwd)
tmp=$(mktemp -d)
trap 'kill "$srv" 2>/dev/null; rm -rf "$tmp"' EXIT
fail() { echo "FAIL: $*"; exit 1; }

secret='{"opencode-go":{"type":"api","key":"sk-FAKE-do-not-log"}}'
b64=$(printf %s "$secret" | base64 | tr -d '\n')
port=$((20000 + RANDOM % 20000))
bind=127.0.0.1; [ -z "${CAD_TEST_DOCKER_HOST:-}" ] || bind=0.0.0.0
# shellcheck disable=SC2016 # JS template literal, not shell
B64=$b64 PORT=$port BIND=$bind node -e '
const http = require("http");
const list = "# comment\nopencode 996c87ae\nopencode gone1\nopencode ../x\nfoo 1\ngithub gh1\n";
http.createServer((q, r) => {
  const md = q.url.startsWith("/computeMetadata/v1/");
  if (md && q.headers["metadata-flavor"] !== "Google") { r.writeHead(403); return r.end(); }
  const p = q.url.replace("/computeMetadata/v1/", "");
  if (p === "instance/attributes/cad-secrets") return r.end(list);
  if (p === "project/project-id") return r.end("proj");
  if (p === "instance/service-accounts/default/token")
    return r.end(JSON.stringify({access_token: "ya29.tok", expires_in: 3599, token_type: "Bearer"}));
  if (["cad-opencode-996c87ae", "cad-github-gh1"].some((n) => q.url === `/v1/projects/proj/secrets/${n}/versions/latest:access`)) {
    if (q.headers.authorization !== "Bearer ya29.tok") { r.writeHead(401); return r.end(); }
    // Pretty-printed like the real API.
    return r.end(`{\n  "name": "projects/1/secrets/cad-opencode-996c87ae/versions/3",\n  "payload": {\n    "data": "${process.env.B64}",\n    "dataCrc32c": "123"\n  }\n}\n`);
  }
  if (q.url.startsWith("/bad/")) return r.end("<html>not json</html>"); // 200 with an unusable body
  r.writeHead(404); r.end("{\"error\":{\"code\":404}}");
}).listen(+process.env.PORT, process.env.BIND);
' &
srv=$!
for _ in $(seq 50); do curl -s "127.0.0.1:$port" >/dev/null && break; sleep 0.1; done

vol=$tmp/vol
mkdir -p "$vol/.aienv/.store/gone1/opencode"
echo old >"$vol/.aienv/.store/gone1/opencode/auth.json"
md="http://127.0.0.1:$port/computeMetadata/v1"
dhost=${CAD_TEST_DOCKER_HOST:-127.0.0.1} net=host; [ "$dhost" = 127.0.0.1 ] || net=bridge

fetch() { # <SM base URL> [list file relative to the volume]; output -> $tmp/log
  if [ -n "${CAD_TEST_IMAGE:-}" ]; then
    docker run --rm --network "$net" --user "$(id -u)" \
      -v "$vol:/data" -e CAD_MD="${md/127.0.0.1/$dhost}" -e CAD_SM="${1/127.0.0.1/$dhost}" \
      ${2:+-e CAD_SECRETS_LIST=/data/$2} --entrypoint /app/bin/fetch-auth "$CAD_TEST_IMAGE" >"$tmp/log" 2>&1
  else
    CAD_VOL=$vol CAD_MD=$md CAD_SM=$1 CAD_SECRETS_LIST=${2:+$vol/$2} bash "$root/deploy/fetch-auth.sh" >"$tmp/log" 2>&1
  fi
}
fetch "http://127.0.0.1:$port/v1"
dir=$vol/.aienv/.store/996c87ae/opencode
f=$dir/auth.json
[ "$(cat "$f" 2>/dev/null)" = "$secret" ] || { cat "$tmp/log"; fail "decoded content differs"; }
perm=$(stat -f %Lp "$f" 2>/dev/null || stat -c %a "$f")
[ "$perm" = 600 ] || fail "mode $perm, want 600"
[ "$(cat "$vol/.aienv/.store/gone1/opencode/auth.json")" = old ] || fail "existing file not kept on 404"
[ ! -e "$vol/.aienv/x" ] || fail "path traversal id accepted"
[ -z "$(find "$dir" -name '.auth.json.*')" ] || fail "temp file left behind"
grep -q "sk-FAKE\|$b64\|ya29.tok" "$tmp/log" && fail "secret or token in log"
grep -q 'cad-opencode-gone1: access failed' "$tmp/log" || fail "no failure log for gone1"
grep -q 'cad-opencode-...x: bad id' "$tmp/log" || fail "no bad-id log"
grep -q 'cad-foo-1: unknown service' "$tmp/log" || fail "no unknown-service log"
g=$vol/.aienv/.store/gh1/github/token
[ "$(cat "$g" 2>/dev/null)" = "$secret" ] || fail "github token not written"
[ "$(stat -f %Lp "$g" 2>/dev/null || stat -c %a "$g")" = 600 ] || fail "github token mode"

# List from a file; a 200 response without payload.data keeps the existing file and leaves no temp file.
printf 'opencode 996c87ae\n' >"$vol/list"
echo prev >"$f"
fetch "http://127.0.0.1:$port/bad" list
[ "$(cat "$f")" = prev ] || fail "file replaced after an unusable response"
grep -q 'cad-opencode-996c87ae: decode/write failed' "$tmp/log" || { cat "$tmp/log"; fail "no decode failure log"; }
[ -z "$(find "$dir" -name '.auth.json.*')" ] || fail "temp file left behind after failure"
grep -q 'cad-opencode-gone1' "$tmp/log" && fail "list file ignored (metadata list used)"

# live-restore override for swarm (startup.sh sourced in lib mode: functions only).
run() { CAD_STARTUP_LIB=1 CAD_VOL=$vol bash -c ". '$root/deploy/gcp/startup.sh'; $1" >"$tmp/log" 2>&1; }
printf '{\n  "live-restore": true,\n  "storage-driver": "overlay2"\n}\n' >"$tmp/daemon.json"
run "live_restore_off '$tmp/daemon.json' '$tmp/out.json'"
grep -q '"live-restore": false' "$tmp/out.json" || fail "live-restore not false"
grep -q overlay2 "$tmp/out.json" || fail "other daemon.json options lost"
echo '{}' >"$tmp/none.json"
run "live_restore_off '$tmp/none.json' '$tmp/out2.json'" && fail "live_restore_off accepted a file without the key"
echo "PASS: fetch-auth${CAD_TEST_IMAGE:+ in $CAD_TEST_IMAGE} (decode, 0600 atomic write, keep-on-failure, no secret in log, list file, live-restore)"
