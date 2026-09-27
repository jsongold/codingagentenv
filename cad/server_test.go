package main

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestSlots(t *testing.T) {
	two := 2
	g := Gate{MemoryMB: 1500, CPUs: 1, ReserveMB: 2048}
	cases := []struct {
		free, cpus int
		g          Gate
		env        string
		want       int
	}{
		{2048 + 4500, 8, g, "", 3},                          // memory bound
		{2048 + 15000, 4, g, "", 4},                         // cpu bound
		{1000, 8, g, "", 0},                                 // below reserve clamps to 0
		{2048 + 15000, 8, Gate{1500, 1, 2048, &two}, "", 2}, // maxSlots
		{1000, 1, g, "7", 7},                                // CAD_SLOTS wins
		{2048 + 15000, 8, Gate{}, "", 0},                    // no policy
	}
	for i, c := range cases {
		t.Setenv("CAD_SLOTS", c.env)
		if got := slots(c.free, c.cpus, c.g); got != c.want {
			t.Errorf("case %d: slots=%d want %d", i, got, c.want)
		}
	}
}

func get(t *testing.T, url, token string) *http.Response {
	req, _ := http.NewRequest("GET", url, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestAuth(t *testing.T) {
	srv := httptest.NewServer(newServer(newHub(), "s3cret"))
	defer srv.Close()
	for _, c := range []struct {
		path, token string
		want        int
	}{{"/healthz", "", 200}, {"/v1/meta?ns=t", "", 401}, {"/v1/meta?ns=t", "wrong", 401}, {"/v1/meta?ns=t", "s3cret", 200}, {"/v1/meta", "s3cret", 400}, {"/v1/quota", "s3cret", 400}, {"/v1/meta?ns=Bad!", "s3cret", 400}} {
		if res := get(t, srv.URL+c.path, c.token); res.StatusCode != c.want {
			t.Errorf("%s token=%q: %d want %d", c.path, c.token, res.StatusCode, c.want)
		}
	}
}

func TestCapacityShape(t *testing.T) {
	h := newHub()
	v, err := collectCapacity()
	if err != nil {
		t.Fatal(err)
	}
	h.publish("capacity", v)
	srv := httptest.NewServer(newServer(h, ""))
	defer srv.Close()
	var m map[string]interface{}
	if err := json.NewDecoder(get(t, srv.URL+"/v1/capacity?ns=t", "").Body).Decode(&m); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"host", "collectedAt", "memTotalMB", "memFreeMB", "cpus", "load1", "slots"} {
		if _, ok := m[k]; !ok {
			t.Errorf("missing %s in %v", k, m)
		}
	}
	if m["memTotalMB"].(float64) <= 0 || m["memFreeMB"].(float64) <= 0 {
		t.Errorf("memory not read: %v", m)
	}
}

func TestQuotaAndEvents(t *testing.T) {
	h := newHub()
	srv := httptest.NewServer(newServer(h, ""))
	defer srv.Close()

	res := get(t, srv.URL+"/v1/events?topics=quota", "")
	defer res.Body.Close()
	rd := bufio.NewReader(res.Body)
	next := func() Event {
		for {
			line, err := rd.ReadString('\n')
			if err != nil {
				t.Fatal(err)
			}
			if strings.HasPrefix(line, "data: ") {
				var e Event
				json.Unmarshal([]byte(line[6:]), &e)
				return e
			}
		}
	}
	if e := next(); e.Topic != "quota" || string(e.Data) != "[]" {
		t.Fatalf("snapshot: %+v", e)
	}

	h.publish("workers", []struct{}{}) // unchanged: must not emit
	body := strings.NewReader(`{"state":"exhausted","resetAt":"2026-01-01T00:00:00Z"}`)
	pr, err := http.Post(srv.URL+"/v1/quota/codex-bot", "application/json", body)
	if err != nil || pr.StatusCode != 204 {
		t.Fatalf("post: %v %v", err, pr)
	}
	done := make(chan Event)
	go func() { done <- next() }()
	select {
	case e := <-done:
		if e.Topic != "quota" || !strings.Contains(string(e.Data), `"codex-bot"`) {
			t.Fatalf("change event: %+v", e)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no change event")
	}

	var qs []Quota
	json.NewDecoder(get(t, srv.URL+"/v1/quota?ns=t", "").Body).Decode(&qs)
	if len(qs) != 1 || qs[0].State != "exhausted" || qs[0].LastHitAt == nil || qs[0].ResetAt == nil || qs[0].Source != "api" {
		t.Fatalf("quota: %+v", qs)
	}
	if pr, _ := http.Post(srv.URL+"/v1/quota/x", "application/json", strings.NewReader(`{"state":"bogus"}`)); pr.StatusCode != 400 {
		t.Errorf("bad state: %d", pr.StatusCode)
	}
}

func TestCapacityChangeKey(t *testing.T) {
	h := newHub()
	c := Capacity{Host: "h", MemTotalMB: 16384, MemFreeMB: 3100, CPUs: 8, Load1: 2.1, Slots: 1}
	h.publish("capacity", c)
	rev := h.rev
	c.MemFreeMB, c.Load1, c.CollectedAt = 3150, 2.2, time.Now() // jitter within buckets
	h.publish("capacity", c)
	if h.rev != rev {
		t.Fatalf("jitter published an event")
	}
	if !strings.Contains(string(h.cur["capacity"].Data), `"memFreeMB":3150`) {
		t.Fatalf("GET value stale: %s", h.cur["capacity"].Data)
	}
	c.Slots = 2
	h.publish("capacity", c)
	if h.rev != rev+1 {
		t.Fatalf("slots change did not publish")
	}
	if !strings.Contains(string(h.cur["capacity"].Data), `"memFreeMB":3150`) {
		t.Errorf("served payload not exact: %s", h.cur["capacity"].Data)
	}
}

func TestDefaultPolicyWithoutFile(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("CAD_POLICY", "")
	p := currentPolicy()
	if p.Source != "builtin" || p.Gate.MemoryMB != 1500 || p.Gate.ReserveMB != 2048 {
		t.Fatalf("policy: %+v", p)
	}
	t.Setenv("CAD_SLOTS", "")
	if n := slots(2048+3000, 8, p.Gate); n != 2 {
		t.Fatalf("slots=%d want 2", n)
	}
}

func TestSnapshotRevOrder(t *testing.T) {
	h := newHub() // publishes workers then quota
	h.publish("capacity", Capacity{Host: "h"})
	s := h.current()
	for i := 1; i < len(s); i++ {
		if s[i].Rev < s[i-1].Rev {
			t.Fatalf("snapshot not rev-ordered: %v", s)
		}
	}
}

func TestApplyCgroup(t *testing.T) {
	dir := t.TempDir()
	w := func(name, s string) { os.WriteFile(filepath.Join(dir, name), []byte(s+"\n"), 0o644) }
	if a, b, c := applyCgroup(dir, 16000, 8000, 8); a != 16000 || b != 8000 || c != 8 {
		t.Fatalf("no files: %d %d %d", a, b, c)
	}
	w("memory.max", "max")
	w("cpu.max", "max 100000")
	if a, b, c := applyCgroup(dir, 16000, 8000, 8); a != 16000 || b != 8000 || c != 8 {
		t.Fatalf("max: %d %d %d", a, b, c)
	}
	w("memory.max", strconv.Itoa(4096<<20))
	w("memory.current", strconv.Itoa(1024<<20))
	w("cpu.max", "150000 100000")
	if a, b, c := applyCgroup(dir, 16000, 8000, 8); a != 4096 || b != 3072 || c != 2 {
		t.Fatalf("limited: %d %d %d", a, b, c)
	}
	if _, b, _ := applyCgroup(dir, 16000, 2000, 8); b != 2000 {
		t.Fatalf("free should be min with host: %d", b)
	}
}

func TestFutureLastEventIDGetsSnapshot(t *testing.T) {
	h := newHub()
	_, first := h.subscribe(strconv.FormatUint(h.rev+1000, 10))
	if len(first) != len(h.cur) {
		t.Fatalf("want snapshot of %d topics, got %v", len(h.cur), first)
	}
	_, first = h.subscribe(strconv.FormatUint(h.rev-1, 10))
	if len(first) != 1 || first[0].Rev != h.rev {
		t.Fatalf("want replay of last event, got %v", first)
	}
}

func TestNegativeCADSlots(t *testing.T) {
	t.Setenv("CAD_SLOTS", "-1")
	if n := slots(100000, 8, Gate{MemoryMB: 1500, CPUs: 1}); n != 0 {
		t.Fatalf("slots=%d want 0", n)
	}
}
