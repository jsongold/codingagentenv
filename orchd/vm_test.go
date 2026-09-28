package main

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

// fakeGcloud answers gcloud calls: status returns the next describe status (the last one repeats), start and ssh
// (readiness probe, sshFailures times) and docker run fail per the fields. Every call is recorded without the --project/--zone suffix.
type fakeGcloud struct {
	status              []string
	startErr, runErr    error
	sshFailures, starts int
	calls               []string
}

func (f *fakeGcloud) install(t *testing.T) {
	t.Helper()
	old, oldSleep := gcloud, sleep
	oldWaits := []time.Duration{vmStopWait, vmStartTimeout, vmReadyWait, vmPoll}
	t.Cleanup(func() {
		gcloud, sleep = old, oldSleep
		vmStopWait, vmStartTimeout, vmReadyWait, vmPoll = oldWaits[0], oldWaits[1], oldWaits[2], oldWaits[3]
	})
	vmStopWait, vmReadyWait, vmPoll = 50*time.Millisecond, 50*time.Millisecond, time.Millisecond
	sleep = time.Sleep
	gcloud = func(_ time.Duration, args ...string) (string, error) {
		if got := strings.Join(args[len(args)-4:], " "); got != "--project p1 --zone z1" {
			t.Errorf("gcloud without project/zone: %v", args)
		}
		c := strings.Join(args[:len(args)-4], " ")
		f.calls = append(f.calls, c)
		switch {
		case strings.HasPrefix(c, "compute instances describe"):
			s := f.status[0]
			if len(f.status) > 1 {
				f.status = f.status[1:]
			}
			return s + "\n", nil
		case strings.HasPrefix(c, "compute instances start"):
			f.starts++
			return "", f.startErr
		case strings.Contains(c, "test -e /run/worker-ready") && f.sshFailures > 0:
			f.sshFailures--
			return "", errors.New("ssh: connection refused")
		case strings.Contains(c, "docker run"):
			return "", f.runErr
		}
		return "", nil
	}
}

const vmPl = `{"runner":{"mode":"vm","instance":"worker-spot","zone":"z1","project":"p1","image":"ghcr.io/o/w:main","model":"prov/m-1"}}`

func TestDispatchVMStartsStoppedVM(t *testing.T) {
	taskEnv(t, reg)
	calls := fakeShell(t, nil)
	f := &fakeGcloud{status: []string{"TERMINATED"}, sshFailures: 2}
	f.install(t)
	code, m, errs := runTask(t, "dispatch", "--issue", "7", "--placement", vmPl)
	if code != 0 || m["started"] != true || m["instance"] != "worker-spot" || m["container"] != "opencode-worker-7" || m["booted"] != true {
		t.Fatalf("code %d %v %s", code, m, errs)
	}
	if _, ok := m["startSec"].(float64); !ok {
		t.Errorf("startSec %v", m["startSec"])
	}
	if f.starts != 1 {
		t.Errorf("starts %d", f.starts)
	}
	want := "compute ssh worker-spot --tunnel-through-iap --quiet --command sudo docker run -d --rm --name opencode-worker-7 -v /var/lib/cad:/data -e ISSUE=7 -e REPO=o/r -e MODEL=prov/m-1 ghcr.io/o/w:main"
	if last := f.calls[len(f.calls)-1]; last != want {
		t.Errorf("docker run call\n got %s\nwant %s", last, want)
	}
	if len(*calls) != 0 { // no gh, no git: the worker reads the issue itself
		t.Errorf("shell calls %v", *calls)
	}
}

func TestDispatchVMRunningAndStopping(t *testing.T) {
	taskEnv(t, reg)
	fakeShell(t, nil)
	f := &fakeGcloud{status: []string{"RUNNING"}}
	f.install(t)
	if code, m, errs := runTask(t, "dispatch", "--issue", "7", "--placement", vmPl); code != 0 || m["booted"] != false || f.starts != 0 {
		t.Fatalf("running: code %d %v %s starts %d", code, m, errs, f.starts)
	}
	f = &fakeGcloud{status: []string{"STOPPING", "STOPPING", "TERMINATED"}} // self-stop in progress: wait, then start
	f.install(t)
	if code, _, errs := runTask(t, "dispatch", "--issue", "7", "--placement", vmPl); code != 0 || f.starts != 1 {
		t.Fatalf("stopping: code %d %s starts %d", code, errs, f.starts)
	}
}

func TestDispatchVMUnavailable(t *testing.T) {
	taskEnv(t, reg)
	fakeShell(t, nil)
	for name, f := range map[string]*fakeGcloud{
		"no spot capacity": {status: []string{"TERMINATED"}, startErr: errors.New("ZONE_RESOURCE_POOL_EXHAUSTED")},
		"never ready":      {status: []string{"TERMINATED"}, sshFailures: 1 << 30},
		"stuck stopping":   {status: []string{"STOPPING"}},
		"repairing":        {status: []string{"REPAIRING"}},
	} {
		f.install(t)
		code, _, errs := runTask(t, "dispatch", "--issue", "7", "--placement", vmPl)
		if code != 5 || !strings.Contains(errs, "computer unavailable") {
			t.Errorf("%s: code %d %s", name, code, errs)
		}
	}
	f := &fakeGcloud{status: []string{"RUNNING"}, runErr: errors.New("docker: Conflict")} // container of this issue already runs
	f.install(t)
	if code, _, _ := runTask(t, "dispatch", "--issue", "7", "--placement", vmPl); code != 1 {
		t.Errorf("docker run failure: code %d", code)
	}
}

func TestDispatchVMBadRunner(t *testing.T) {
	taskEnv(t, reg)
	fakeShell(t, nil)
	f := &fakeGcloud{status: []string{"RUNNING"}}
	f.install(t)
	bad := strings.Replace(vmPl, "prov/m-1", "m; rm -rf /", 1)
	if code, _, _ := runTask(t, "dispatch", "--issue", "7", "--placement", bad); code != 2 || len(f.calls) != 0 {
		t.Errorf("unsafe model: code %d calls %v", code, f.calls)
	}
}

func TestPolicyVMRunnerNeedsFields(t *testing.T) {
	f := t.TempDir() + "/p.json"
	os.WriteFile(f, []byte(`{"classes":{"c":{}},"runners":{"opencode@gce-spot":{"mode":"vm","instance":"w","zone":"z","project":"p","model":"m"}}}`), 0o644)
	t.Setenv("ORCHD_POLICY", f)
	if _, err := loadPolicy(); err == nil || !strings.Contains(err.Error(), "vm needs instance, zone, project, image and model") {
		t.Errorf("missing image: %v", err)
	}
	t.Setenv("ORCHD_POLICY", "policy.json")
	if _, err := loadPolicy(); err != nil {
		t.Errorf("seed policy: %v", err)
	}
}
