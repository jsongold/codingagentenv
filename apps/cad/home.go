package main

import (
	"os"
	"path/filepath"
)

var executable = os.Executable // tests fake it

// appDir is cad's own directory, independent of the CWD: $CAD_HOME > the dir above the bin/ holding
// the real executable (tools/cad runs apps/cad/bin/cad) if it has config.json > "." (go run / go test).
func appDir() string {
	if d := os.Getenv("CAD_HOME"); d != "" {
		return d
	}
	if exe, err := executable(); err == nil {
		if exe, err = filepath.EvalSymlinks(exe); err == nil && filepath.Base(filepath.Dir(exe)) == "bin" {
			d := filepath.Dir(filepath.Dir(exe))
			if _, err := os.Stat(filepath.Join(d, "config.json")); err == nil {
				return d
			}
		}
	}
	return "."
}

// policyFile: CAD_CONFIG > CAD_POLICY (old name, alias) > <appDir>/config.json.
func policyFile() string {
	for _, e := range []string{"CAD_CONFIG", "CAD_POLICY"} {
		if p := os.Getenv(e); p != "" {
			return p
		}
	}
	return filepath.Join(appDir(), "config.json")
}
