package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"time"
)

// ModeState is <state>/mode/<ns>.json (or _global.json). It outlives the Orchestrator's context
// (compaction, restarts): the owner says "MODE=URGENT", the Orchestrator runs `orchd mode set urgent`.
type ModeState struct {
	Mode  string    `json:"mode"`
	Since time.Time `json:"since"`
	By    string    `json:"by,omitempty"`
}

// modeFile: ns "" = the global file. ns never starts with "_" (nsRe), so it cannot collide.
func modeFile(ns string) string {
	if ns == "" {
		ns = "_global"
	}
	return filepath.Join(stateDir(), "mode", ns+".json")
}

// readMode returns "" when the file does not exist.
func readMode(ns string) (string, error) {
	b, err := os.ReadFile(modeFile(ns))
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	var m ModeState
	if err == nil {
		err = json.Unmarshal(b, &m)
	}
	if err != nil {
		return "", fmt.Errorf("mode file %s: %v", modeFile(ns), err)
	}
	return m.Mode, nil
}

// writeMode writes atomically: a reader sees the old file or the new one, never a partial one.
func writeMode(ns string, m ModeState) error {
	f := modeFile(ns)
	if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(m, "", "  ")
	tmp, err := os.CreateTemp(filepath.Dir(f), ".mode-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // no-op after a successful rename
	if _, err = tmp.Write(append(b, '\n')); err == nil {
		err = tmp.Close()
	} else {
		tmp.Close()
	}
	if err == nil {
		err = os.Rename(tmp.Name(), f)
	}
	return err
}

// resolveMode: flag > ns file > global file > env ORCHD_MODE > "auto" (source "default": nothing overrides it).
func resolveMode(flagMode, ns string) (mode, source string, err error) {
	if flagMode != "" {
		return flagMode, "flag", nil
	}
	if ns != "" {
		if m, err := readMode(ns); err != nil || m != "" {
			return m, "file:ns", err
		}
	}
	if m, err := readMode(""); err != nil || m != "" {
		return m, "file:global", err
	}
	if m := os.Getenv("ORCHD_MODE"); m != "" {
		return m, "env", nil
	}
	return "auto", "default", nil
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

func modeCmd(args []string, w io.Writer) (int, error) {
	if len(args) == 0 {
		return 2, errors.New("mode: want set, clear or show")
	}
	sub, args := args[0], args[1:]
	var name string
	if sub == "set" {
		if len(args) == 0 {
			return 2, errors.New("mode set: want a mode name")
		}
		name, args = args[0], args[1:]
	}
	fs := flag.NewFlagSet("mode", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	ns, by := fs.String("ns", "", ""), fs.String("by", "", "")
	if err := fs.Parse(args); err != nil || fs.NArg() > 0 {
		return 2, fmt.Errorf("mode %s: bad args %q", sub, args)
	}
	if *ns != "" && !nsRe.MatchString(*ns) {
		return 2, fmt.Errorf("--ns %q: want %s", *ns, nsRe)
	}
	switch sub {
	case "set":
		pol, err := loadPolicy()
		if err != nil {
			return 1, err
		}
		if _, err := modeRules(pol, name); err != nil {
			return 2, err
		}
		m := ModeState{Mode: name, Since: time.Now().UTC().Truncate(time.Second), By: *by}
		if err := writeMode(*ns, m); err != nil {
			return 1, err
		}
		return 0, printJSON(w, map[string]interface{}{"mode": m.Mode, "since": m.Since, "file": modeFile(*ns)})
	case "clear":
		if err := os.Remove(modeFile(*ns)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return 1, err
		}
		return 0, printJSON(w, map[string]string{"cleared": modeFile(*ns)})
	case "show":
		mode, src, err := resolveMode("", *ns)
		if err != nil {
			return 1, err
		}
		return 0, printJSON(w, map[string]string{"mode": mode, "modeSource": src})
	}
	return 2, fmt.Errorf("mode %q: want set, clear or show", sub)
}
