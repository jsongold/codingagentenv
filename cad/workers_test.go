package main

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// resetWorkerStore clears the package-level worker cache between tests, since
// it is shared global state (like pol is for policy tests).
func resetWorkerStore() {
	workerStore.mu.Lock()
	defer workerStore.mu.Unlock()
	workerStore.byProvider = map[string][]Worker{}
	workerStore.fails = map[string]int{}
}

func writePolicy(t *testing.T, allowed ...string) {
	t.Helper()
	p := defaultPolicy()
	p.Providers.Allowed = allowed
	b, _ := json.Marshal(p)
	path := filepath.Join(t.TempDir(), "policy.json")
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CAD_POLICY", path)
}

// writeProvider writes an executable list script under a fresh providers dir
// and points CAD_PROVIDERS_DIR at it; returns the script's path for editing.
func writeProvider(t *testing.T, name, script string) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("CAD_PROVIDERS_DIR", dir)
	path := filepath.Join(dir, name, "list")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

const fakeScript = `#!/bin/sh
cat <<'JSON'
[
  {"id": "fake-1", "state": "running", "startedAt": "2026-01-01T00:00:00Z", "labels": {"role": "test"}},
  {"id": "fake-2", "state": "starting", "startedAt": "2026-01-01T00:05:00Z", "labels": {}},
  {"id": "fake-3", "state": "stopped", "startedAt": "2026-01-01T00:10:00Z", "exitCode": 0, "labels": {"result": "ok"}}
]
JSON
`

func TestPollWorkersFakeProvider(t *testing.T) {
	resetWorkerStore()
	writeProvider(t, "fake", fakeScript)
	writePolicy(t, "fake")

	pollWorkers()
	h := newHub()
	h.publish("workers", currentWorkers())
	rev := h.rev

	srv := httptest.NewServer(newServer(h, ""))
	defer srv.Close()
	var ws []Worker
	if err := json.NewDecoder(get(t, srv.URL+"/v1/workers", "").Body).Decode(&ws); err != nil {
		t.Fatal(err)
	}
	if len(ws) != 3 {
		t.Fatalf("want 3 workers, got %d: %+v", len(ws), ws)
	}
	for _, w := range ws {
		if w.Provider != "fake" {
			t.Errorf("provider not set: %+v", w)
		}
		if w.LastSeenAt.IsZero() {
			t.Errorf("lastSeenAt not set: %+v", w)
		}
	}
	finished := ws[2] // fake-3, sorted after fake-1/fake-2 by id
	if finished.ID != "fake-3" || finished.ExitCode == nil || *finished.ExitCode != 0 || finished.Labels["result"] != "ok" {
		t.Errorf("finished worker: %+v", finished)
	}

	// lastSeenAt-only changes must not republish (changeKey ignores it).
	pollWorkers()
	h.publish("workers", currentWorkers())
	if h.rev != rev {
		t.Fatalf("republished on lastSeenAt-only change: rev %d -> %d", rev, h.rev)
	}
}

func TestPollWorkersNotAllowedIgnored(t *testing.T) {
	resetWorkerStore()
	writeProvider(t, "fake", fakeScript)
	writePolicy(t, "other-provider") // "fake" has a script but isn't allowed

	pollWorkers()
	if ws := currentWorkers(); len(ws) != 0 {
		t.Fatalf("want no workers for a disallowed provider, got %+v", ws)
	}
}

func TestPollWorkersFailingKeepsThenUnknown(t *testing.T) {
	resetWorkerStore()
	script := writeProvider(t, "flaky", fakeScript)
	writePolicy(t, "flaky")

	pollWorkers()
	before := currentWorkers()
	if len(before) != 3 || before[0].State != "running" {
		t.Fatalf("setup: want 3 workers, first running, got %+v", before)
	}

	// Break the script: subsequent polls fail.
	if err := os.WriteFile(script, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	for i := 0; i < workerFailThreshold-1; i++ {
		pollWorkers()
		if ws := currentWorkers(); len(ws) != 3 || ws[0].State != "running" {
			t.Fatalf("poll %d: want previous list kept with state running, got %+v", i, ws)
		}
	}

	pollWorkers() // reaches workerFailThreshold
	after := currentWorkers()
	if len(after) != 3 {
		t.Fatalf("want previous list still present, got %+v", after)
	}
	for _, w := range after {
		if w.State != "unknown" {
			t.Errorf("want state unknown after %d failures, got %+v", workerFailThreshold, w)
		}
	}
}
