package main

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const ocSample = `{"usage":{"rolling":{"status":"ok","percent":3,"resetsAt":"2026-09-28T06:20:30.808Z"},` +
	`"weekly":{"status":"%s","percent":1,"resetsAt":"2026-10-05T00:00:00.000Z"},` +
	`"monthly":{"status":"ok","percent":9,"resetsAt":"2026-10-03T18:13:39.000Z"}}}`

func TestOpencodeUsage(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "xdg"))
	write := func(p, body string) {
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte(body), 0o600)
	}
	write(filepath.Join(home, ".aienv/.store/aa/opencode/auth.json"),
		`{"opencode":{"type":"api","key":"OC-SECRET"},"opencode-go":{"type":"api","key":"GO-SECRET"},"anthropic":{"key":"OTHER-SECRET"}}`)
	write(filepath.Join(home, ".aienv/.store/bb/opencode/auth.json"), `{"opencode":{"type":"api","key":"BB-SECRET"}}`)
	write(filepath.Join(home, "xdg/opencode/auth.json"), `{"opencode":{"type":"api","key":"DEF-SECRET"}}`)
	write(filepath.Join(home, ".aienv/.store/nokey/opencode/auth.json"), `{"anthropic":{"key":"OTHER-SECRET"}}`)

	var logs bytes.Buffer
	log.SetOutput(&logs)
	defer log.SetOutput(os.Stderr)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Header.Get("Authorization") {
		case "Bearer GO-SECRET":
			w.Write([]byte(strings.Replace(ocSample, "%s", "ok", 1)))
		case "Bearer BB-SECRET":
			w.Write([]byte(strings.Replace(ocSample, "%s", "rate-limited", 1)))
		case "Bearer DEF-SECRET":
			http.Error(w, "nope", http.StatusUnauthorized)
		default:
			t.Errorf("unexpected auth %q", r.Header.Get("Authorization"))
		}
	}))
	defer srv.Close()
	opencodeUsageURL = srv.URL
	defer func() { opencodeUsageURL = "https://opencode.ai/zen/go/v1/usage" }()

	u := collectUsage([]string{"opencode/aa", "opencode/bb", "opencode/default", "opencode/nokey", "opencode/missing"}, time.Minute)
	a := u["opencode/aa"]
	if a.Error != "" || a.FiveHour.UsedPct != 3 || a.SevenDay.UsedPct != 1 || a.Monthly.UsedPct != 9 ||
		a.Monthly.ResetsAt.Day() != 3 || a.SevenDay.RateLimited || a.FetchedAt.IsZero() {
		t.Errorf("aa: %+v", a)
	}
	if b := u["opencode/bb"]; b.Error != "" || !b.SevenDay.RateLimited || b.FiveHour.RateLimited {
		t.Errorf("bb rate-limited: %+v", b)
	}
	if d := u["opencode/default"]; d.Error != "http 401" || d.FiveHour != nil {
		t.Errorf("default: %+v", d)
	}
	if n := u["opencode/nokey"]; n.Error != "no opencode key in auth.json" {
		t.Errorf("nokey: %+v", n)
	}
	if m := u["opencode/missing"]; m.Error == "" || strings.Contains(m.Error, home) {
		t.Errorf("missing: %+v", m)
	}
	out, _ := json.Marshal(u)
	for _, s := range []string{"SECRET", home} {
		if strings.Contains(string(out), s) || strings.Contains(logs.String(), s) {
			t.Errorf("%q leaked: %s %s", s, out, logs.String())
		}
	}
	if !strings.Contains(string(out), `"monthly":{"usedPct":9`) {
		t.Errorf("json: %s", out)
	}
}
