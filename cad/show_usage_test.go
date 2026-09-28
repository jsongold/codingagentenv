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
		"claude/b1c8ef41": {
			// Window not started: resets_at is null in .claude.json -> zero time.Time. Must
			// print "-", not a formatted zero-value date (bug in PR #30 review).
			FiveHour:  &UsageWindow{UsedPct: 0, ResetsAt: time.Time{}},
			SevenDay:  &UsageWindow{UsedPct: 11, ResetsAt: now.Add(72 * time.Hour)},
			FetchedAt: now.Add(-30 * time.Second),
		},
		"codex/2e33b72a": {
			FiveHour:  nil, // null window -> "-"
			SevenDay:  &UsageWindow{UsedPct: 38, ResetsAt: now.Add(48 * time.Hour)},
			FetchedAt: now.Add(-12 * time.Second),
		},
	}
	var buf bytes.Buffer
	// opencode/996c87ae is in the policy but has no usage entry: "usage unknown".
	if err := printUsageTable(&buf, []string{"claude/a12e00a7", "claude/b1c8ef41", "codex/2e33b72a", "opencode/996c87ae"}, m, now); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 5 {
		t.Fatalf("want header + 4 rows, got %d:\n%s", len(lines), out)
	}
	if !strings.HasPrefix(lines[0], "AGENT") || !strings.Contains(lines[0], "NOTE") {
		t.Fatalf("header: %q", lines[0])
	}
	// Row order is sorted by agent.
	if !strings.HasPrefix(lines[1], "claude/a12e00a7") || !strings.HasPrefix(lines[2], "claude/b1c8ef41") ||
		!strings.HasPrefix(lines[3], "codex/2e33b72a") || !strings.HasPrefix(lines[4], "opencode/996c87ae") {
		t.Fatalf("sort order:\n%s", out)
	}
	if !strings.Contains(lines[1], "16%") || !strings.Contains(lines[1], "2%") || !strings.Contains(lines[1], "12s ago") {
		t.Fatalf("claude row: %q", lines[1])
	}
	if !strings.Contains(lines[1], "in 2h03m") {
		t.Fatalf("relative reset missing: %q", lines[1])
	}
	// Zero (unset) reset time must print "-", not a formatted zero-value date.
	if fields := strings.Fields(lines[2]); fields[2] != "-" {
		t.Fatalf("zero resets_at: want -, got %q (%q)", fields[2], lines[2])
	}
	if got := strings.Fields(lines[3])[1]; got != "-" {
		t.Fatalf("codex null 5h window: want -, got %q (%q)", got, lines[3])
	}
	if !strings.Contains(lines[4], "usage unknown") {
		t.Fatalf("unknown agent note: %q", lines[4])
	}
	// opencode row is all dashes besides the note.
	fields := strings.Fields(lines[4])
	for _, f := range fields[1:6] {
		if f != "-" {
			t.Fatalf("unknown agent row should be dashes: %q", lines[4])
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
