package main

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func init() { cadRetryEvery = 10 * time.Millisecond }

func TestPlaceDefersWhenCadNotReady(t *testing.T) {
	t.Setenv("ORCHD_STATE_DIR", t.TempDir())
	t.Setenv("ORCHD_MODE", "")
	t.Setenv("ORCHD_POLICY", "policy.json")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" && r.URL.Query().Has("ready") {
			http.Error(w, `{"ready":false}`, http.StatusServiceUnavailable)
			return
		}
		t.Errorf("asked %s before cad was ready", r.URL)
	}))
	defer srv.Close()
	t.Setenv("CAD_ADDR", strings.TrimPrefix(srv.URL, "http://"))
	out, code := runCmd("place", "--class", "light-edit", "--self", "claude/a12e00a7")
	if code != 3 || !strings.Contains(out, "cad not ready") || !strings.Contains(out, "defer_until") {
		t.Fatalf("%d %s", code, out)
	}
}

// Connection refused while cad restarts: retried, and placed once cad is back.
func TestCadReadyRetriesRefused(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close() // refused until the server below comes up
	var hits atomic.Int32
	go func() {
		time.Sleep(15 * time.Millisecond) // after the first attempt, before the retries run out
		ln, err := net.Listen("tcp", addr)
		if err != nil {
			t.Error(err)
			return
		}
		srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1) })}
		t.Cleanup(func() { srv.Close() })
		srv.Serve(ln)
	}()
	if ok, err := cadReady(addr); !ok || err != nil || hits.Load() != 1 {
		t.Fatalf("ready=%v err=%v hits=%d", ok, err, hits.Load())
	}
	start := time.Now()
	if _, err := cadReady("127.0.0.1:1"); err == nil || !strings.Contains(err.Error(), "cad unreachable") || time.Since(start) < 3*cadRetryEvery {
		t.Fatalf("gave up too early or without error: %v after %v", err, time.Since(start))
	}
}

func TestPlaceStaleUsage(t *testing.T) {
	pol, s := seedPolicy(t), PlaceSpec{Class: "light-edit", Self: "claude/a12e00a7"}
	u := seedUsage()
	for a, v := range u {
		v.Stale = true
		u[a] = v
	}
	got, st, _ := place(pol, usableUsage(u, time.Now()), s, 5)
	if st != 200 || got.Agent != "claude/a12e00a7" || !strings.Contains(strings.Join(got.Reason, ","), "claude/a12e00a7: usage stale") {
		t.Fatalf("pass: %d %+v", st, got)
	}
	pol.Placement.StaleUsage = "block"
	got, st, _ = place(pol, usableUsage(u, time.Now()), s, 5)
	if st == 200 || !strings.Contains(strings.Join(got.Reason, ","), "claude/a12e00a7: usage stale") {
		t.Fatalf("block: %d %+v", st, got)
	}
}
