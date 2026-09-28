package main

import (
	"os"
	"path/filepath"
	"testing"
)

// config lookup: CAD_CONFIG > CAD_POLICY > CAD_HOME > dir above the executable's bin/ (with config.json) > CWD.
func TestPolicyFileLookup(t *testing.T) {
	root := t.TempDir()
	app, env := filepath.Join(root, "app"), filepath.Join(root, "env")
	os.MkdirAll(filepath.Join(app, "bin"), 0o755)
	os.Symlink(filepath.Join(app, "bin", "cad"), filepath.Join(root, "link")) // like ~/.local/bin/cad
	t.Cleanup(func() { executable = os.Executable })
	executable = func() (string, error) { return filepath.Join(root, "link"), nil }
	for _, e := range []string{"CAD_CONFIG", "CAD_POLICY", "CAD_HOME"} {
		t.Setenv(e, "")
	}
	os.WriteFile(filepath.Join(app, "bin", "cad"), nil, 0o755)
	check := func(want string) {
		t.Helper()
		if got := policyFile(); got != want {
			t.Errorf("got %s, want %s", got, want)
		}
	}
	check("config.json") // exe dir has no config.json -> CWD
	os.WriteFile(filepath.Join(app, "config.json"), []byte(`{}`), 0o644)
	realApp, _ := filepath.EvalSymlinks(app)
	check(filepath.Join(realApp, "config.json"))
	t.Setenv("CAD_HOME", env)
	check(filepath.Join(env, "config.json"))
	t.Setenv("CAD_POLICY", "/old.json")
	check("/old.json")
	t.Setenv("CAD_CONFIG", "/new.json")
	check("/new.json")
}
