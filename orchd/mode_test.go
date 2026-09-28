package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func modeEnv(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	t.Setenv("ORCHD_STATE_DIR", d)
	t.Setenv("ORCHD_MODE", "")
	t.Setenv("ORCHD_POLICY", "policy.json")
	return d
}

// Each source is added from lowest to highest; the newest one must win.
func TestResolveModePrecedence(t *testing.T) {
	modeEnv(t)
	check := func(flag, want, wantSrc string) {
		t.Helper()
		if m, src, err := resolveMode(flag, "default"); err != nil || m != want || src != wantSrc {
			t.Errorf("got %s %s %v, want %s %s", m, src, err, want, wantSrc)
		}
	}
	check("", "auto", "default")
	t.Setenv("ORCHD_MODE", "e")
	check("", "e", "env")
	writeMode("", ModeState{Mode: "g"})
	check("", "g", "file:global")
	writeMode("default", ModeState{Mode: "n"})
	check("", "n", "file:ns")
	check("f", "f", "flag")
	if m, src, _ := resolveMode("", "other"); m != "g" || src != "file:global" { // another ns sees only the global file
		t.Errorf("other ns: %s %s", m, src)
	}
}

func TestModeCmdRoundTrip(t *testing.T) {
	d := modeEnv(t)
	for _, c := range []struct {
		args []string
		code int
		want string
	}{
		{[]string{"mode", "show", "--ns", "default"}, 0, `"modeSource": "default"`},
		{[]string{"mode", "set", "urgent", "--ns", "default", "--by", "owner"}, 0, `"urgent"`},
		{[]string{"mode", "show", "--ns", "default"}, 0, `"modeSource": "file:ns"`},
		{[]string{"mode", "set", "auto"}, 0, `"auto"`},
		{[]string{"mode", "show"}, 0, `"modeSource": "file:global"`},
		{[]string{"mode", "clear", "--ns", "default"}, 0, "cleared"},
		{[]string{"mode", "clear", "--ns", "default"}, 0, "cleared"}, // idempotent
		{[]string{"mode", "clear"}, 0, "cleared"},
		{[]string{"mode", "show", "--ns", "default"}, 0, `"mode": "auto"`},
		{[]string{"mode", "set", "nope"}, 2, "nope"},
		{[]string{"mode", "set"}, 2, ""},
		{[]string{"mode", "frob"}, 2, ""},
		{[]string{"mode", "show", "--ns", "Bad!"}, 2, ""},
	} {
		if out, code := runCmd(c.args...); code != c.code || !strings.Contains(out, c.want) {
			t.Errorf("%v: %d %s", c.args, code, out)
		}
	}
	// set leaves exactly one valid JSON file and no temp files behind.
	runCmd("mode", "set", "urgent", "--ns", "default", "--by", "owner")
	ents, _ := os.ReadDir(filepath.Join(d, "mode"))
	if len(ents) != 1 || ents[0].Name() != "default.json" {
		t.Fatalf("mode dir: %v", ents)
	}
	var m ModeState
	b, _ := os.ReadFile(filepath.Join(d, "mode", "default.json"))
	if err := json.Unmarshal(b, &m); err != nil || m.Mode != "urgent" || m.By != "owner" || m.Since.IsZero() {
		t.Fatalf("%v %+v %s", err, m, b)
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
	runCmd("mode", "set", "urgent", "--ns", "default")
	if p, code := place(); code != 0 || p.Mode != "urgent" || p.ModeSource != "file:ns" || p.Computer != "local" || p.Rule != 0 || p.Runner.Mode != "subagent" {
		t.Errorf("urgent: %d %+v", code, p)
	}
	if p, _ := place("--mode", "auto"); p.ModeSource != "flag" || p.Computer != "claude-cloud" {
		t.Errorf("flag: %+v", p)
	}
	if _, code := place("--mode", "nope"); code != 2 {
		t.Errorf("unknown flag mode: %d", code)
	}
	runCmd("mode", "clear", "--ns", "default")
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
	pol := Policy{Runners: map[string]Runner{"claude": {Mode: "subagent"}, "claude@claude-cloud": {Mode: "cloud", Cmd: "c"}}}
	for computer, want := range map[string]string{"claude-cloud": "cloud", "local": "subagent", "gce-spot": "subagent"} {
		if rn, ok := pol.runner("claude", computer); !ok || rn.Mode != want {
			t.Errorf("%s: %+v %v", computer, rn, ok)
		}
	}
	if _, ok := pol.runner("codex", "local"); ok {
		t.Error("codex has no runner")
	}
}
