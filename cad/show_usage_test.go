package main

import (
	"bytes"
	"encoding/json"
	"net"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestPrintUsageTable(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	m := UsageMap{
		"claude/a12e00a7": {
			FiveHour:  &UsageWindow{UsedPct: 16, ResetsAt: now.Add(2*time.Hour + 3*time.Minute)},
			SevenDay:  &UsageWindow{UsedPct: 2, ResetsAt: now.Add(-30 * 24 * time.Hour)}, // far away -> date form
			FetchedAt: now.Add(-12 * time.Second),
		},
		"codex/2e33b72a": {
			FiveHour:  nil, // null window -> "-"
			SevenDay:  &UsageWindow{UsedPct: 38, ResetsAt: now.Add(48 * time.Hour)},
			FetchedAt: now.Add(-12 * time.Second),
		},
	}
	var buf bytes.Buffer
	// opencode/996c87ae is in the policy but has no usage entry: "usage unknown".
	if err := printUsageTable(&buf, []string{"claude/a12e00a7", "codex/2e33b72a", "opencode/996c87ae"}, m, now); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 4 {
		t.Fatalf("want header + 3 rows, got %d:\n%s", len(lines), out)
	}
	if !strings.HasPrefix(lines[0], "AGENT") || !strings.Contains(lines[0], "NOTE") {
		t.Fatalf("header: %q", lines[0])
	}
	// Row order is sorted by agent.
	if !strings.HasPrefix(lines[1], "claude/a12e00a7") || !strings.HasPrefix(lines[2], "codex/2e33b72a") || !strings.HasPrefix(lines[3], "opencode/996c87ae") {
		t.Fatalf("sort order:\n%s", out)
	}
	if !strings.Contains(lines[1], "16%") || !strings.Contains(lines[1], "2%") || !strings.Contains(lines[1], "12s ago") {
		t.Fatalf("claude row: %q", lines[1])
	}
	if !strings.Contains(lines[1], "in 2h03m") {
		t.Fatalf("relative reset missing: %q", lines[1])
	}
	if got := strings.Fields(lines[2])[1]; got != "-" {
		t.Fatalf("codex null 5h window: want -, got %q (%q)", got, lines[2])
	}
	if !strings.Contains(lines[3], "usage unknown") {
		t.Fatalf("unknown agent note: %q", lines[3])
	}
	// opencode row is all dashes besides the note.
	fields := strings.Fields(lines[3])
	for _, f := range fields[1:6] {
		if f != "-" {
			t.Fatalf("unknown agent row should be dashes: %q", lines[3])
		}
	}
}

func TestShowUsageJSON(t *testing.T) {
	cliEnv(t)
	run(t, "", "add", "agent", "claude/test")
	t.Setenv("CAD_ADDR", closedAddr(t)) // nothing listens here -> forces the fallback
	orig := usageCollector
	defer func() { usageCollector = orig }()
	usageCollector = func(agents []string, every time.Duration) UsageMap {
		return UsageMap{"claude/test": {FiveHour: &UsageWindow{UsedPct: 7}}}
	}
	out, code := run(t, "", "show", "--usage", "--json")
	if code != 0 {
		t.Fatalf("code %d: %s", code, out)
	}
	var m UsageMap
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatalf("not valid json: %v\n%s", err, out)
	}
	if m["claude/test"].FiveHour == nil || m["claude/test"].FiveHour.UsedPct != 7 {
		t.Fatalf("json passthrough: %+v", m)
	}
	if !strings.Contains(out, "\n  ") { // printJSON indents
		t.Fatalf("expected indented json: %s", out)
	}
}

// closedAddr returns a loopback address nothing is listening on (connection refused), simulating
// cad not being up.
func closedAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()
	return addr
}

func TestShowUsageFallbackWhenDaemonUnreachable(t *testing.T) {
	cliEnv(t)
	run(t, "", "add", "agent", "claude/test")
	t.Setenv("CAD_ADDR", closedAddr(t))

	orig := usageCollector
	defer func() { usageCollector = orig }()
	called := false
	usageCollector = func(agents []string, every time.Duration) UsageMap {
		called = true
		return UsageMap{"claude/test": {FiveHour: &UsageWindow{UsedPct: 42}}}
	}
	out, code := run(t, "", "show", "--usage")
	if code != 0 {
		t.Fatalf("code %d: %s", code, out)
	}
	if !called {
		t.Fatal("expected fallback collector to run")
	}
	if !strings.Contains(out, "42%") {
		t.Fatalf("fallback data missing from table: %s", out)
	}
}

func TestShowUsageLocalForcesFallback(t *testing.T) {
	cliEnv(t)
	// Even a running daemon must be ignored with --local.
	h := newHub()
	h.publish("usage", UsageMap{"claude/remote": {FiveHour: &UsageWindow{UsedPct: 99}}})
	srv := httptest.NewServer(newServer(h, ""))
	defer srv.Close()
	t.Setenv("CAD_ADDR", strings.TrimPrefix(srv.URL, "http://"))

	orig := usageCollector
	defer func() { usageCollector = orig }()
	usageCollector = func(agents []string, every time.Duration) UsageMap {
		return UsageMap{"claude/local": {FiveHour: &UsageWindow{UsedPct: 1}}}
	}
	out, code := run(t, "", "show", "--usage", "--local", "--json")
	if code != 0 {
		t.Fatalf("code %d: %s", code, out)
	}
	if strings.Contains(out, "remote") || !strings.Contains(out, "local") {
		t.Fatalf("--local did not force in-process collection: %s", out)
	}
}

func TestShowUsageBadFlag(t *testing.T) {
	cliEnv(t)
	if out, code := run(t, "", "show", "--usage", "--bogus"); code != 2 {
		t.Fatalf("want exit 2, got %d: %s", code, out)
	}
}
