#!/usr/bin/env bash
# Runs deploy/worker-run.sh inside the worker image (Dockerfile.worker) with fake gh/opencode on PATH and a local
# bare repo (CLONE_URL), network-free: new branch -> commit, push, PR create with "Closes #n"; second run reuses
# the pushed branch and does not open a second PR; missing auth files -> exit 3; the token never reaches the log.
# Run: bash test/opencode-worker.test.sh   (needs docker; builds the image as opencode-worker-test)
set -euo pipefail
root=$(cd "$(dirname "$0")/.." && pwd)
tmp=$(mktemp -d)
trap 'docker run --rm -v "$tmp:/t" --entrypoint rm opencode-worker-test -rf /t/data /t/nogh /t/remote.git 2>/dev/null; rm -rf "$tmp"' EXIT
fail() { echo "FAIL: $*"; cat "$tmp/log" 2>/dev/null; exit 1; }
docker info >/dev/null 2>&1 || { echo "skip: docker is not running"; exit 0; }
docker build -q -f "$root/Dockerfile.worker" -t opencode-worker-test "$root" >/dev/null

mkdir -p "$tmp/bin" "$tmp/data/.aienv/.store/aa/opencode" "$tmp/data/.aienv/.store/gh/github"
echo '{}' >"$tmp/data/.aienv/.store/aa/opencode/auth.json"
echo 'ghp_FAKE_do_not_log' >"$tmp/data/.aienv/.store/gh/github/token"
cat >"$tmp/bin/gh" <<'SH'
#!/bin/bash
echo "gh $*" >>/t/gh.calls
case "$1 $2" in
  "auth setup-git") exit 0 ;;
  "issue view") case "$*" in *title*) echo "Fix the thing" ;; *) echo "body" ;; esac ;;
  "pr list") if [ -f /t/pr ]; then echo 1; else echo 0; fi ;;
  "pr create") touch /t/pr; echo "https://github.com/o/r/pull/1" ;;
esac
SH
cat >"$tmp/bin/opencode" <<'SH'
#!/bin/bash
[ -f "$HOME/.local/share/opencode/auth.json" ] || exit 9
echo "run $(date +%s%N)" >>change.txt
SH
chmod +x "$tmp/bin/"*
git init -q --bare -b main "$tmp/remote.git"
git clone -q "$tmp/remote.git" "$tmp/seed" && git -C "$tmp/seed" -c user.name=t -c user.email=t@t commit -q --allow-empty -m init && git -C "$tmp/seed" push -q origin main

run() { # [data dir]
  docker run --rm -v "$tmp:/t" -v "${1:-$tmp/data}:/data" -e PATH=/t/bin:/home/worker/.opencode/bin:/usr/bin:/bin \
    -e ISSUE=7 -e REPO=o/r -e MODEL=p/m -e CLONE_URL=/t/remote.git opencode-worker-test >"$tmp/log" 2>&1
}
run || fail "first run exit $?"
[ "$(git -C "$tmp/remote.git" rev-list --count main..task/7)" = 1 ] || fail "no commit pushed to task/7"
grep -q 'pr create --repo o/r --base main --head task/7 --title Fix the thing --body Closes #7' "$tmp/gh.calls" || fail "pr create args"
run || fail "second run exit $?"
[ "$(git -C "$tmp/remote.git" rev-list --count main..task/7)" = 2 ] || fail "second run did not build on task/7"
[ "$(grep -c 'pr create' "$tmp/gh.calls")" = 1 ] || fail "second PR opened"
grep -q ghp_FAKE "$tmp/log" && fail "token in log"
mkdir -p "$tmp/nogh/.aienv/.store/aa/opencode" && cp "$tmp/data/.aienv/.store/aa/opencode/auth.json" "$_/"
code=0; run "$tmp/nogh" || code=$?
[ "$code" = 3 ] || fail "missing token: exit $code, want 3"
echo "PASS: opencode-worker (branch, commit, push, PR once, branch reuse, auth check, no token in log)"
