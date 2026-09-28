#!/usr/bin/env bash
# Runs deploy/gcp/worker-startup.sh in a throwaway debian container with fake curl (metadata), docker, systemctl
# and shutdown: the task from metadata worker-task runs once (not again on a reboot), the pull of a cached image runs
# after it; the die-event watcher (worker-stop.sh) powers off only after an opencode-worker-* container dies and none
# runs after the grace; the idle safety net only after idle-minutes.
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
case "$*" in *stop-grace-seconds*) echo 1 ;; *idle-minutes*) echo 2 ;; *worker-task*) cat /task 2>/dev/null || exit 22 ;; *) exit 22 ;; esac
SH
cat >/fake/docker <<'SH'
#!/bin/bash
echo "docker $*" >>/calls
case $1 in ps) cat /ps 2>/dev/null ;; events) cat /events ;; image) [ -e /img ] ;; pull) [ -e /nopull ] && exit 1; touch /img ;; esac
SH
printf '#!/bin/bash\necho "$0 $*" >>/calls\n' >/fake/systemctl
printf '#!/bin/bash\necho shutdown >>/down\n' >/fake/shutdown
printf '#!/bin/bash\n' >/fake/chown
chmod +x /fake/*
run='docker run -d --rm --name opencode-worker-7 -v /var/lib/cad:/data -e ISSUE=7 -e REPO=o/r -e MODEL=prov/m-1 ghcr.io/o/w:main'
bash /s.sh >/log                    # first boot (create-worker.sh): no task, pulls before
grep -q 'worker: no task' /log || fail "no-task boot: $(cat /log)"
grep -q 'docker run -d' /calls && fail "ran a worker without a task"
head -3 /calls | grep -q 'docker pull' || fail "first boot: pull should run first"
echo '7-100 7 o/r prov/m-1 ghcr.io/o/w:main' >/task; : >/calls
bash /s.sh >/log                    # dispatched boot, cached image
grep -q 'ready (stop-grace-seconds=1, idle-minutes=2)' /log || fail "metadata not read: $(cat /log)"
grep -qx "$run" /calls || fail "task not run: $(cat /calls)"
grep -q 'systemctl start worker-stop.service' /calls || fail "watcher not started"
tail -1 /calls | grep -q 'docker pull' || fail "cached image: pull should run last (after the task)"
: >/calls; bash /s.sh >/log         # reboot with the same task: consumed
grep -q "docker run -d" /calls && fail "task ran twice"
grep -q 'task 7-100 already ran' /log || fail "no skip: $(cat /log)"
echo '7-200 7 o/r prov/m-1 ghcr.io/o/w:main' >/task; : >/calls
bash /s.sh >/log; grep -qx "$run" /calls || fail "next task not run"

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
echo "PASS: worker-startup (metadata task once, die-event stop with grace, idle safety net)"
IN
