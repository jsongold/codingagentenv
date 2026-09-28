#!/usr/bin/env bash
# Runs deploy/gcp/worker-startup.sh in a throwaway debian container with fake curl (metadata), docker, systemctl
# and shutdown, then drives the scripts it generated: the die-event watcher (worker-stop.sh) powers off only after
# the pull of a cached image runs after ready; an opencode-worker-* container dies and none runs after the grace; the idle safety net only after idle-minutes.
# Run: bash test/worker-startup.test.sh   (needs docker)
set -euo pipefail
root=$(cd "$(dirname "$0")/.." && pwd)
docker info >/dev/null 2>&1 || { echo "skip: docker is not running"; exit 0; }
# shellcheck disable=SC2016 # the script runs in the container
docker run --rm -i -v "$root/deploy/gcp/worker-startup.sh:/s.sh:ro" debian:bookworm-slim bash -s <<'IN'
set -euo pipefail
fail() { echo "FAIL: $*"; exit 1; }
mkdir -p /fake /etc/systemd/system && export PATH=/fake:$PATH
cat >/fake/curl <<'SH'
#!/bin/bash
case "$*" in *stop-grace-seconds*) echo 1 ;; *idle-minutes*) echo 2 ;; *) exit 22 ;; esac
SH
cat >/fake/docker <<'SH'
#!/bin/bash
echo "docker $*" >>/calls
case $1 in ps) cat /ps 2>/dev/null ;; events) cat /events ;; esac
SH
printf '#!/bin/bash\necho "$0 $*" >>/calls\n' >/fake/systemctl
printf '#!/bin/bash\necho shutdown >>/down\n' >/fake/shutdown
printf '#!/bin/bash\n' >/fake/chown
chmod +x /fake/*
bash /s.sh >/log
grep -q 'ready (stop-grace-seconds=1, idle-minutes=2)' /log || fail "metadata not read: $(cat /log)"
[ -e /run/worker-ready ] || fail "no ready marker"
grep -q 'systemctl start worker-stop.service' /calls || fail "watcher not started"
tail -1 /calls | grep -q 'docker pull' || fail "cached image: pull should run last (after ready)"

printf 'fetch-auth-x\nopencode-worker-7\n' >/events; echo abc >/ps
bash /etc/worker-stop.sh; [ ! -e /down ] || fail "shut down while a worker runs"
: >/ps; printf 'other\n' >/events
bash /etc/worker-stop.sh; [ ! -e /down ] || fail "shut down on a non-worker die"
printf 'opencode-worker-7\n' >/events
bash /etc/worker-stop.sh; [ -e /down ] || fail "no shutdown after the last worker died"
rm /down

bash /etc/worker-idle.sh; bash /etc/worker-idle.sh; [ ! -e /down ] || fail "idle shutdown too early"
echo $(( $(date +%s) - 121 )) >/run/worker-idle-since
echo abc >/ps; bash /etc/worker-idle.sh; [ ! -e /down ] || fail "idle shutdown while a worker runs"
echo $(( $(date +%s) - 121 )) >/run/worker-idle-since
: >/ps; bash /etc/worker-idle.sh; [ -e /down ] || fail "no idle shutdown after idle-minutes"
echo "PASS: worker-startup (metadata, ready marker, die-event stop with grace, idle safety net)"
IN
