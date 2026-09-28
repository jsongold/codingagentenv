package main

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

func TestUsageSnapshotRoundTrip(t *testing.T) {
	t.Setenv("CAD_STATE_DIR", t.TempDir()+"/state") // created on first save
	if _, ok := loadUsageSnapshot(time.Now(), time.Hour); ok {
		t.Fatal("loaded a snapshot that does not exist")
	}
	at := time.Date(2026, 9, 28, 1, 2, 3, 0, time.UTC)
	in := UsageMap{"claude/a": {FiveHour: &UsageWindow{UsedPct: 40}, FetchedAt: at}, "codex/b": {Error: "boom"}}
	if err := saveUsageSnapshot(in); err != nil {
		t.Fatal(err)
	}
	m, ok := loadUsageSnapshot(time.Now(), time.Hour)
	if !ok || len(m) != 2 || !m["claude/a"].Stale || !m["codex/b"].Stale || m["claude/a"].FiveHour.UsedPct != 40 ||
		!m["claude/a"].FetchedAt.Equal(at) || m["codex/b"].Error != "boom" {
		t.Fatalf("round trip: %+v ok=%v", m, ok)
	}
	if _, ok := loadUsageSnapshot(time.Now().Add(2*time.Hour), time.Hour); ok {
		t.Fatal("snapshot older than maxAge was loaded")
	}
	os.WriteFile(snapshotFile(), []byte("{torn"), 0o644)
	if _, ok := loadUsageSnapshot(time.Now(), time.Hour); ok {
		t.Fatal("corrupt snapshot was loaded")
	}
}

func TestReadiness(t *testing.T) {
	t.Setenv("CAD_STATE_DIR", t.TempDir())
	ready := func(h *hub) (int, map[string]interface{}) {
		srv := httptest.NewServer(newServer(h, "tok")) // readiness needs no token, like /healthz
		defer srv.Close()
		res := get(t, srv.URL+"/healthz?ready", "")
		defer res.Body.Close()
		var m map[string]interface{}
		json.NewDecoder(res.Body).Decode(&m)
		return res.StatusCode, m
	}
	h := newHub()
	h.loadSnapshot() // none yet
	if code, m := ready(h); code != 503 || m["ready"] != false || m["reason"] == nil {
		t.Fatalf("before collection: %d %v", code, m)
	}
	c := collector{"usage", fixed(time.Minute), func() (interface{}, error) {
		return UsageMap{"claude/a": {FiveHour: &UsageWindow{UsedPct: 10}}}, nil
	}}
	h.collectOne(c)
	if code, m := ready(h); code != 200 || m["usage"] != "collected" {
		t.Fatalf("after collection: %d %v", code, m)
	}
	// restart: a new hub is ready from the snapshot the collection wrote, with stale usage
	h2 := newHub()
	h2.loadSnapshot()
	if code, m := ready(h2); code != 200 || m["usage"] != "snapshot" {
		t.Fatalf("after snapshot load: %d %v", code, m)
	}
	var u UsageMap
	if !h2.topic("usage", &u) || !u["claude/a"].Stale {
		t.Fatalf("snapshot usage not published stale: %+v", u)
	}
	h2.collectOne(c)
	if !h2.topic("usage", &u) || u["claude/a"].Stale {
		t.Fatalf("real collection did not overwrite the snapshot: %+v", u)
	}
	if code, m := ready(h2); code != 200 || m["usage"] != "collected" {
		t.Fatalf("after real collection: %d %v", code, m)
	}
}
