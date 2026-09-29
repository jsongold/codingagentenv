package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

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
