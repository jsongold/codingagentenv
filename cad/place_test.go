package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func seedPolicy(t *testing.T) Policy {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", ".agent", "policy.json"))
	if err != nil {
		t.Fatal(err)
	}
	var p Policy
	if err := json.Unmarshal(b, &p); err != nil {
		t.Fatal(err)
	}
	return p
}

var (
	r5 = time.Date(2026, 9, 27, 15, 0, 0, 0, time.UTC)
	r7 = time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC)
)

func win(pct5, pct7 float64) AgentUsage {
	return AgentUsage{FiveHour: &UsageWindow{pct5, r5}, SevenDay: &UsageWindow{pct7, r7}}
}

// seedUsage: every agent fits (reservePct 15).
func seedUsage() map[string]AgentUsage {
	return map[string]AgentUsage{"claude/a12e00a7": win(30, 40), "claude/b1c8ef41": win(10, 50), "codex/2e33b72a": win(20, 30), "opencode/996c87ae": win(0, 0)}
}

func TestPlace(t *testing.T) {
	pol, s := seedPolicy(t), PlaceSpec{Class: "light-edit"}
	check := func(name string, u map[string]AgentUsage, slots, wantSt int, wantAgent string, wantRule int) Placement {
		t.Helper()
		got, st, _ := place(pol, u, s, slots)
		if st != wantSt || got.Agent != wantAgent || got.Rule != wantRule {
			t.Errorf("%s: %d %+v, want %d %s rule %d", name, st, got, wantSt, wantAgent, wantRule)
		}
		return got
	}
	if got := check("claude ok", seedUsage(), 5, 200, "claude/a12e00a7", 0); got.Computer != "local" {
		t.Errorf("computer %q", got.Computer)
	}

	u := seedUsage()
	u["claude/a12e00a7"], u["claude/b1c8ef41"] = win(90, 0), win(90, 0)
	got := check("claude over 5h", u, 5, 200, "opencode/996c87ae", 1)
	if !reflect.DeepEqual(got.Reason, []string{"claude/a12e00a7: 5h window", "claude/b1c8ef41: 5h window"}) {
		t.Errorf("reasons %v", got.Reason)
	}

	// All over -> 409, defer_until = earliest time any blocked agent fits again.
	early := time.Date(2026, 9, 27, 13, 0, 0, 0, time.UTC)
	u["claude/a12e00a7"] = AgentUsage{FiveHour: &UsageWindow{90, early}} // null 7d window is skipped
	u["opencode/996c87ae"] = win(90, 90)                                  // frees only at r7
	if _, st, until := place(pol, u, s, 5); st != http.StatusConflict || !until.Equal(early) {
		t.Errorf("409: %d %v", st, until)
	}

	delete(u, "claude/a12e00a7")
	check("usage unknown -> ok", u, 5, 200, "claude/a12e00a7", 0)

	pol.Rules[0].Class = []string{"needs-db"}
	got = check("class-restricted rule skipped", seedUsage(), 5, 200, "opencode/996c87ae", 1)
	if got.Reason[0] != "rule 0: class" {
		t.Errorf("reasons %v", got.Reason)
	}

	check("no local slot -> 422", seedUsage(), 0, 422, "", 0)

	first, _, _ := place(pol, seedUsage(), s, 5)
	for i := 0; i < 20; i++ {
		if again, _, _ := place(pol, seedUsage(), s, 5); !reflect.DeepEqual(first, again) {
			t.Fatalf("run %d: %+v != %+v", i, again, first)
		}
	}
}

func TestPostPlace(t *testing.T) {
	t.Setenv("CAD_POLICY", filepath.Join("..", ".agent", "policy.json"))
	h := newHub()
	h.publish("capacity", Capacity{Slots: 2})
	h.publish("usage", UsageMap(seedUsage()))
	srv := httptest.NewServer(newServer(h, "tok"))
	defer srv.Close()
	post := func(q, body, token string) *http.Response {
		req, _ := http.NewRequest("POST", srv.URL+"/v1/place"+q, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	ok := `{"class":"light-edit","placement":{"strategy":"ignored"}}`
	for _, c := range []struct {
		q, body, token string
		want           int
	}{
		{"?ns=a", ok, "tok", 200},
		{"", ok, "tok", 400},
		{"?ns=a", ok, "bad", 401},
		{"?ns=a", `{"class":"nope"}`, "tok", 400},
		{"?ns=a", `{}`, "tok", 400},
	} {
		res := post(c.q, c.body, c.token)
		if res.StatusCode != c.want {
			t.Errorf("%s %s: %d want %d", c.q, c.body, res.StatusCode, c.want)
		}
		if c.want == 200 {
			var p Placement
			json.NewDecoder(res.Body).Decode(&p)
			if p.Computer != "local" || p.Agent != "claude/a12e00a7" || p.Rule != 0 {
				t.Errorf("200 body: %+v", p)
			}
		}
		res.Body.Close()
	}
}

func TestPlaceHubUsage(t *testing.T) {
	now := time.Now()
	past := &UsageWindow{95, now.Add(-time.Minute)}
	u := UsageMap{
		"claude/b1c8ef41": {FiveHour: past, SevenDay: past},
		"claude/default":  {Error: "claude -p /usage: exit status 1"},
	}
	got := usableUsage(u, now)
	if b := got["claude/b1c8ef41"]; b.FiveHour.UsedPct != 0 || b.SevenDay.UsedPct != 0 {
		t.Fatalf("past reset not 0%%: %+v", b)
	}
	if past.UsedPct != 95 {
		t.Fatal("usableUsage mutated the published window")
	}
	if _, ok := got["claude/default"]; ok {
		t.Fatal("error agent kept as known")
	}
}
