package main

import (
	"encoding/json"
	"os"
	"testing"
)

func modeEnv(t *testing.T) {
	t.Helper()
	t.Setenv("ORCHD_MODE", "")
	t.Setenv("ORCHD_POLICY", "policy.json")
}

// flag > env ORCHD_MODE > "auto".
func TestResolveModePrecedence(t *testing.T) {
	modeEnv(t)
	if m, src := resolveMode(""); m != "auto" || src != "default" {
		t.Errorf("got %s %s, want auto default", m, src)
	}
	t.Setenv("ORCHD_MODE", "e")
	if m, src := resolveMode(""); m != "e" || src != "env" {
		t.Errorf("got %s %s, want e env", m, src)
	}
	if m, src := resolveMode("f"); m != "f" || src != "flag" { // flag beats env
		t.Errorf("got %s %s, want f flag", m, src)
	}
}

type placeOut struct {
	Placement
	Mode       string `json:"mode"`
	ModeSource string `json:"modeSource"`
	CadAddr    string `json:"cadAddr"`
}

func TestPlaceWithMode(t *testing.T) {
	modeEnv(t)
	fakeCad(t, seedUsage(), 2)
	place := func(args ...string) (placeOut, int) {
		t.Helper()
		out, code := runCmd(append([]string{"place", "--class", "gate-heavy", "--self", "claude/a12e00a7"}, args...)...)
		var p placeOut
		json.Unmarshal([]byte(out), &p)
		return p, code
	}
	// auto (nothing set): Claude cloud first, runner looked up as claude@claude-cloud.
	if p, code := place(); code != 0 || p.Mode != "auto" || p.ModeSource != "default" || p.CadAddr != os.Getenv("CAD_ADDR") || p.Computer != "claude-cloud" || p.Runner.Mode != "cloud" {
		t.Errorf("auto: %d %+v", code, p)
	}
	if p, code := place("--mode", "urgent"); code != 0 || p.Mode != "urgent" || p.ModeSource != "flag" || p.Computer != "local" || p.Rule != 0 || p.Runner.Mode != "subagent" {
		t.Errorf("urgent: %d %+v", code, p)
	}
	if p, _ := place("--mode", "auto"); p.ModeSource != "flag" || p.Computer != "claude-cloud" {
		t.Errorf("flag: %+v", p)
	}
	if _, code := place("--mode", "nope"); code != 2 {
		t.Errorf("unknown flag mode: %d", code)
	}
	t.Setenv("ORCHD_MODE", "urgent")
	if p, _ := place(); p.ModeSource != "env" || p.Computer != "local" {
		t.Errorf("env: %+v", p)
	}
	t.Setenv("ORCHD_MODE", "nope")
	if _, code := place(); code != 2 {
		t.Errorf("unknown env mode: %d", code)
	}
}

// Urgent falls back to the auto rules when local cannot take the task.
func TestUrgentFallsBack(t *testing.T) {
	pol := seedPolicy(t)
	pol.Rules = pol.Modes["urgent"].Rules
	got, st, _ := place(pol, seedUsage(), PlaceSpec{Class: "gate-heavy", Self: "claude/a12e00a7"}, 0)
	if st != 200 || got.Computer != "claude-cloud" || got.Rule != 2 {
		t.Fatalf("%d %+v", st, got)
	}
}

func TestRunnerLookup(t *testing.T) {
	pol := Policy{Runners: map[string]Runner{"claude": {Mode: "subagent"}, "claude@claude-cloud": {Mode: "cloud", Cmd: []string{"c"}}}}
	for computer, want := range map[string]string{"claude-cloud": "cloud", "local": "subagent", "gce-spot": "subagent"} {
		if rn, ok := pol.runner("claude", computer); !ok || rn.Mode != want {
			t.Errorf("%s: %+v %v", computer, rn, ok)
		}
	}
	if _, ok := pol.runner("codex", "local"); ok {
		t.Error("codex has no runner")
	}
}
