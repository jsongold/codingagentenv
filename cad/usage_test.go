package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fixture .claude.json: the usage key plus account data that must never be extracted.
const claudeJSON = `{"oauthAccount":{"emailAddress":"secret@example.com","accountUuid":"u-1"},"userID":"SECRET",
"cachedUsageUtilization":{"fetchedAtMs":%d,"accountUuid":"u-1","utilization":{
"five_hour":{"utilization":12.5,"resets_at":"2026-09-27T14:29:59.784780+00:00","used_dollars":null},
"seven_day":{"utilization":11,"resets_at":"2026-10-01T21:59:59.784812+00:00"},"seven_day_opus":null}}}`

func TestCollectUsage(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	write := func(p, body string) {
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte(body), 0o600)
	}
	now := time.Now().UnixMilli()
	write(filepath.Join(home, ".claude.json"), strings.Replace(claudeJSON, "%d", "0", 1)) // old: stale
	write(filepath.Join(home, ".aienv/.store/aa/.claude.json"), strings.Replace(claudeJSON, "%d", itoa64(now), 1))
	write(filepath.Join(home, ".aienv/.store/bb/.claude.json"), `{"userID":"SECRET"}`) // /usage never ran
	usageRunner = func(_ context.Context, dir string) error {
		if dir != "" && dir != filepath.Join(home, ".aienv/.store", filepath.Base(dir)) {
			t.Errorf("config dir %q", dir)
		}
		if strings.HasSuffix(dir, "/cc") {
			return errors.New("claude -p /usage: exit status 1")
		}
		return nil
	}
	defer func() { usageRunner = runClaudeUsage }()
	codexRunner = func(context.Context, string) ([]codexWin, error) { return nil, errors.New("no codex") }
	defer func() { codexRunner = runCodexUsage }()

	u := collectUsage([]string{"claude/default", "claude/aa", "claude/bb", "claude/cc", "codex/x"}, time.Minute)
	if len(u) != 5 {
		t.Fatalf("want 4 claude + 1 codex agents, got %v", u)
	}
	a := u["claude/aa"]
	if a.Error != "" || a.Stale || a.FiveHour.UsedPct != 12.5 || a.SevenDay.UsedPct != 11 || a.FiveHour.ResetsAt.Hour() != 14 || a.FetchedAt.UnixMilli() != now {
		t.Errorf("aa: %+v", a)
	}
	if d := u["claude/default"]; !d.Stale || d.Error != "" {
		t.Errorf("default should be stale: %+v", d)
	}
	if b := u["claude/bb"]; b.Error != "cachedUsageUtilization missing" {
		t.Errorf("bb: %+v", b)
	}
	if c := u["claude/cc"]; !strings.Contains(c.Error, "exit status 1") {
		t.Errorf("cc: %+v", c)
	}
	b, _ := json.Marshal(u)
	if strings.Contains(string(b), "SECRET") || strings.Contains(string(b), "secret@") || strings.Contains(string(b), "u-1") || strings.Contains(string(b), home) {
		t.Errorf("account data or path leaked: %s", b)
	}

	// fetchedAt alone does not publish an event.
	h := newHub()
	h.publish("usage", u)
	rev := h.rev
	a.FetchedAt = a.FetchedAt.Add(time.Minute)
	u["claude/aa"] = a
	if h.publish("usage", u); h.rev != rev {
		t.Error("fetchedAt change published an event")
	}
}

func itoa64(n int64) string { b, _ := json.Marshal(n); return string(b) }

func TestUsageEvery(t *testing.T) {
	p := filepath.Join(t.TempDir(), "p.json")
	t.Setenv("CAD_POLICY", p)
	t.Setenv("CAD_USAGE_EVERY", "")
	set := func(every string) {
		os.WriteFile(p, []byte(`{"collect":{"usage":{"every":"`+every+`"}}}`), 0o644)
		os.Chtimes(p, time.Now(), time.Now().Add(time.Duration(len(every))*time.Second)) // new mtime
	}
	set("5s")
	if d := usageEvery(); d != 5*time.Second {
		t.Fatalf("policy: %v", d)
	}
	set("bogus")
	if d := usageEvery(); d != 5*time.Second {
		t.Fatalf("invalid should keep last good: %v", d)
	}
	t.Setenv("CAD_USAGE_EVERY", "2s")
	if d := usageEvery(); d != 2*time.Second {
		t.Fatalf("env override: %v", d)
	}
}
