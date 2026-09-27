#!/bin/bash
# Tests for tools/ai-review (and codex-localreview's quota exit) with stubbed gh, claude,
# codex-localreview, codex and a fake cad. Run: bash test/ai-review.test.sh
set -u

ROOT=$(cd "$(dirname "$0")/.." && pwd)
TMP=$(mktemp -d)
FAILED=0
check() { # name, expected, actual
  if [ "$2" = "$3" ]; then echo "ok   - $1"; else echo "FAIL - $1 (expected '$2', got '$3')"; FAILED=1; fi
}

# A worktree whose HEAD is the "PR head" the fake gh reports.
WT=$TMP/wt
git init -q "$WT" && git -C "$WT" -c user.name=t -c user.email=t@t commit -q --allow-empty -m init
git -C "$WT" remote add origin "$WT"
export FAKE_HEAD=$(git -C "$WT" rev-parse HEAD)

mkdir -p "$TMP/bin"
cat >"$TMP/bin/gh" <<'EOF'
#!/bin/bash
echo "gh $*" >>"$CALLS"
case "$1 $2" in
  "pr view") echo "$FAKE_HEAD" ;;
  "pr comment") echo "https://github.test/pr/1#comment" ;;
  api*) [ -n "${FAKE_BOT:-}" ] && echo "$FAKE_BOT"; exit 0 ;;
esac
EOF
cat >"$TMP/bin/codex-localreview" <<'EOF'
#!/bin/bash
echo "codex-localreview $*" >>"$CALLS"
case ${FAKE_LOCAL:-1} in
  0) echo "#$1 done P1=0 https://github.test/pr/1#local" ;;
  3) echo "#$1 QUOTA: out of credits"; exit 3 ;;
  *) echo "#$1 FAILED: boom"; exit 1 ;;
esac
EOF
cat >"$TMP/bin/claude" <<'EOF'
#!/bin/bash
echo "claude $* stdin=$(head -c 20)" >>"$CALLS"
[ "${FAKE_CLAUDE:-1}" = 0 ] && echo "No issues found." || { echo "claude broke" >&2; exit 1; }
EOF
chmod +x "$TMP/bin"/*
export PATH="$TMP/bin:$PATH" CALLS="$TMP/calls" XDG_STATE_HOME="$TMP/state"

printf '{"review":{"reviewers":["codex-bot","codex-local","claude-opus"],"excludeImplementer":true}}' >"$TMP/policy.json"
export CAD_POLICY="$TMP/policy.json"

# Fake cad: GET /v1/quota serves $TMP/quota.json; POSTs are appended to $TMP/posts.
echo '[]' >"$TMP/quota.json"
node -e '
const fs = require("fs"), [q, posts, portFile] = process.argv.slice(1);
require("http").createServer((req, res) => {
  let b = ""; req.on("data", (c) => (b += c)).on("end", () => {
    if (req.method === "POST") { fs.appendFileSync(posts, `${req.url} ${b}\n`); res.writeHead(204).end(); }
    else if (req.url === "/v1/quota") res.end(fs.readFileSync(q));
    else res.writeHead(404).end();
  });
}).listen(0, "127.0.0.1", function () { fs.writeFileSync(portFile, String(this.address().port)); });
' "$TMP/quota.json" "$TMP/posts" "$TMP/port" &
CAD_PID=$!
trap 'kill $CAD_PID; rm -rf "$TMP"' EXIT
for _ in $(seq 50); do [ -s "$TMP/port" ] && break; sleep 0.1; done
export CAD_URL="http://127.0.0.1:$(cat "$TMP/port")"

review() { # env assignments may precede; runs ai-review 1 $WT "$@"
  : >"$CALLS"; : >"$TMP/posts"
  bash "$ROOT/tools/ai-review" 1 "$WT" "$@" >"$TMP/out" 2>"$TMP/err"
}
called() { grep -c "^$1" "$CALLS"; }

FAKE_BOT=https://github.test/pr/1#bot review
check "bot review on head: exit 0" 0 $?
check "codex-bot has priority" "codex-bot reviewed #1: https://github.test/pr/1#bot" "$(cat "$TMP/out")"
check "later reviewers not run" 0 "$(called codex-localreview)"

FAKE_LOCAL=0 review
check "no bot review -> codex-local" "codex-local reviewed #1: https://github.test/pr/1#local" "$(cat "$TMP/out")"

FAKE_LOCAL=0 FAKE_CLAUDE=0 review --implementer codex-local
check "implementer excluded -> claude-opus" "claude-opus reviewed #1: https://github.test/pr/1#comment" "$(cat "$TMP/out")"
check "implementer not run" 0 "$(called codex-localreview)"
check "claude review file written" 1 "$(grep -c 'No issues found' "$TMP/state/ai-review/claude-1.md")"
check "claude gets the prompt on stdin" 1 "$(grep -c "stdin=You are reviewing" "$CALLS")"

FAKE_LOCAL=0 FAKE_CLAUDE=0 AI_REVIEW_IMPLEMENTER=codex-local review
check "AI_REVIEW_IMPLEMENTER also excludes" 0 "$(called codex-localreview)"

echo '[{"reviewer":"codex-bot","state":"exhausted"}]' >"$TMP/quota.json"
FAKE_BOT=https://github.test/pr/1#bot FAKE_LOCAL=0 review
check "exhausted reviewer skipped" "codex-local reviewed #1: https://github.test/pr/1#local" "$(cat "$TMP/out")"
check "exhausted reviewer not queried" 0 "$(called 'gh api')"
echo '[]' >"$TMP/quota.json"

FAKE_LOCAL=3 FAKE_CLAUDE=0 review
check "quota hit falls through" "claude-opus reviewed #1: https://github.test/pr/1#comment" "$(cat "$TMP/out")"
check "quota hit is POSTed to cad" '/v1/quota/codex-local {"state":"exhausted"}' "$(cat "$TMP/posts")"

FAKE_LOCAL=1 FAKE_CLAUDE=0 review
check "plain failure is not POSTed" "" "$(cat "$TMP/posts")"

review
check "all fail: exit 1" 1 $?
check "all fail: one-line reason" 1 "$(grep -c '^ai-review: no reviewer succeeded for #1' "$TMP/err")"
check "all fail: nothing on stdout" "" "$(cat "$TMP/out")"

FAKE_LOCAL=0 CAD_URL=http://127.0.0.1:1 review
check "cad unreachable: nothing skipped" "codex-local reviewed #1: https://github.test/pr/1#local" "$(cat "$TMP/out")"

bash "$ROOT/tools/ai-review" 1 >/dev/null 2>&1
check "missing worktree arg: exit 1" 1 $?

# codex-localreview: exit 3 on codex's usage-limit error, exit 0 with the comment URL on success.
cat >"$TMP/bin/codex" <<'EOF'
#!/bin/bash
[ -n "${FAKE_CODEX_OUT:-}" ] && { while [ "$1" != -o ]; do shift; done; echo "$FAKE_CODEX_OUT" >"$2"; exit 0; }
echo "ERROR: Your workspace is out of credits. Ask your workspace owner to refill in order to continue."; exit 1
EOF
chmod +x "$TMP/bin/codex"
export CODEX_REVIEW_DIR="$TMP/lrev"
bash "$ROOT/tools/codex-localreview" 1 "$WT" >"$TMP/out" 2>&1
check "codex-localreview: usage limit exits 3" 3 $?
FAKE_CODEX_OUT="[P1] a bug" bash "$ROOT/tools/codex-localreview" 1 "$WT" >"$TMP/out" 2>&1
check "codex-localreview: success prints URL" "0 #1 done P1=1 https://github.test/pr/1#comment" "$? $(cat "$TMP/out")"

exit $FAILED
