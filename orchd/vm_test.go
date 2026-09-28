package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// fakeCompute is a Compute Engine API for one instance (project p1, zone z1, worker-spot): get answers the next
// status (the last one repeats); setMetadata checks the fingerprint (race: another dispatch changed it right after
// our get); start fails with startErr (HTTP) or opErr (in the operation, after one PENDING poll). Every call is
// recorded as "METHOD path" without the zone prefix.
type fakeCompute struct {
	status          []string
	startErr, opErr string
	race, pollFail  bool
	fp              string
	items           []metaItem
	calls           []string
}

func (f *fakeCompute) install(t *testing.T) {
	t.Helper()
	f.fp = "fp1"
	if f.items == nil {
		f.items = []metaItem{{"worker-task", "old"}, {"startup-script", "#!/bin/bash\nmd instance/attributes/worker-task"}}
	}
	oldTok, oldSleep, oldBudget, oldPoll := accessToken, sleep, vmBudget, vmPoll
	t.Cleanup(func() { accessToken, sleep, vmBudget, vmPoll = oldTok, oldSleep, oldBudget, oldPoll })
	vmBudget, vmPoll, sleep = 50*time.Millisecond, time.Millisecond, time.Sleep
	accessToken = func() (string, error) { return "tok", nil }
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, ok := strings.CutPrefix(r.URL.Path, "/projects/p1/zones/z1/")
		if !ok || r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("bad request %s %v", r.URL.Path, r.Header)
		}
		f.calls = append(f.calls, r.Method+" "+p)
		switch {
		case r.Method == "GET" && p == "instances/worker-spot":
			s := f.status[0]
			if len(f.status) > 1 {
				f.status = f.status[1:]
			}
			json.NewEncoder(w).Encode(map[string]any{"status": s, "metadata": map[string]any{"fingerprint": f.fp, "items": f.items}})
			if f.race {
				f.fp = "other"
			}
		case r.Method == "POST" && p == "instances/worker-spot/setMetadata":
			var b struct {
				Fingerprint string
				Items       []metaItem
			}
			json.NewDecoder(r.Body).Decode(&b)
			if b.Fingerprint != f.fp {
				http.Error(w, `{"error":{"code":412,"message":"fingerprint"}}`, 412)
				return
			}
			f.items, f.fp = b.Items, "fp2"
			io.WriteString(w, `{"name":"op-md","status":"DONE"}`)
		case r.Method == "POST" && p == "instances/worker-spot/start":
			if f.startErr != "" {
				http.Error(w, f.startErr, 503)
				return
			}
			io.WriteString(w, `{"name":"op-start","status":"PENDING"}`)
		case r.Method == "GET" && p == "operations/op-start":
			if f.pollFail {
				http.Error(w, "backend error", 500)
				return
			}
			if f.opErr != "" {
				fmt.Fprintf(w, `{"name":"op-start","status":"DONE","error":{"errors":[{"code":%q,"message":"m"}]}}`, f.opErr)
				return
			}
			io.WriteString(w, `{"name":"op-start","status":"DONE"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	t.Setenv("ORCHD_COMPUTE_URL", srv.URL)
}

func (f *fakeCompute) task() string {
	for _, it := range f.items {
		if it.Key == "worker-task" {
			return it.Value
		}
	}
	return ""
}

const vmPl = `{"runner":{"mode":"vm","instance":"worker-spot","zone":"z1","project":"p1","image":"ghcr.io/o/w:main","model":"prov/m-1"}}`

func TestDispatchVMStartsStoppedVM(t *testing.T) {
	taskEnv(t, reg)
	calls := fakeShell(t, nil)
	f := &fakeCompute{status: []string{"TERMINATED"}}
	f.install(t)
	code, m, errs := runTask(t, "dispatch", "--issue", "7", "--placement", vmPl)
	if code != 0 || m["started"] != true || m["instance"] != "worker-spot" || m["container"] != "opencode-worker-7" {
		t.Fatalf("code %d %v %s", code, m, errs)
	}
	if m["zone"] != "z1" || m["project"] != "p1" { // what "orchd vm status" needs besides instance
		t.Errorf("zone %v project %v", m["zone"], m["project"])
	}
	if _, ok := m["startSec"].(float64); !ok {
		t.Errorf("startSec %v", m["startSec"])
	}
	want := "GET instances/worker-spot,POST instances/worker-spot/setMetadata,POST instances/worker-spot/start,GET operations/op-start"
	if got := strings.Join(f.calls, ","); got != want {
		t.Errorf("calls\n got %s\nwant %s", got, want)
	}
	task := f.task()
	if !strings.HasPrefix(task, "7-") || !strings.HasSuffix(task, " 7 o/r prov/m-1 ghcr.io/o/w:main") || task != m["task"] {
		t.Errorf("task %q (printed %v)", task, m["task"])
	}
	if len(f.items) != 2 || f.items[1].Key != "startup-script" { // other metadata kept, the old task replaced
		t.Errorf("items %v", f.items)
	}
	if len(*calls) != 0 { // no gh, no git, no gcloud: the worker reads the issue itself
		t.Errorf("shell calls %v", *calls)
	}
}

func TestDispatchVMWaitsOutStopping(t *testing.T) {
	taskEnv(t, reg)
	fakeShell(t, nil)
	f := &fakeCompute{status: []string{"STOPPING", "PENDING_STOP", "TERMINATED"}} // self-stop in progress
	f.install(t)
	if code, _, errs := runTask(t, "dispatch", "--issue", "7", "--placement", vmPl); code != 0 || len(f.calls) != 6 {
		t.Fatalf("code %d %s calls %v", code, errs, f.calls)
	}
}

func TestDispatchVMUnavailable(t *testing.T) {
	taskEnv(t, reg)
	fakeShell(t, nil)
	for name, f := range map[string]*fakeCompute{
		"busy":                  {status: []string{"RUNNING"}},
		"being started":         {status: []string{"STAGING"}},
		"stuck stopping":        {status: []string{"STOPPING"}},
		"concurrent dispatch":   {status: []string{"TERMINATED"}, race: true},
		"start refused":         {status: []string{"TERMINATED"}, startErr: "ZONE_RESOURCE_POOL_EXHAUSTED"},
		"no spot capacity (op)": {status: []string{"TERMINATED"}, opErr: "ZONE_RESOURCE_POOL_EXHAUSTED"},
		"old startup script":    {status: []string{"TERMINATED"}, items: []metaItem{{"startup-script", "#!/bin/bash"}}},
		"reserved by a fresh task": {status: []string{"TERMINATED"}, items: []metaItem{{"startup-script", "worker-task"},
			{"worker-task", fmt.Sprintf("8-%d 8 o/r m i", time.Now().Unix()-10)}}},
	} {
		f.install(t)
		code, _, errs := runTask(t, "dispatch", "--issue", "7", "--placement", vmPl)
		if code != 5 || !strings.Contains(errs, "computer unavailable") {
			t.Errorf("%s: code %d %s", name, code, errs)
		}
		if (name == "busy" || name == "concurrent dispatch") && f.task() != "old" {
			t.Errorf("%s: task overwritten: %q", name, f.task())
		}
		if f.startErr+f.opErr != "" && (f.task() != "" || len(f.items) != 1) { // start failed: our task taken back
			t.Errorf("%s: stale task left: %v", name, f.items)
		}
	}
	f := &fakeCompute{status: []string{"TERMINATED", "STAGING"}, pollFail: true} // start accepted, poll failed: it is starting
	f.install(t)
	if code, m, errs := runTask(t, "dispatch", "--issue", "7", "--placement", vmPl); code != 0 || m["started"] != true {
		t.Errorf("accepted start: code %d %s", code, errs)
	}
	f = &fakeCompute{status: []string{"TERMINATED"}, items: []metaItem{{"startup-script", "worker-task"},
		{"worker-task", fmt.Sprintf("8-%d 8 o/r m i", time.Now().Add(-time.Hour).Unix())}}} // finished task: not a reservation
	f.install(t)
	if code, _, errs := runTask(t, "dispatch", "--issue", "7", "--placement", vmPl); code != 0 {
		t.Errorf("old task: code %d %s", code, errs)
	}
	t.Setenv("ORCHD_COMPUTE_URL", "http://127.0.0.1:1") // API unreachable
	if code, _, errs := runTask(t, "dispatch", "--issue", "7", "--placement", vmPl); code != 5 {
		t.Errorf("unreachable: code %d %s", code, errs)
	}
}

// TestDispatchVMPicksAnyStoppedInstance: runner.instance lists two VMs of the same kind (create-worker.sh, up to
// 3); the first is busy, so dispatch skips it and starts the second stopped one.
func TestDispatchVMPicksAnyStoppedInstance(t *testing.T) {
	taskEnv(t, reg)
	fakeShell(t, nil)
	oldTok, oldSleep, oldBudget, oldPoll := accessToken, sleep, vmBudget, vmPoll
	t.Cleanup(func() { accessToken, sleep, vmBudget, vmPoll = oldTok, oldSleep, oldBudget, oldPoll })
	vmBudget, vmPoll, sleep = 50*time.Millisecond, time.Millisecond, time.Sleep
	accessToken = func() (string, error) { return "tok", nil }
	items := map[string]any{"fingerprint": "fp1", "items": []metaItem{{"startup-script", "worker-task"}}}
	var calls []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, _ := strings.CutPrefix(r.URL.Path, "/projects/p1/zones/z1/")
		calls = append(calls, r.Method+" "+p)
		switch {
		case r.Method == "GET" && p == "instances/worker-spot":
			json.NewEncoder(w).Encode(map[string]any{"status": "RUNNING", "metadata": items})
		case r.Method == "GET" && p == "instances/worker-spot-2":
			json.NewEncoder(w).Encode(map[string]any{"status": "TERMINATED", "metadata": items})
		case r.Method == "POST" && p == "instances/worker-spot-2/setMetadata":
			io.WriteString(w, `{"name":"op-md","status":"DONE"}`)
		case r.Method == "POST" && p == "instances/worker-spot-2/start":
			io.WriteString(w, `{"name":"op-start","status":"DONE"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	t.Setenv("ORCHD_COMPUTE_URL", srv.URL)
	pl := `{"runner":{"mode":"vm","instance":"worker-spot,worker-spot-2","zone":"z1","project":"p1","image":"ghcr.io/o/w:main","model":"prov/m-1"}}`
	code, m, errs := runTask(t, "dispatch", "--issue", "7", "--placement", pl)
	if code != 0 || m["instance"] != "worker-spot-2" || m["zone"] != "z1" || m["project"] != "p1" || m["container"] != "opencode-worker-7" {
		t.Fatalf("code %d %v %s", code, m, errs)
	}
	if len(calls) < 2 || calls[0] != "GET instances/worker-spot" || calls[1] != "GET instances/worker-spot-2" {
		t.Errorf("did not check worker-spot before worker-spot-2: %v", calls)
	}
}

// TestDispatchVMSkipsReservedInstance: the first instance is TERMINATED but reserved by a fresh worker-task
// (another dispatch mid-flight); the second is TERMINATED and free. Regression for a bug where a reserved (or
// old-startup-script) first candidate reported the whole computer unavailable instead of trying the next one.
func TestDispatchVMSkipsReservedInstance(t *testing.T) {
	taskEnv(t, reg)
	fakeShell(t, nil)
	oldTok, oldSleep, oldBudget, oldPoll := accessToken, sleep, vmBudget, vmPoll
	t.Cleanup(func() { accessToken, sleep, vmBudget, vmPoll = oldTok, oldSleep, oldBudget, oldPoll })
	vmBudget, vmPoll, sleep = 50*time.Millisecond, time.Millisecond, time.Sleep
	accessToken = func() (string, error) { return "tok", nil }
	reserved := map[string]any{"fingerprint": "fp1", "items": []metaItem{
		{"startup-script", "worker-task"},
		{"worker-task", fmt.Sprintf("8-%d 8 o/r m i", time.Now().Unix()-10)},
	}}
	free := map[string]any{"fingerprint": "fp1", "items": []metaItem{{"startup-script", "worker-task"}}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, _ := strings.CutPrefix(r.URL.Path, "/projects/p1/zones/z1/")
		switch {
		case r.Method == "GET" && p == "instances/worker-spot":
			json.NewEncoder(w).Encode(map[string]any{"status": "TERMINATED", "metadata": reserved})
		case r.Method == "GET" && p == "instances/worker-spot-2":
			json.NewEncoder(w).Encode(map[string]any{"status": "TERMINATED", "metadata": free})
		case r.Method == "POST" && p == "instances/worker-spot-2/setMetadata":
			io.WriteString(w, `{"name":"op-md","status":"DONE"}`)
		case r.Method == "POST" && p == "instances/worker-spot-2/start":
			io.WriteString(w, `{"name":"op-start","status":"DONE"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	t.Setenv("ORCHD_COMPUTE_URL", srv.URL)
	pl := `{"runner":{"mode":"vm","instance":"worker-spot,worker-spot-2","zone":"z1","project":"p1","image":"ghcr.io/o/w:main","model":"prov/m-1"}}`
	code, m, errs := runTask(t, "dispatch", "--issue", "7", "--placement", pl)
	if code != 0 || m["instance"] != "worker-spot-2" {
		t.Fatalf("code %d %v %s", code, m, errs)
	}
}

// TestDispatchVMAllInstancesBusy: every VM of the kind is running another task.
func TestDispatchVMAllInstancesBusy(t *testing.T) {
	taskEnv(t, reg)
	fakeShell(t, nil)
	oldTok, oldSleep, oldBudget, oldPoll := accessToken, sleep, vmBudget, vmPoll
	t.Cleanup(func() { accessToken, sleep, vmBudget, vmPoll = oldTok, oldSleep, oldBudget, oldPoll })
	vmBudget, vmPoll, sleep = 50*time.Millisecond, time.Millisecond, time.Sleep
	accessToken = func() (string, error) { return "tok", nil }
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, _ := strings.CutPrefix(r.URL.Path, "/projects/p1/zones/z1/")
		switch p {
		case "instances/worker-spot":
			json.NewEncoder(w).Encode(map[string]any{"status": "RUNNING", "metadata": map[string]any{"fingerprint": "fp1"}})
		case "instances/worker-spot-2":
			json.NewEncoder(w).Encode(map[string]any{"status": "STAGING", "metadata": map[string]any{"fingerprint": "fp1"}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	t.Setenv("ORCHD_COMPUTE_URL", srv.URL)
	pl := `{"runner":{"mode":"vm","instance":"worker-spot,worker-spot-2","zone":"z1","project":"p1","image":"ghcr.io/o/w:main","model":"prov/m-1"}}`
	if code, _, errs := runTask(t, "dispatch", "--issue", "7", "--placement", pl); code != 5 || !strings.Contains(errs, "computer unavailable") {
		t.Errorf("code %d %s", code, errs)
	}
}

// TestDispatchVMRejectsStartedWhileWaiting: the instance is STOPPING, but by the time it stops it is RUNNING
// (another actor started it), not TERMINATED. Regression for a bug where only the metadata (not the status) was
// re-checked after the wait, so a VM someone else started could still be assigned this task.
func TestDispatchVMRejectsStartedWhileWaiting(t *testing.T) {
	taskEnv(t, reg)
	fakeShell(t, nil)
	f := &fakeCompute{status: []string{"STOPPING", "RUNNING"}}
	f.install(t)
	code, _, errs := runTask(t, "dispatch", "--issue", "7", "--placement", vmPl)
	if code != 5 || !strings.Contains(errs, "computer unavailable") {
		t.Fatalf("code %d %s", code, errs)
	}
	if f.task() != "old" {
		t.Errorf("task overwritten: %q", f.task())
	}
}

// TestDispatchVMRetriesAfterFingerprintConflict: the first candidate's setMetadata always 412s (another dispatch
// grabbed it between our GET and our POST); dispatch must retry with the next stopped instance instead of exiting
// with "computer unavailable". Regression for a bug where a fingerprint conflict failed the whole dispatch even
// when another instance was free.
func TestDispatchVMRetriesAfterFingerprintConflict(t *testing.T) {
	taskEnv(t, reg)
	fakeShell(t, nil)
	oldTok, oldSleep, oldBudget, oldPoll := accessToken, sleep, vmBudget, vmPoll
	t.Cleanup(func() { accessToken, sleep, vmBudget, vmPoll = oldTok, oldSleep, oldBudget, oldPoll })
	vmBudget, vmPoll, sleep = 50*time.Millisecond, time.Millisecond, time.Sleep
	accessToken = func() (string, error) { return "tok", nil }
	free := map[string]any{"fingerprint": "fp1", "items": []metaItem{{"startup-script", "worker-task"}}}
	var calls []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, _ := strings.CutPrefix(r.URL.Path, "/projects/p1/zones/z1/")
		calls = append(calls, r.Method+" "+p)
		switch {
		case r.Method == "GET" && p == "instances/worker-spot":
			json.NewEncoder(w).Encode(map[string]any{"status": "TERMINATED", "metadata": free})
		case r.Method == "GET" && p == "instances/worker-spot-2":
			json.NewEncoder(w).Encode(map[string]any{"status": "TERMINATED", "metadata": free})
		case r.Method == "POST" && p == "instances/worker-spot/setMetadata": // always conflicts: raced away
			http.Error(w, `{"error":{"code":412,"message":"fingerprint"}}`, 412)
		case r.Method == "POST" && p == "instances/worker-spot-2/setMetadata":
			io.WriteString(w, `{"name":"op-md","status":"DONE"}`)
		case r.Method == "POST" && p == "instances/worker-spot-2/start":
			io.WriteString(w, `{"name":"op-start","status":"DONE"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	t.Setenv("ORCHD_COMPUTE_URL", srv.URL)
	pl := `{"runner":{"mode":"vm","instance":"worker-spot,worker-spot-2","zone":"z1","project":"p1","image":"ghcr.io/o/w:main","model":"prov/m-1"}}`
	code, m, errs := runTask(t, "dispatch", "--issue", "7", "--placement", pl)
	if code != 0 || m["instance"] != "worker-spot-2" {
		t.Fatalf("code %d %v %s", code, m, errs)
	}
	want := "GET instances/worker-spot,POST instances/worker-spot/setMetadata,GET instances/worker-spot-2,POST instances/worker-spot-2/setMetadata,POST instances/worker-spot-2/start"
	if got := strings.Join(calls, ","); got != want {
		t.Errorf("calls\n got %s\nwant %s", got, want)
	}
}

func TestDispatchVMBadRunner(t *testing.T) {
	taskEnv(t, reg)
	fakeShell(t, nil)
	f := &fakeCompute{status: []string{"TERMINATED"}}
	f.install(t)
	bad := strings.Replace(vmPl, "prov/m-1", "m; rm -rf /", 1)
	if code, _, _ := runTask(t, "dispatch", "--issue", "7", "--placement", bad); code != 2 || len(f.calls) != 0 {
		t.Errorf("unsafe model: code %d calls %v", code, f.calls)
	}
}

// TestVMStatus: read-only status check for one instance (skills/orchestrate's monitor Subagent, issue #67).
func TestVMStatus(t *testing.T) {
	oldTok := accessToken
	t.Cleanup(func() { accessToken = oldTok })
	accessToken = func() (string, error) { return "tok", nil }
	var calls []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		if r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("bad auth header %v", r.Header)
		}
		if r.URL.Path != "/projects/p1/zones/z1/instances/worker-spot" {
			http.NotFound(w, r)
			return
		}
		io.WriteString(w, `{"status":"TERMINATED","metadata":{"fingerprint":"fp1","items":[]}}`)
	}))
	defer srv.Close()
	t.Setenv("ORCHD_COMPUTE_URL", srv.URL)
	code, m, errs := runTask(t, "vm", "status", "--instance", "worker-spot", "--zone", "z1", "--project", "p1")
	if code != 0 || m["instance"] != "worker-spot" || m["status"] != "TERMINATED" {
		t.Fatalf("code %d %v %s", code, m, errs)
	}
	if len(calls) != 1 || calls[0] != "GET /projects/p1/zones/z1/instances/worker-spot" {
		t.Errorf("calls %v", calls)
	}
}

func TestVMStatusErrors(t *testing.T) {
	oldTok := accessToken
	t.Cleanup(func() { accessToken = oldTok })
	accessToken = func() (string, error) { return "tok", nil }

	if code, _, errs := runTask(t, "vm"); code != 2 || !strings.Contains(errs, "want status") {
		t.Errorf("no subcommand: code %d %s", code, errs)
	}
	if code, _, errs := runTask(t, "vm", "bogus"); code != 2 || !strings.Contains(errs, "want status") {
		t.Errorf("bad subcommand: code %d %s", code, errs)
	}
	if code, _, errs := runTask(t, "vm", "status", "--instance", "w; rm -rf /", "--zone", "z1", "--project", "p1"); code != 2 || !strings.Contains(errs, "--instance") {
		t.Errorf("unsafe instance: code %d %s", code, errs)
	}
	if code, _, errs := runTask(t, "vm", "status", "--instance", "worker-spot", "--zone", "z1"); code != 2 || !strings.Contains(errs, "--project") {
		t.Errorf("missing project: code %d %s", code, errs)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no", 404)
	}))
	defer srv.Close()
	t.Setenv("ORCHD_COMPUTE_URL", srv.URL)
	if code, _, errs := runTask(t, "vm", "status", "--instance", "worker-spot", "--zone", "z1", "--project", "p1"); code != 1 {
		t.Errorf("API error: code %d %s", code, errs)
	}
}

func TestAccessTokenFromMetadataServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Metadata-Flavor") != "Google" || r.URL.Path != "/computeMetadata/v1/instance/service-accounts/default/token" {
			http.Error(w, "no", 403)
			return
		}
		io.WriteString(w, `{"access_token":"ya29.x","expires_in":3599,"token_type":"Bearer"}`)
	}))
	defer srv.Close()
	t.Setenv("GCE_METADATA_HOST", strings.TrimPrefix(srv.URL, "http://"))
	if tok, err := accessToken(); err != nil || tok != "ya29.x" {
		t.Errorf("token %q %v", tok, err)
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
