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
	race            bool
	fp              string
	items           []metaItem
	calls           []string
}

func (f *fakeCompute) install(t *testing.T) {
	t.Helper()
	f.fp = "fp1"
	f.items = []metaItem{{"worker-task", "old"}, {"startup-script", "#!/bin/bash"}}
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
	} {
		f.install(t)
		code, _, errs := runTask(t, "dispatch", "--issue", "7", "--placement", vmPl)
		if code != 5 || !strings.Contains(errs, "computer unavailable") {
			t.Errorf("%s: code %d %s", name, code, errs)
		}
		if (name == "busy" || name == "concurrent dispatch") && f.task() != "old" {
			t.Errorf("%s: task overwritten: %q", name, f.task())
		}
	}
	t.Setenv("ORCHD_COMPUTE_URL", "http://127.0.0.1:1") // API unreachable
	if code, _, errs := runTask(t, "dispatch", "--issue", "7", "--placement", vmPl); code != 5 {
		t.Errorf("unreachable: code %d %s", code, errs)
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
