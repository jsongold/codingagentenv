package main

import (
	"os"
	"path/filepath"
	"testing"
)

// lookup: ORCHD_POLICY / ORCHD_STATE_DIR > ORCHD_HOME > dir above the executable's bin/ (with policy.json) > CWD.
func TestAppDirLookup(t *testing.T) {
	root := t.TempDir()
	app, env := filepath.Join(root, "app"), filepath.Join(root, "env")
	os.MkdirAll(filepath.Join(app, "bin"), 0o755)
	os.WriteFile(filepath.Join(app, "bin", "orchd"), nil, 0o755)
	os.Symlink(filepath.Join(app, "bin", "orchd"), filepath.Join(root, "link")) // like ~/.local/bin/orchd
	t.Cleanup(func() { executable = os.Executable })
	executable = func() (string, error) { return filepath.Join(root, "link"), nil }
	for _, e := range []string{"ORCHD_POLICY", "ORCHD_STATE_DIR", "ORCHD_HOME"} {
		t.Setenv(e, "")
	}
	check := func(wantPol, wantState string) {
		t.Helper()
		if p, s := policyFile(), stateDir(); p != wantPol || s != wantState {
			t.Errorf("got %s %s, want %s %s", p, s, wantPol, wantState)
		}
	}
	check("policy.json", "state") // exe dir has no policy.json -> CWD
	os.WriteFile(filepath.Join(app, "policy.json"), []byte(`{}`), 0o644)
	realApp, _ := filepath.EvalSymlinks(app)
	check(filepath.Join(realApp, "policy.json"), filepath.Join(realApp, "state"))
	t.Setenv("ORCHD_HOME", env)
	check(filepath.Join(env, "policy.json"), filepath.Join(env, "state"))
	t.Setenv("ORCHD_POLICY", "/p.json")
	t.Setenv("ORCHD_STATE_DIR", "/s")
	check("/p.json", "/s")
}

// CAD_ADDR > mode cadAddr (auto: top-level) > 127.0.0.1:7878; the seed sends urgent local, auto to the VM tunnel.
func TestCadAddr(t *testing.T) {
	t.Setenv("CAD_ADDR", "")
	pol := seedPolicy(t)
	for mode, want := range map[string]string{"auto": "127.0.0.1:17878", "urgent": "127.0.0.1:7878"} {
		if got := cadAddr(pol, mode); got != want {
			t.Errorf("%s: %s want %s", mode, got, want)
		}
	}
	if got := cadAddr(Policy{Modes: map[string]Mode{"x": {}}}, "x"); got != "127.0.0.1:7878" {
		t.Errorf("unset: %s", got)
	}
	t.Setenv("CAD_ADDR", "10.0.0.1:1")
	if got := cadAddr(pol, "urgent"); got != "10.0.0.1:1" {
		t.Errorf("env: %s", got)
	}
}

func TestClaudeConfigDirDefaultIsEmpty(t *testing.T) {
	if got := claudeConfigDir("claude/default"); got != "" {
		t.Errorf("claude/default must not get a CLAUDE_CONFIG_DIR: %q", got)
	}
}
