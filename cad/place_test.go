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

// seedUsage is fixed test usage (the old provisional .agent/usage.json).
func seedUsage(t *testing.T) map[string]AgentUsage {
	at := func(s string) time.Time { v, _ := time.Parse(time.RFC3339, s); return v }
	w := func(pct5, pct7 float64) AgentUsage {
		return AgentUsage{FiveHour: &UsageWindow{pct5, at("2026-09-27T15:00:00Z")}, SevenDay: &UsageWindow{pct7, at("2026-10-03T00:00:00Z")}}
	}
	return map[string]AgentUsage{"claude/a12e00a7": w(30, 40), "claude/b1c8ef41": w(90, 50), "claude/default": w(10, 20), "codex/2e33b72a": w(20, 30), "opencode/996c87ae": w(0, 0)}
}

func spec(class, strategy string, mem, min int, maxCost float64, allow ...string) PlaceSpec {
	var s PlaceSpec
	s.Class, s.Placement.Strategy, s.Placement.Allow, s.Placement.MaxCostUSD = class, strategy, allow, maxCost
	s.Resources.MemoryMB, s.Resources.CPUs, s.Resources.TimeoutMin = mem, 1, min
	return s
}

func TestPlace(t *testing.T) {
	pol, use := seedPolicy(t), seedUsage(t)
	explicit := spec("light-edit", "cheap", 1024, 30, 0)
	explicit.Placement.Provider = "e2b"
	explicit.Resources.TimeoutMin = 120 // e2b maxMin 60
	ranked := spec("light-edit", "ranked", 1024, 30, 0, "e2b", "cloud-run-jobs", "gce-spot")
	ranked.Placement.Order = []string{"cloud-run-jobs", "e2b"}
	cases := []struct {
		name  string
		s     PlaceSpec
		slots int
		want  string // computer, "" = 422
	}{
		{"local-first", spec("light-edit", "cheap", 1024, 30, 0), 5, "local"},
		{"local no slots -> cheapest free non-preemptible", spec("light-edit", "cheap", 1024, 30, 0), 0, "claude-cloud"},
		{"needs-db excludes cloud-run", spec("needs-db", "cheap", 1024, 30, 0, "cloud-run-jobs", "e2b"), 0, "e2b"},
		{"needs-db only cloud-run -> 422", spec("needs-db", "cheap", 1024, 30, 0, "cloud-run-jobs"), 0, ""},
		{"maxCostUSD filters e2b", spec("light-edit", "cheap", 1024, 30, 0.02, "e2b", "gce-spot"), 0, "gce-spot"},
		{"maxCostUSD filters all -> 422", spec("light-edit", "cheap", 1024, 30, 0.0001, "e2b", "gce-spot"), 0, ""},
		{"cheap picks preemptible when cheaper", spec("light-edit", "cheap", 1024, 30, 0, "e2b", "gce-spot"), 0, "gce-spot"},
		{"safe excludes preemptible", spec("light-edit", "safe", 1024, 30, 0, "e2b", "gce-spot"), 0, "e2b"},
		{"fast by coldStart", spec("light-edit", "fast", 1024, 30, 0, "gce-spot", "cloud-run-jobs", "e2b"), 0, "e2b"},
		{"ranked by order", ranked, 0, "cloud-run-jobs"},
		{"explicit provider infeasible -> 422", explicit, 5, ""},
		{"unknown class -> 422", spec("nope", "cheap", 1024, 30, 0), 5, ""},
	}
	for _, c := range cases {
		got, st, _ := place(pol, use, c.s, c.slots)
		ok := st == http.StatusOK
		if ok != (c.want != "") || got.Computer != c.want || !ok && st != http.StatusUnprocessableEntity {
			t.Errorf("%s: got %q status=%d reasons=%v, want %q", c.name, got.Computer, st, got.Reason, c.want)
		}
		// seed usage: a12e00a7 fits (5h 30%, 7d 40%) and is lexicographically first.
		if ok && got.Agent != "claude/a12e00a7" {
			t.Errorf("%s: agent %q", c.name, got.Agent)
		}
	}
}

func TestPlaceTieBreakAndDeterminism(t *testing.T) {
	pol := seedPolicy(t)
	pol.Agents = []string{"claude/b", "codex/x", "claude/a"}
	pol.Computers["twin"] = pol.Computers["e2b"] // same key as e2b; "e2b" < "twin"
	s := spec("light-edit", "safe", 1024, 30, 0, "twin", "e2b")
	first, st, _ := place(pol, nil, s, 0)
	if st != http.StatusOK || first.Agent != "claude/a" || first.Computer != "e2b" {
		t.Fatalf("got %+v", first)
	}
	for i := 0; i < 20; i++ {
		if again, _, _ := place(pol, nil, s, 0); !reflect.DeepEqual(first, again) {
			t.Fatalf("run %d: %+v != %+v", i, again, first)
		}
	}
	// Service priority beats the agent name; unlisted services go last.
	pol.AgentPriority = []string{"codex", "claude"}
	if got, _, _ := place(pol, nil, s, 0); got.Agent != "codex/x" {
		t.Fatalf("agentPriority: %+v", got)
	}
	pol.AgentPriority, pol.Agents = []string{"codex"}, []string{"claude/a", "aaa/z", "codex/x"}
	pol.ClassAgents["light-edit"] = []string{"*/*"}
	if got, _, _ := place(pol, nil, s, 0); got.Agent != "codex/x" {
		t.Fatalf("listed first: %+v", got)
	}
	pol.Agents = []string{"claude/a", "aaa/z"}
	if got, _, _ := place(pol, nil, s, 0); got.Agent != "aaa/z" {
		t.Fatalf("unlisted by name: %+v", got)
	}
}

func TestPostPlace(t *testing.T) {
	t.Setenv("CAD_POLICY", filepath.Join("..", ".agent", "policy.json"))
	h := newHub()
	h.publish("capacity", Capacity{Slots: 2})
	h.publish("usage", UsageMap(seedUsage(t)))
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
	ok := `{"class":"light-edit","resources":{"memoryMB":1024,"cpus":1,"timeoutMin":30},"placement":{"strategy":"cheap"}}`
	for _, c := range []struct {
		q, body, token string
		want           int
	}{
		{"?ns=a", ok, "tok", 200},
		{"", ok, "tok", 400},
		{"?ns=a", ok, "bad", 401},
		{"?ns=a", `{"class":"light-edit","placement":{"strategy":"weird"}}`, "tok", 400},
		{"?ns=a", `{"class":"needs-db","resources":{"memoryMB":1024,"cpus":1},"placement":{"allow":["cloud-run-jobs"]}}`, "tok", 422},
	} {
		res := post(c.q, c.body, c.token)
		if res.StatusCode != c.want {
			t.Errorf("%s %s: %d want %d", c.q, c.body, res.StatusCode, c.want)
		}
		if c.want == 200 {
			var p Placement
			json.NewDecoder(res.Body).Decode(&p)
			if p.Computer != "local" || p.Agent != "claude/a12e00a7" {
				t.Errorf("200 body: %+v", p)
			}
		}
		res.Body.Close()
	}
}

func TestPlaceWindow(t *testing.T) {
	pol, seed := seedPolicy(t), seedUsage(t)
	at := func(s string) time.Time { v, _ := time.Parse(time.RFC3339, s); return v }
	full := func(pct5, pct7 float64, r5, r7 string) AgentUsage {
		return AgentUsage{FiveHour: &UsageWindow{pct5, at(r5)}, SevenDay: &UsageWindow{pct7, at(r7)}}
	}
	has := func(rs []string, want string) bool {
		for _, r := range rs {
			if r == want {
				return true
			}
		}
		return false
	}
	s := spec("light-edit", "cheap", 1024, 30, 0)

	// seed: b1c8ef41 is at 5h 90% > 85 - 1 and is dropped; a12e00a7 wins.
	got, st, _ := place(pol, seed, s, 5)
	if st != 200 || got.Agent != "claude/a12e00a7" || !has(got.Reason, "claude/b1c8ef41: 5h window") {
		t.Fatalf("5h drop: %d %+v", st, got)
	}

	// Agents are sorted, so claude/* < codex/* < opencode/*: light-edit reaches codex only
	// once every claude agent is filtered, and opencode only once codex is too.
	u := map[string]AgentUsage{}
	for k, v := range seed {
		u[k] = v
	}
	for _, a := range []string{"claude/a12e00a7", "claude/b1c8ef41"} {
		u[a] = full(90, 0, "2026-09-27T15:00:00Z", "2026-10-03T00:00:00Z")
	}
	if got, _, _ = place(pol, u, s, 5); got.Agent != "codex/2e33b72a" {
		t.Errorf("claude filtered: %+v", got)
	}
	u["codex/2e33b72a"] = full(0, 85, "2026-09-27T15:00:00Z", "2026-10-03T00:00:00Z") // 7d 85+1 > 85
	if got, _, _ = place(pol, u, s, 5); got.Agent != "opencode/996c87ae" || !has(got.Reason, "codex/2e33b72a: 7d window") {
		t.Errorf("codex 7d filtered: %+v", got)
	}

	// A nil (null) window is not filtered and adds no reason: codex with no 5h window, 7d low.
	u["codex/2e33b72a"] = AgentUsage{SevenDay: &UsageWindow{10, at("2026-10-03T00:00:00Z")}}
	if got, _, _ = place(pol, u, s, 5); got.Agent != "codex/2e33b72a" || has(got.Reason, "codex/2e33b72a: 5h window") {
		t.Errorf("null window: %+v", got)
	}
	u["codex/2e33b72a"] = full(0, 85, "2026-09-27T15:00:00Z", "2026-10-03T00:00:00Z")

	// Unknown usage is kept (with a reason) and wins over nothing.
	delete(u, "opencode/996c87ae")
	if got, st, _ = place(pol, u, s, 5); st != 200 || got.Agent != "opencode/996c87ae" || !has(got.Reason, "opencode/996c87ae: usage unknown") {
		t.Errorf("unknown kept: %d %+v", st, got)
	}

	// needs-db is claude-only: all claude over -> 409, defer_until = earliest agent-free time.
	// b1c8ef41 exceeds both windows so it is free only at its later reset (10-03);
	// a12e00a7 frees at its own 5h reset (09-27T13), the earliest. A null window is skipped.
	u["claude/a12e00a7"] = AgentUsage{FiveHour: &UsageWindow{90, at("2026-09-27T13:00:00Z")}}
	u["claude/b1c8ef41"] = full(90, 90, "2026-09-27T12:00:00Z", "2026-10-03T00:00:00Z")
	_, st, until := place(pol, u, spec("needs-db", "cheap", 1024, 30, 0), 5)
	if st != http.StatusConflict || !until.Equal(at("2026-09-27T13:00:00Z")) {
		t.Errorf("409: %d %v", st, until)
	}
	// Same usage but no feasible computer: the window isn't the cause -> 422.
	if _, st, _ = place(pol, u, spec("needs-db", "cheap", 1024, 30, 0, "cloud-run-jobs"), 5); st != http.StatusUnprocessableEntity {
		t.Errorf("422: %d", st)
	}
}

func TestPlaceHubUsage(t *testing.T) {
	t.Setenv("CAD_POLICY", filepath.Join("..", ".agent", "policy.json"))
	now := time.Now()
	past, future := &UsageWindow{95, now.Add(-time.Minute)}, &UsageWindow{95, now.Add(time.Hour)}
	u := UsageMap{
		"claude/a12e00a7": {FiveHour: future, SevenDay: &UsageWindow{1, now.Add(time.Hour)}}, // 5h over
		"claude/b1c8ef41": {FiveHour: past, SevenDay: past},                                  // both reset: 0%
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
	h := newHub()
	h.publish("capacity", Capacity{Slots: 2})
	h.publish("usage", u)
	srv := httptest.NewServer(newServer(h, ""))
	defer srv.Close()
	res, err := http.Post(srv.URL+"/v1/place?ns=a", "application/json", strings.NewReader(`{"class":"needs-db","resources":{"memoryMB":1024,"cpus":1,"timeoutMin":30}}`))
	if err != nil {
		t.Fatal(err)
	}
	var p Placement
	json.NewDecoder(res.Body).Decode(&p)
	res.Body.Close()
	reasons := strings.Join(p.Reason, "|")
	if res.StatusCode != 200 || p.Agent != "claude/b1c8ef41" || !strings.Contains(reasons, "claude/a12e00a7: 5h window") {
		t.Fatalf("%d %+v", res.StatusCode, p)
	}
}
