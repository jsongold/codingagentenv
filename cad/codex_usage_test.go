package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeAppServer answers like `codex app-server` and records the methods it received in order.
func fakeAppServer(t *testing.T, got *[]string, silent bool) (io.Writer, io.Reader) {
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	go func() {
		defer outW.Close()
		sc := bufio.NewScanner(inR)
		for sc.Scan() {
			var m struct {
				ID     *int   `json:"id"`
				Method string `json:"method"`
			}
			json.Unmarshal(sc.Bytes(), &m)
			*got = append(*got, m.Method)
			if silent {
				continue
			}
			switch m.Method {
			case "initialize":
				io.WriteString(outW, `{"method":"configWarning","params":{}}`+"\n")
				io.WriteString(outW, `{"id":1,"result":{"userAgent":"x"}}`+"\n")
			case "account/rateLimits/read":
				io.WriteString(outW, `{"id":7,"result":{}}`+"\n")
				io.WriteString(outW, `{"id":2,"result":{"rateLimits":{"limitId":"codex","planType":"plus","accountId":"SECRET",`+
					`"primary":{"usedPercent":32,"windowDurationMins":10080,"resetsAt":1791090986},"secondary":null}}}`+"\n")
			}
		}
	}()
	t.Cleanup(func() { inR.Close(); outR.Close() })
	return inW, outR
}

func TestCodexRateLimits(t *testing.T) {
	var got []string
	w, r := fakeAppServer(t, &got, false)
	wins, err := codexRateLimits(context.Background(), w, r)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, ",") != "initialize,initialized,account/rateLimits/read" {
		t.Errorf("handshake order: %v", got)
	}
	u := codexWindows(wins)
	if u.SevenDay.UsedPct != 32 || u.SevenDay.ResetsAt.Unix() != 1791090986 || u.FiveHour != nil {
		t.Errorf("windows: %+v", u)
	}
	if b, _ := json.Marshal(u); strings.Contains(string(b), "SECRET") || !strings.Contains(string(b), `"fiveHour":null`) {
		t.Errorf("leaked or 5h not null: %s", b)
	}

	var got2 []string
	w, r = fakeAppServer(t, &got2, true)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := codexRateLimits(ctx, w, r); err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Errorf("want timeout, got %v", err)
	}
}

func TestCodexUsageFor(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	store := filepath.Join(home, ".aienv/.store")
	sess := filepath.Join(store, "fb/sessions/2026/09/27")
	os.MkdirAll(sess, 0o755)
	os.WriteFile(filepath.Join(sess, "rollout-2026-09-27T01-a.jsonl"), []byte(
		`{"type":"session_meta","payload":{"id":"SECRET"}}`+"\n"+
			`{"type":"event_msg","payload":{"type":"token_count","rate_limits":{"primary":{"used_percent":10,"window_minutes":10080,"resets_at":1791090986},"secondary":null}}}`+"\n"+
			`{"type":"event_msg","payload":{"type":"token_count","rate_limits":{"primary":{"used_percent":32,"window_minutes":10080,"resets_at":1791090986},"secondary":{"used_percent":5,"window_minutes":300,"resets_at":1791000000}}}}`+"\n"+
			`{"type":"event_msg","payload":{"type":"agent_message"}}`+"\n"), 0o600)
	codexRunner = func(_ context.Context, h string) ([]codexWin, error) {
		switch h {
		case filepath.Join(store, "ok"):
			return []codexWin{{UsedPercent: 32, WindowDurationMins: 10080, ResetsAt: 1791090986}}, nil
		default:
			return nil, errors.New("codex app-server: exited before answering")
		}
	}
	defer func() { codexRunner = runCodexUsage }()

	u := collectUsage([]string{"codex/ok", "codex/fb", "codex/none", "opencode/x"}, time.Minute)
	if len(u) != 3 {
		t.Fatalf("want 3 codex agents, got %v", u)
	}
	if o := u["codex/ok"]; o.Error != "" || o.Stale || o.SevenDay.UsedPct != 32 || o.FetchedAt.IsZero() {
		t.Errorf("ok: %+v", o)
	}
	if f := u["codex/fb"]; f.Error != "" || !f.Stale || f.SevenDay.UsedPct != 32 || f.FiveHour.UsedPct != 5 {
		t.Errorf("fallback: %+v", f)
	}
	if n := u["codex/none"]; !strings.Contains(n.Error, "exited") {
		t.Errorf("none: %+v", n)
	}
}

// TestCodexByLimitID: every snapshot of rateLimitsByLimitId is scanned (primary and secondary),
// windows go by length, and the highest usedPercent wins per window.
func TestCodexByLimitID(t *testing.T) {
	win := func(res string) AgentUsage {
		t.Helper()
		wins, err := codexExchange(io.Discard, strings.NewReader(`{"id":1,"result":{}}`+"\n"+`{"id":2,"result":`+res+"}\n"))
		if err != nil {
			t.Fatal(err)
		}
		return codexWindows(wins)
	}
	team := `{"primary":{"usedPercent":1,"windowDurationMins":300,"resetsAt":100},"secondary":{"usedPercent":34,"windowDurationMins":10080,"resetsAt":200}}`
	other := `{"primary":{"usedPercent":60,"windowDurationMins":10080,"resetsAt":300},"secondary":null}`
	u := win(`{"rateLimits":` + team + `,"rateLimitsByLimitId":{"codex":` + team + `,"codex_other":` + other + `}}`)
	if u.FiveHour == nil || u.FiveHour.UsedPct != 1 || u.SevenDay == nil || u.SevenDay.UsedPct != 60 || u.SevenDay.ResetsAt.Unix() != 300 {
		t.Errorf("two limits: 5h %+v 7d %+v", u.FiveHour, u.SevenDay)
	}
	u = win(`{"rateLimits":` + team + `,"rateLimitsByLimitId":{"codex":` + team + `}}`) // team plan: 7d in secondary
	if u.FiveHour == nil || u.FiveHour.UsedPct != 1 || u.SevenDay == nil || u.SevenDay.UsedPct != 34 {
		t.Errorf("team: 5h %+v 7d %+v", u.FiveHour, u.SevenDay)
	}
	u = win(`{"rateLimits":` + other + `,"rateLimitsByLimitId":{}}`) // plus plan, empty map: falls back to rateLimits
	if u.FiveHour != nil || u.SevenDay == nil || u.SevenDay.UsedPct != 60 {
		t.Errorf("plus: 5h %+v 7d %+v", u.FiveHour, u.SevenDay)
	}
}
