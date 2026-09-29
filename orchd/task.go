package main

// dispatch: runs the runner place chose. orchd shells out to whatever runner.cmd names; shellIn is a
// package var so tests replace it.

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// shellIn runs name with stdin (nil = none) and returns stdout; the error carries stderr. dispatch uses it.
var shellIn = func(stdin io.Reader, name string, args ...string) (string, error) {
	c := exec.Command(name, args...)
	var errb bytes.Buffer
	c.Stdin, c.Stderr = stdin, &errb
	out, err := c.Output()
	if err != nil {
		return "", fmt.Errorf("%s: %v: %s", name, err, strings.TrimSpace(errb.String()))
	}
	return string(out), nil
}

// NS is one entry of the namespace registry (cad/config/namespaces.json, shared with cad; orchd only reads it).
type NS struct {
	Repo               string `json:"repo"`
	Path               string `json:"path"`
	CloudWorkerSession string `json:"cloudWorkerSession"`
}

// namespacesFile: ORCHD_NAMESPACES > <app>/../cad/config/namespaces.json > its .example (seed).
func namespacesFile() string {
	if f := os.Getenv("ORCHD_NAMESPACES"); f != "" {
		return f
	}
	f := filepath.Join(appDir(), "..", "cad", "config", "namespaces.json")
	if _, err := os.Stat(f); err != nil {
		return strings.TrimSuffix(f, ".json") + ".example.json"
	}
	return f
}

// readNS: the registry entry only (no git/gh lookups).
func readNS(name string) (NS, error) {
	var ns NS
	if b, err := os.ReadFile(namespacesFile()); err == nil {
		var reg map[string]NS
		if err := json.Unmarshal(b, &reg); err != nil {
			return ns, fmt.Errorf("%s: %v", namespacesFile(), err)
		}
		ns = reg[name]
	}
	return ns, nil
}

// dispatchCmd runs the placement's runner (the JSON place printed; "-" = stdin): each {key} in runner.cmd
// becomes the runner record's same-named string field, runner.stdin (a path) is piped in, and the command's
// stdout is printed. No branching on runner.mode: what differs between destinations lives in the record.
func dispatchCmd(args []string, stdin io.Reader, w io.Writer) (int, error) {
	fs := flag.NewFlagSet("dispatch", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	placement := fs.String("placement", "", "")
	if err := fs.Parse(args); err != nil || fs.NArg() > 0 || *placement == "" {
		return 2, fmt.Errorf("dispatch: bad args %q", args)
	}
	raw := []byte(*placement)
	if *placement == "-" {
		var err error
		if raw, err = io.ReadAll(stdin); err != nil {
			return 1, err
		}
	}
	var pl struct {
		Runner json.RawMessage `json:"runner"`
	}
	var rn Runner
	var rec map[string]any
	err := json.Unmarshal(raw, &pl)
	if err == nil {
		err = errors.Join(json.Unmarshal(pl.Runner, &rn), json.Unmarshal(pl.Runner, &rec))
	}
	if err != nil {
		return 2, fmt.Errorf("--placement: %v (want the JSON orchd place printed)", err)
	}
	if len(rn.Cmd) == 0 {
		return 2, errors.New("--placement: runner has no cmd")
	}
	argv := make([]string, len(rn.Cmd))
	for i := range rn.Cmd {
		argv[i] = cmdKey.ReplaceAllStringFunc(rn.Cmd[i], func(k string) string {
			v, ok := rec[k[1:len(k)-1]].(string)
			if !ok || v == "" {
				err = errors.Join(err, fmt.Errorf("runner.cmd: %s has no value in the runner record", k))
			}
			return v
		})
	}
	if err != nil {
		return 2, err
	}
	var in io.Reader
	if rn.Stdin != "" {
		f, err := os.Open(rn.Stdin)
		if err != nil {
			return 1, err
		}
		defer f.Close()
		in = f
	}
	out, err := shellIn(in, argv[0], argv[1:]...)
	if err != nil {
		return 1, err
	}
	_, err = io.WriteString(w, out)
	return 0, err
}

var cmdKey = regexp.MustCompile(`\{[A-Za-z0-9_]+\}`)

// cloudSession: env CLAUDE_CLOUD_SESSION > the namespace registry's cloudWorkerSession.
func cloudSession(ns NS) string {
	if s := os.Getenv("CLAUDE_CLOUD_SESSION"); s != "" {
		return s
	}
	return ns.CloudWorkerSession
}
