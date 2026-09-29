package main

import (
	"fmt"
	"maps"
	"os"
	"slices"
)

// resolveMode: flag > env ORCHD_MODE > "auto" (source "default": nothing overrides it).
func resolveMode(flagMode string) (mode, source string) {
	if flagMode != "" {
		return flagMode, "flag"
	}
	if m := os.Getenv("ORCHD_MODE"); m != "" {
		return m, "env"
	}
	return "auto", "default"
}

// modeRules: "auto" = top-level rules (local last); any other name must be in policy.modes.
func modeRules(pol Policy, mode string) ([]Rule, error) {
	if mode == "auto" {
		return pol.Rules, nil
	}
	if m, ok := pol.Modes[mode]; ok {
		return m.Rules, nil
	}
	return nil, fmt.Errorf("mode %q: want auto or one of %v", mode, slices.Sorted(maps.Keys(pol.Modes)))
}

// cadAddr: env CAD_ADDR > the mode's cadAddr (auto: top-level cadAddr) > 127.0.0.1:7878.
// urgent runs locally and trusts the local cad; auto uses the always-on VM cad (IAP tunnel).
func cadAddr(pol Policy, mode string) string {
	a := os.Getenv("CAD_ADDR")
	switch {
	case a != "":
	case mode == "auto":
		a = pol.CadAddr
	default:
		a = pol.Modes[mode].CadAddr
	}
	if a == "" {
		a = "127.0.0.1:7878"
	}
	return a
}
