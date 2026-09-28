package main

// dispatch for runner mode "vm": start a stopped worker VM (deploy/gcp/create-worker.sh) and run the worker image
// there over IAP ssh. The VM powers itself off after its last worker container exits (deploy/gcp/worker-startup.sh).
// gcloud references:
//   start: waits for the operation unless --async; "Only a stopped virtual machine can be started"
//     https://cloud.google.com/sdk/gcloud/reference/compute/instances/start
//   Spot: a stopped Spot VM may fail to start when the zone lacks capacity
//     https://cloud.google.com/compute/docs/instances/create-use-spot
//   ssh: --tunnel-through-iap, --command https://cloud.google.com/sdk/gcloud/reference/compute/ssh
//   states (STOPPING / PENDING_STOP -> TERMINATED) https://cloud.google.com/compute/docs/instances/instance-life-cycle

import (
	"context"
	"fmt"
	"io"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// gcloud runs gcloud with a timeout; tests replace it.
var gcloud = func(timeout time.Duration, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	out, err := shellCtx(ctx, "", "gcloud", args...)
	if ctx.Err() != nil {
		err = fmt.Errorf("gcloud %s: timeout after %s", strings.Join(args[:min(3, len(args))], " "), timeout)
	}
	return out, err
}

// vmBudget bounds waiting out STOPPING, the start and the readiness probes together; each probe and the final
// docker run get vmCall more at most, so a dispatch returns within ~4 min (the Orchestrator's tool-call limit).
// Tests shorten them.
var (
	vmBudget = 170 * time.Second
	vmCall   = 30 * time.Second
	vmPoll   = 5 * time.Second
	sleep    = time.Sleep
)

// safeArg: values that go into the remote shell command line unquoted.
var safeArg = regexp.MustCompile(`^[A-Za-z0-9._/:@-]+$`)

// dispatchVM returns exit 5 ("computer unavailable") when the VM cannot be started or reached in time, so the
// Orchestrator re-places with `orchd place --exclude <computer>`.
func dispatchVM(rn Runner, repo string, n int, w io.Writer) (int, error) {
	for k, v := range map[string]string{"instance": rn.Instance, "zone": rn.Zone, "project": rn.Project, "image": rn.Image, "model": rn.Model, "repo": repo} {
		if !safeArg.MatchString(v) {
			return 2, fmt.Errorf("--placement: runner.%s %q: want %s", k, v, safeArg)
		}
	}
	t0 := time.Now()
	deadline := t0.Add(vmBudget)
	g := func(d time.Duration, a ...string) (string, error) {
		return gcloud(d, append(a, "--project", rn.Project, "--zone", rn.Zone)...)
	}
	unavailable := func(format string, a ...any) (int, error) {
		return 5, fmt.Errorf("computer unavailable: "+format, a...)
	}
	status := func() (string, error) {
		s, err := g(vmCall, "compute", "instances", "describe", rn.Instance, "--format=value(status)")
		return strings.TrimSpace(s), err
	}
	st, err := status()
	for err == nil && (st == "STOPPING" || st == "PENDING_STOP") {
		if time.Now().After(deadline) {
			return unavailable("%s still %s after %s", rn.Instance, st, vmBudget)
		}
		sleep(vmPoll)
		st, err = status()
	}
	if err != nil {
		return unavailable("%v", err)
	}
	booted := st != "RUNNING"
	switch st {
	case "RUNNING", "PROVISIONING", "STAGING": // already up or being started: the ready wait covers it
	case "TERMINATED":
		if _, err := g(time.Until(deadline), "compute", "instances", "start", rn.Instance); err != nil {
			return unavailable("start %s: %v", rn.Instance, err) // e.g. no Spot capacity in the zone
		}
	default:
		return unavailable("%s is %s", rn.Instance, st)
	}
	ssh := func(cmd string) (string, error) {
		return g(vmCall, "compute", "ssh", rn.Instance, "--tunnel-through-iap", "--quiet", "--command", cmd)
	}
	for ; ; sleep(vmPoll) {
		_, err := ssh("test -e /run/worker-ready")
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			return unavailable("%s not ready within %s: %v", rn.Instance, vmBudget, err)
		}
	}
	name := "opencode-worker-" + strconv.Itoa(n)
	if _, err := ssh(fmt.Sprintf("sudo docker run -d --rm --name %s -v /var/lib/cad:/data -e ISSUE=%d -e REPO=%s -e MODEL=%s %s",
		name, n, repo, rn.Model, rn.Image)); err != nil {
		if st, serr := status(); serr != nil || st != "RUNNING" { // preempted or stopped itself meanwhile
			return unavailable("%s went %s before docker run: %v", rn.Instance, st, err)
		}
		return 1, err
	}
	return 0, printJSON(w, map[string]any{"started": true, "instance": rn.Instance, "container": name, "booted": booted,
		"startSec": math.Round(time.Since(t0).Seconds()*10) / 10})
}
