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
	b, err := os.ReadFile("policy.json")
	if err != nil {
		t.Fatal(err)
	}
	var p Policy
	if err := json.Unmarshal(b, &p); err != nil {
		t.Fatal(err)
	}
	// The place() logic tests below were written against the local-only rule list; the seeded
	// auto/urgent lists are covered in mode_test.go.
	p.Rules = []Rule{{Agent: "self", Computer: "local"}, {Agent: "opencode/*", Computer: "local"}}
	return p
}

var (
	r5 = time.Date(2026, 9, 27, 15, 0, 0, 0, time.UTC)
	r7 = time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC)
)

func win(pct5, pct7 float64) AgentUsage {
	return AgentUsage{FiveHour: &UsageWindow{UsedPct: pct5, ResetsAt: r5}, SevenDay: &UsageWindow{UsedPct: pct7, ResetsAt: r7}}
}

// seedUsage: every agent fits (reservePct 15).
func seedUsage() map[string]AgentUsage {
	return map[string]AgentUsage{"claude/a12e00a7": win(30, 40), "claude/b1c8ef41": win(10, 50), "codex/2e33b72a": win(20, 30), "opencode/996c87ae": win(0, 0)}
}

func TestPlace(t *testing.T) {
	pol, s := seedPolicy(t), PlaceSpec{Class: "light-edit", Self: "claude/a12e00a7"}
	check := func(name string, u map[string]AgentUsage, slots, wantSt int, wantAgent string, wantRule int) Placement {
		t.Helper()
		got, st, _ := place(pol, u, s, slots)
		if st != wantSt || got.Agent != wantAgent || got.Rule != wantRule {
			t.Errorf("%s: %d %+v, want %d %s rule %d", name, st, got, wantSt, wantAgent, wantRule)
		}
		return got
	}
	if got := check("claude ok", seedUsage(), 5, 200, "claude/a12e00a7", 0); got.Computer != "local" || got.Runner == nil || got.Runner.Mode != "subagent" {
		t.Errorf("computer %q runner %+v", got.Computer, got.Runner)
	}

	u := seedUsage()
	u["claude/a12e00a7"], u["claude/b1c8ef41"] = win(90, 0), win(90, 0)
	got := check("self over 5h", u, 5, 200, "opencode/996c87ae", 1)
	if !reflect.DeepEqual(got.Reason, []string{"claude/a12e00a7: 5h window"}) {
		t.Errorf("reasons %v", got.Reason)
	}
	if got.Runner == nil || got.Runner.Mode != "process" || got.Runner.Model != "opencode-go/deepseek-v4-pro" {
		t.Errorf("runner %+v", got.Runner)
	}

	// self missing or not in policy.agents -> the self rule is skipped.
	for _, self := range []string{"", "claude/zzzz"} {
		s.Self = self
		if got := check("self "+self, seedUsage(), 5, 200, "opencode/996c87ae", 1); got.Reason[0] != "rule 0: self unknown" {
			t.Errorf("reasons %v", got.Reason)
		}
	}
	s.Self = "claude/b1c8ef41" // any registered agent can be self
	check("self b1c8", seedUsage(), 5, 200, "claude/b1c8ef41", 0)
	s.Self = "claude/a12e00a7"

	// All over -> 409, defer_until = earliest time any blocked agent fits again.
	early := time.Date(2026, 9, 27, 13, 0, 0, 0, time.UTC)
	u["claude/a12e00a7"] = AgentUsage{FiveHour: &UsageWindow{UsedPct: 90, ResetsAt: early}} // null 7d window is skipped
	u["opencode/996c87ae"] = win(90, 90)                                                    // frees only at r7
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

// fakeCad serves /v1/usage and /v1/capacity like cad does (token + ns required); usage nil = 404.
func fakeCad(t *testing.T, usage map[string]AgentUsage, slots int) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" { // ready, like cad after its first collection
			return
		}
		if r.Header.Get("Authorization") != "Bearer tok" || r.URL.Query().Get("ns") != "default" {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		switch r.URL.Path {
		case "/v1/usage":
			if usage == nil {
				http.NotFound(w, r)
				return
			}
			json.NewEncoder(w).Encode(usage)
		case "/v1/capacity":
			json.NewEncoder(w).Encode(map[string]int{"slots": slots})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	t.Setenv("CAD_ADDR", strings.TrimPrefix(srv.URL, "http://"))
	t.Setenv("CAD_TOKEN", "tok")
}

func runCmd(args ...string) (string, int) {
	var out, errb strings.Builder
	code := run(args, &out, &errb)
	return out.String() + errb.String(), code
}

func TestPlaceCmd(t *testing.T) {
	t.Setenv("ORCHD_STATE_DIR", t.TempDir())
	t.Setenv("ORCHD_MODE", "")
	t.Setenv("ORCHD_POLICY", "policy.json")
	fakeCad(t, seedUsage(), 2)
	out, code := runCmd("place", "--class", "light-edit", "--self", "claude/a12e00a7")
	var p Placement
	json.Unmarshal([]byte(out), &p)
	if code != 0 || p.Computer != "claude-cloud" || p.Agent != "claude/a12e00a7" || p.Rule != 0 || p.Runner.Mode != "cloud" { // auto mode
		t.Fatalf("%d %s", code, out)
	}
	for _, c := range []struct {
		args []string
		want int
	}{
		{[]string{"place", "--class", "nope"}, 2},
		{[]string{"place"}, 2},
		{[]string{"place", "--class", "light-edit", "--self", "Claude"}, 2},
		{[]string{"place", "--class", "light-edit", "--ns", "Bad!"}, 2},
		{[]string{"place", "--class", "light-edit", "--ns", "other"}, 1}, // cad answers 400
		{[]string{"show", "nope"}, 2},
		{[]string{"frob"}, 2},
		{[]string{"show", "rules"}, 0},
	} {
		if out, code := runCmd(c.args...); code != c.want {
			t.Errorf("%v: %d want %d: %s", c.args, code, c.want, out)
		}
	}

	u := seedUsage()
	soon := &UsageWindow{UsedPct: 90, ResetsAt: time.Now().Add(time.Hour)} // the fixed seed resets are in the past
	for a := range u {
		u[a] = AgentUsage{FiveHour: soon}
	}
	fakeCad(t, u, 2)
	if out, code := runCmd("place", "--class", "light-edit", "--self", "claude/a12e00a7"); code != 3 || !strings.Contains(out, `"defer_until"`) {
		t.Errorf("defer: %d %s", code, out)
	}
	fakeCad(t, seedUsage(), 0)
	if out, code := runCmd("place", "--class", "light-edit"); code != 4 || !strings.Contains(out, "no local slot") {
		t.Errorf("no slot: %d %s", code, out)
	}
	fakeCad(t, nil, 2) // usage not collected yet: agents are "usage unknown" and still placed
	if out, code := runCmd("place", "--class", "light-edit", "--self", "claude/a12e00a7"); code != 0 || !strings.Contains(out, "usage unknown") {
		t.Errorf("no usage: %d %s", code, out)
	}
	t.Setenv("CAD_ADDR", "127.0.0.1:1")
	if out, code := runCmd("place", "--class", "light-edit"); code != 1 || !strings.Contains(out, "cad unreachable") {
		t.Errorf("unreachable: %d %s", code, out)
	}
}

func TestUsableUsage(t *testing.T) {
	now := time.Now()
	past := &UsageWindow{UsedPct: 95, ResetsAt: now.Add(-time.Minute)}
	u := map[string]AgentUsage{
		"claude/b1c8ef41": {FiveHour: past, SevenDay: past},
		"claude/default":  {Error: "claude -p /usage: exit status 1"},
	}
	got := usableUsage(u, now)
	if b := got["claude/b1c8ef41"]; b.FiveHour.UsedPct != 0 || b.SevenDay.UsedPct != 0 {
		t.Fatalf("past reset not 0%%: %+v", b)
	}
	if past.UsedPct != 95 {
		t.Fatal("usableUsage mutated the input window")
	}
	if _, ok := got["claude/default"]; ok {
		t.Fatal("error agent kept as known")
	}
}

// A class added to the policy file is accepted without a code change; a policy without classes
// or with a bad runner is refused.
func TestPolicyFile(t *testing.T) {
	t.Setenv("ORCHD_STATE_DIR", t.TempDir())
	pol := seedPolicy(t)
	pol.Classes["docs-only"] = Class{Criteria: "x", EstPct: 1}
	b, _ := json.Marshal(pol)
	f := filepath.Join(t.TempDir(), "p.json")
	os.WriteFile(f, b, 0o644)
	t.Setenv("ORCHD_POLICY", f)
	fakeCad(t, seedUsage(), 1)
	if out, code := runCmd("place", "--class", "docs-only", "--self", "claude/a12e00a7"); code != 0 || !strings.Contains(out, `"subagent"`) {
		t.Fatalf("%d %s", code, out)
	}
	for body, want := range map[string]string{
		`{"agents":[]}`: `no "classes"`,
		`{"classes":{"x":{}},"runners":{"opencode":{"mode":"process"}}}`: "process needs cmd",
		`{"classes":{"x":{}},"placement":{"staleUsage":"maybe"}}`:        "want pass or block",
	} {
		os.WriteFile(f, []byte(body), 0o644)
		if out, code := runCmd("show"); code != 1 || !strings.Contains(out, want) {
			t.Errorf("%s: %d %s", body, code, out)
		}
	}
}

// A candidate without a runner, or a subagent-backed one other than self, is skipped.
func TestPlaceRunnerChecks(t *testing.T) {
	pol := seedPolicy(t)
	pol.Rules = append([]Rule{{Agent: "codex/*", Computer: "local"}, {Agent: "claude/*", Computer: "local"}}, pol.Rules...)
	got, st, _ := place(pol, seedUsage(), PlaceSpec{Class: "light-edit", Self: "claude/b1c8ef41"}, 5)
	want := []string{"codex/2e33b72a: no runner", "claude/a12e00a7: subagent runs only as self"}
	if st != 200 || got.Agent != "claude/b1c8ef41" || got.Rule != 1 || !reflect.DeepEqual(got.Reason, want) {
		t.Fatalf("%d %+v", st, got)
	}
	got, st, _ = place(pol, seedUsage(), PlaceSpec{Class: "light-edit"}, 5) // no self: claude/* matches nothing usable
	if st != 200 || got.Agent != "opencode/996c87ae" || got.Rule != 3 {
		t.Fatalf("%d %+v", st, got)
	}
}

// opencode's monthly window and a rate-limited window block like 5h/7d; other services are unchanged.
func TestPlaceMonthlyAndRateLimited(t *testing.T) {
	pol, s := seedPolicy(t), PlaceSpec{Class: "light-edit", Self: "claude/a12e00a7"}
	for name, oc := range map[string]AgentUsage{
		"monthly window": {FiveHour: &UsageWindow{ResetsAt: r5}, Monthly: &UsageWindow{UsedPct: 90, ResetsAt: r7}},
		"7d window":      {SevenDay: &UsageWindow{UsedPct: 1, ResetsAt: r7, RateLimited: true}},
	} {
		u := seedUsage()
		u["claude/a12e00a7"], u["claude/b1c8ef41"] = win(90, 0), win(90, 0)
		u["opencode/996c87ae"] = oc
		got, _, _ := place(pol, u, s, 5)
		if got.Agent == "opencode/996c87ae" || !strings.Contains(strings.Join(got.Reason, ","), "opencode/996c87ae: "+name) {
			t.Errorf("%s: %+v", name, got)
		}
	}
	u := seedUsage()
	u["claude/a12e00a7"], u["claude/b1c8ef41"] = win(90, 0), win(90, 0)
	u["opencode/996c87ae"] = AgentUsage{Monthly: &UsageWindow{UsedPct: 80, ResetsAt: r7}} // 80+est fits
	if got, st, _ := place(pol, u, s, 5); st != 200 || got.Agent != "opencode/996c87ae" {
		t.Errorf("monthly fits: %d %+v", st, got)
	}
}
