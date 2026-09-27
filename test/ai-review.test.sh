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
git init -q -b main "$WT" && git -C "$WT" -c user.name=t -c user.email=t@t commit -q --allow-empty -m init
git -C "$WT" remote add origin "$WT"
HEAD_SHA=$(git -C "$WT" rev-parse HEAD)
export HEAD_FILE=$TMP/head # the PR head the fake gh reports; fakes write here to simulate a push
echo "$HEAD_SHA" >"$HEAD_FILE"

mkdir -p "$TMP/bin"
cat >"$TMP/bin/gh" <<'EOF'
#!/bin/bash
echo "gh $*" >>"$CALLS"
case "$1 $2" in
  "pr view") cat "$HEAD_FILE" ;;
  "pr comment") echo "https://github.test/pr/1#comment" ;;
  api*) while [ "$1" != --jq ]; do shift; done; echo "${FAKE_REVIEWS:-[]}" | jq -r "$2" ;;
esac
EOF
cat >"$TMP/bin/codex-localreview" <<'EOF'
#!/bin/bash
echo "codex-localreview $*" >>"$CALLS"
[ -n "${FAKE_MOVE:-}" ] && echo moved >"$HEAD_FILE"
case ${FAKE_LOCAL:-1} in
  0) echo "#$1 done P1=0 https://github.test/pr/1#local" ;;
  3) echo "#$1 QUOTA: out of credits"; exit 3 ;;
  4) echo "#$1 STALE: head moved; not posted"; exit 4 ;;
  *) echo "#$1 FAILED: boom"; exit 1 ;;
esac
EOF
cat >"$TMP/bin/claude" <<'EOF'
#!/bin/bash
echo "claude $* stdin=$(head -c 20) base=$(git rev-parse -q --verify origin/main >/dev/null && echo fetched)" >>"$CALLS"
[ -n "${FAKE_MOVE:-}" ] && echo moved >"$HEAD_FILE"
[ "${FAKE_CLAUDE:-1}" = 0 ] && echo "No issues found." || { echo "claude broke" >&2; exit 1; }
EOF
chmod +x "$TMP/bin"/*
export PATH="$TMP/bin:$PATH" CALLS="$TMP/calls" XDG_STATE_HOME="$TMP/state"

printf '{"review":{"reviewers":["codex-bot","codex-local","claude-opus"],"excludeImplementer":true}}' >"$TMP/policy.json"
export CAD_POLICY="$TMP/policy.json" AI_REVIEW_IMPLEMENTER=someone-else

# Fake cad: GET /v1/quota serves $TMP/quota.json; POSTs are appended to $TMP/posts.
echo '[]' >"$TMP/quota.json"
node -e '
const fs = require("fs"), [q, posts, portFile] = process.argv.slice(1);
require("http").createServer((req, res) => {
  let b = ""; req.on("data", (c) => (b += c)).on("end", () => {
    if (req.method === "POST") { fs.appendFileSync(posts, `${req.url} ${b}\n`); res.writeHead(204).end(); }
    else if (req.url === "/v1/quota?ns=default") res.end(fs.readFileSync(q));
    else res.writeHead(404).end();
  });
}).listen(0, "127.0.0.1", function () { fs.writeFileSync(portFile, String(this.address().port)); });
' "$TMP/quota.json" "$TMP/posts" "$TMP/port" &
CAD_PID=$!
trap 'kill $CAD_PID; rm -rf "$TMP"' EXIT
for _ in $(seq 50); do [ -s "$TMP/port" ] && break; sleep 0.1; done
export CAD_URL="http://127.0.0.1:$(cat "$TMP/port")"

review() { # env assignments may precede; runs ai-review 1 $WT "$@"
  : >"$CALLS"; : >"$TMP/posts"; echo "$HEAD_SHA" >"$HEAD_FILE"
  bash "$ROOT/tools/ai-review" 1 "$WT" "$@" >"$TMP/out" 2>"$TMP/err"
}
called() { grep -c "^$1" "$CALLS"; }

bot() { printf '[{"user":{"login":"chatgpt-codex-connector[bot]"},"commit_id":"%s","state":"%s","html_url":"https://github.test/pr/1#bot"}]' "$1" "$2"; }
FAKE_REVIEWS=$(bot "$HEAD_SHA" COMMENTED) review
check "bot review on head: exit 0" 0 $?
check "codex-bot has priority" "codex-bot reviewed #1: https://github.test/pr/1#bot" "$(cat "$TMP/out")"
check "later reviewers not run" 0 "$(called codex-localreview)"

FAKE_LOCAL=0 review
check "no bot review -> codex-local" "codex-local reviewed #1: https://github.test/pr/1#local" "$(cat "$TMP/out")"

FAKE_REVIEWS=$(bot "$HEAD_SHA" DISMISSED) FAKE_LOCAL=0 review
check "dismissed bot review is not coverage" "codex-local reviewed #1: https://github.test/pr/1#local" "$(cat "$TMP/out")"
FAKE_REVIEWS=$(bot 0000000 COMMENTED) FAKE_LOCAL=0 review
check "bot review on an old commit is not coverage" "codex-local reviewed #1: https://github.test/pr/1#local" "$(cat "$TMP/out")"

AI_REVIEW_IMPLEMENTER= FAKE_LOCAL=0 review
check "no implementer with excludeImplementer: exit 1" 1 $?
check "no implementer: clear message, no review" "ai-review: policy review.excludeImplementer is on: pass --implementer <name> or set AI_REVIEW_IMPLEMENTER|0" "$(cat "$TMP/err")|$(wc -l <"$CALLS" | tr -d ' ')"
AI_REVIEW_IMPLEMENTER= FAKE_LOCAL=0 review --implementer ''
check "empty --implementer is refused" 1 $?

FAKE_MOVE=1 FAKE_CLAUDE=0 review --implementer codex-local
check "head moved during claude review: exit 1" 1 $?
check "stale claude review is not posted" 0 "$(called 'gh pr comment')"
check "stale is reported" 1 "$(grep -c 'stale: #1 head moved' "$TMP/err")"
FAKE_MOVE=1 FAKE_LOCAL=0 review
check "head moved during codex-local review: exit 1, not accepted" "1|" "$?|$(cat "$TMP/out")"
FAKE_LOCAL=4 FAKE_CLAUDE=0 review
check "codex-localreview stale (exit 4): exit 1, no fallback" "1|0" "$?|$(called claude)"
check "codex-localreview stale is reported" 1 "$(grep -c '^ai-review: stale: #1 STALE' "$TMP/err")"

FAKE_LOCAL=0 FAKE_CLAUDE=0 review --implementer codex-local
check "implementer excluded -> claude-opus" "claude-opus reviewed #1: https://github.test/pr/1#comment" "$(cat "$TMP/out")"
check "implementer not run" 0 "$(called codex-localreview)"
check "claude review file written" 1 "$(grep -c 'No issues found' "$TMP/state/ai-review/claude-1.md")"
check "claude fallback fetches origin/main first" 1 "$(grep -c "base=fetched" "$CALLS")"
check "claude gets the prompt on stdin" 1 "$(grep -c "stdin=You are reviewing" "$CALLS")"

FAKE_LOCAL=0 FAKE_CLAUDE=0 AI_REVIEW_IMPLEMENTER=codex-local review
check "AI_REVIEW_IMPLEMENTER also excludes" 0 "$(called codex-localreview)"

echo '[{"reviewer":"codex-bot","state":"exhausted"}]' >"$TMP/quota.json"
FAKE_REVIEWS=$(bot "$HEAD_SHA" COMMENTED) FAKE_LOCAL=0 review
check "exhausted codex-bot still counts an existing review" "codex-bot reviewed #1: https://github.test/pr/1#bot" "$(cat "$TMP/out")"
echo '[{"reviewer":"codex-local","state":"exhausted"}]' >"$TMP/quota.json"
FAKE_LOCAL=0 FAKE_CLAUDE=0 review
check "exhausted reviewer skipped" "claude-opus reviewed #1: https://github.test/pr/1#comment" "$(cat "$TMP/out")"
check "exhausted reviewer not run" 0 "$(called codex-localreview)"
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
[ -n "${FAKE_MOVE_ON_CODEX:-}" ] && echo moved >"$HEAD_FILE"
[ -n "${FAKE_CODEX_OUT:-}" ] && { while [ "$1" != -o ]; do shift; done; echo "$FAKE_CODEX_OUT" >"$2"; exit 0; }
echo "ERROR: Your workspace is out of credits. Ask your workspace owner to refill in order to continue."; exit 1
EOF
chmod +x "$TMP/bin/codex"
export CODEX_REVIEW_DIR="$TMP/lrev"
bash "$ROOT/tools/codex-localreview" 1 "$WT" >"$TMP/out" 2>&1
check "codex-localreview: usage limit exits 3" 3 $?
: >"$CALLS"
FAKE_CODEX_OUT="[P1] a bug" FAKE_MOVE_ON_CODEX=1 bash "$ROOT/tools/codex-localreview" 1 "$WT" >"$TMP/out" 2>&1
check "codex-localreview: head moved exits 4" 4 $?
check "codex-localreview: stale review not posted" 0 "$(called 'gh pr comment')"
echo "$HEAD_SHA" >"$HEAD_FILE"
FAKE_CODEX_OUT="[P1] a bug" bash "$ROOT/tools/codex-localreview" 1 "$WT" >"$TMP/out" 2>&1
check "codex-localreview: success prints URL" "0 #1 done P1=1 https://github.test/pr/1#comment" "$? $(cat "$TMP/out")"

exit $FAILED
