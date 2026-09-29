package main

import (
	"os"
	"path/filepath"
	"strings"
)

var executable = os.Executable // tests fake it

// appDir is orchd's own directory, independent of the CWD: $ORCHD_HOME > the dir above the bin/ holding
// the real executable (tools/orchd runs orchd/bin/orchd) if it has policy.json > "." (go run / go test).
func appDir() string {
	if d := os.Getenv("ORCHD_HOME"); d != "" {
		return d
	}
	if exe, err := executable(); err == nil {
		if exe, err = filepath.EvalSymlinks(exe); err == nil && filepath.Base(filepath.Dir(exe)) == "bin" {
			d := filepath.Dir(filepath.Dir(exe))
			if _, err := os.Stat(filepath.Join(d, "policy.json")); err == nil {
				return d
			}
		}
	}
	return "."
}

// policyFile: ORCHD_POLICY > <appDir>/policy.json.
func policyFile() string {
	if p := os.Getenv("ORCHD_POLICY"); p != "" {
		return p
	}
	return filepath.Join(appDir(), "policy.json")
}

// claudeConfigDir: CLAUDE_CONFIG_DIR for a claude/<id> agent, mirroring cad's usage collector
// (cad/usage.go usageStore, agent claude/<id> -> config dir $HOME/.aienv/.store/<id>; "default" ->
// the plain ~/.claude.json, i.e. no CLAUDE_CONFIG_DIR). No dir expresses "default", so it returns ""
// and dispatch refuses the {configDir} template instead of pointing claude at the wrong place.
func claudeConfigDir(agent string) string {
	home, _ := os.UserHomeDir()
	if id := strings.TrimPrefix(agent, "claude/"); id != "default" {
		return filepath.Join(home, ".aienv", ".store", id)
	}
	return ""
}

// stateDir: ORCHD_STATE_DIR > <appDir>/state (gitignored).
func stateDir() string {
	if d := os.Getenv("ORCHD_STATE_DIR"); d != "" {
		return d
	}
	return filepath.Join(appDir(), "state")
}
