package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const cliUsage = `usage:
  cad                                   run the server (env: CAD_ADDR, CAD_TOKEN, CAD_POLICY, CAD_USAGE, ...)
  cad show [agents|computers|classAgents|usage|policy]   print records (no arg = everything)
  cad add agent <service/account>
  cad add computer <name> [--replace] [--file f.json | -]  (JSON body; piped stdin also works)
  cad add classagent <class> <pattern>
  cad set usage <agent> [--5h pct] [--5h-reset RFC3339] [--7d pct] [--7d-reset RFC3339] [--file f.json | -]
  cad rm agent <agent> | computer <name> | classagent <class> [pattern] | usage <agent>
files: CAD_POLICY > .agent/policy.json, CAD_USAGE > .agent/usage.json. A running cad re-reads them on mtime change.
`

// knownClasses are the task classes of ADR-0010.
var knownClasses = []string{"light-edit", "gate-heavy", "needs-db", "long-running", "urgent", "retry"}

var agentRe = regexp.MustCompile(`^[a-z0-9-]+/[a-z0-9-]+$`)

// computerNumFields must be present (as numbers) in a computer body.
var computerNumFields = []string{"vcpuHourUSD", "gibHourUSD", "minBillSec", "coldStartSec", "maxMemMB", "maxCpus", "maxMin"}

// isCLI reports whether args[0] selects the record CLI instead of the server.
func isCLI(args []string) bool {
	if len(args) == 0 {
		return false
	}
	switch args[0] {
	case "show", "add", "set", "rm", "help", "-h", "-help", "--help":
		return true
	}
	return false
}

// runCLI executes one record command. stdin is nil when nothing is piped. Returns the exit code.
func runCLI(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if err := cli(args, stdin, stdout); err != nil {
		fmt.Fprintf(stderr, "cad: %v\n", err)
		if errors.Is(err, errUsage) {
			fmt.Fprint(stderr, cliUsage)
		}
		return 1
	}
	return 0
}

var errUsage = errors.New("bad usage")

func cli(args []string, stdin io.Reader, stdout io.Writer) error {
	kind := ""
	if len(args) > 1 {
		kind = strings.ToLower(args[1])
	}
	rest := args[min(len(args), 2):]
	switch args[0] {
	case "help", "-h", "-help", "--help":
		_, err := fmt.Fprint(stdout, cliUsage)
		return err
	case "show":
		return show(kind, stdout)
	case "add":
		return add(kind, rest, stdin)
	case "set":
		if kind != "usage" || len(rest) < 1 {
			return errUsage
		}
		return setUsage(rest[0], rest[1:], stdin)
	case "rm":
		return rm(kind, rest)
	}
	return errUsage
}

func policyFile() string {
	if p := os.Getenv("CAD_POLICY"); p != "" {
		return p
	}
	return ".agent/policy.json"
}

func usageFile() string {
	if p := os.Getenv("CAD_USAGE"); p != "" {
		return p
	}
	return ".agent/usage.json"
}

// loadPolicy reads the policy file; a missing file yields defaultPolicy (add creates it).
func loadPolicy() (Policy, error) {
	b, err := os.ReadFile(policyFile())
	if errors.Is(err, os.ErrNotExist) {
		return defaultPolicy(), nil
	}
	if err != nil {
		return Policy{}, err
	}
	var p Policy
	if err := json.Unmarshal(b, &p); err != nil {
		return Policy{}, fmt.Errorf("%s: %v", policyFile(), err)
	}
	p.Source = policyFile()
	return p, nil
}

// loadUsageRaw keeps every key (including "_provisional") so writes drop nothing.
func loadUsageRaw() (map[string]json.RawMessage, error) {
	raw := map[string]json.RawMessage{}
	b, err := os.ReadFile(usageFile())
	if errors.Is(err, os.ErrNotExist) {
		return raw, nil
	}
	if err != nil {
		return nil, err
	}
	if _, err := parseUsage(b); err != nil {
		return nil, fmt.Errorf("%s: %v", usageFile(), err)
	}
	return raw, json.Unmarshal(b, &raw)
}

func savePolicy(p Policy) error {
	b, err := json.Marshal(p)
	if err != nil {
		return err
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		return err
	}
	delete(m, "source") // runtime-only field
	b, err = json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	var check Policy
	if err := json.Unmarshal(b, &check); err != nil {
		return err
	}
	return writeAtomic(policyFile(), b)
}

func saveUsage(raw map[string]json.RawMessage) error {
	b, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return err
	}
	if _, err := parseUsage(b); err != nil {
		return err
	}
	return writeAtomic(usageFile(), b)
}

// writeAtomic writes a temp file next to name and renames it over name, so readers
// (the daemon's mtime re-read) never see a partial file.
func writeAtomic(name string, b []byte) error {
	dir := filepath.Dir(name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	mode := os.FileMode(0o644)
	if st, err := os.Stat(name); err == nil {
		mode = st.Mode().Perm()
	}
	f, err := os.CreateTemp(dir, "."+filepath.Base(name)+".*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name()) // no-op after a successful rename
	if _, err := f.Write(append(b, '\n')); err != nil {
		f.Close()
		return err
	}
	if err := f.Chmod(mode); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), name)
}

func printJSON(w io.Writer, v interface{}) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "%s\n", b)
	return err
}

func show(kind string, w io.Writer) error {
	var p Policy
	var u map[string]json.RawMessage
	var err error
	if kind != "usage" {
		if p, err = loadPolicy(); err != nil {
			return err
		}
	}
	if kind == "" || kind == "usage" {
		if u, err = loadUsageRaw(); err != nil {
			return err
		}
	}
	switch kind {
	case "":
		return printJSON(w, map[string]interface{}{"policy": p, "usage": u})
	case "agents":
		return printJSON(w, p.Agents)
	case "computers":
		return printJSON(w, p.Computers)
	case "classagents":
		return printJSON(w, p.ClassAgents)
	case "usage":
		return printJSON(w, u)
	case "policy":
		return printJSON(w, p)
	}
	return errUsage
}

func add(kind string, args []string, stdin io.Reader) error {
	p, err := loadPolicy()
	if err != nil {
		return err
	}
	switch kind {
	case "agent":
		if len(args) != 1 {
			return errUsage
		}
		a := args[0]
		if !agentRe.MatchString(a) {
			return fmt.Errorf("agent %q: want <service>/<account> matching %s", a, agentRe)
		}
		for _, x := range p.Agents {
			if x == a {
				return fmt.Errorf("agent %q already exists", a)
			}
		}
		p.Agents = append(p.Agents, a)
		sort.Strings(p.Agents)
	case "computer":
		if len(args) < 1 || strings.HasPrefix(args[0], "-") {
			return errUsage
		}
		name := args[0]
		fs := flag.NewFlagSet("add computer", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		replace := fs.Bool("replace", false, "")
		file := fs.String("file", "", "")
		if err := fs.Parse(args[1:]); err != nil {
			return fmt.Errorf("%v: %w", err, errUsage)
		}
		body, err := readBody(*file, fs.Args(), stdin)
		if err != nil {
			return err
		}
		c, err := parseComputer(body)
		if err != nil {
			return fmt.Errorf("computer %q: %v", name, err)
		}
		if _, ok := p.Computers[name]; ok && !*replace {
			return fmt.Errorf("computer %q already exists (use --replace)", name)
		}
		if p.Computers == nil {
			p.Computers = map[string]Computer{}
		}
		p.Computers[name] = c
	case "classagent":
		if len(args) != 2 {
			return errUsage
		}
		class, pat := args[0], args[1]
		if !isKnownClass(class) {
			return fmt.Errorf("class %q: want one of %v", class, knownClasses)
		}
		if _, err := path.Match(pat, ""); err != nil {
			return fmt.Errorf("pattern %q: %v", pat, err)
		}
		for _, x := range p.ClassAgents[class] {
			if x == pat {
				return fmt.Errorf("classagent %s %q already exists", class, pat)
			}
		}
		if p.ClassAgents == nil {
			p.ClassAgents = map[string][]string{}
		}
		p.ClassAgents[class] = append(p.ClassAgents[class], pat)
	default:
		return errUsage
	}
	return savePolicy(p)
}

func isKnownClass(c string) bool {
	for _, k := range knownClasses {
		if k == c {
			return true
		}
	}
	return false
}

// readBody returns the JSON body from --file, a "-" positional, or piped stdin.
func readBody(file string, pos []string, stdin io.Reader) ([]byte, error) {
	switch {
	case len(pos) > 1 || (len(pos) == 1 && pos[0] != "-"):
		return nil, fmt.Errorf("unexpected args %v: %w", pos, errUsage)
	case file != "" && file != "-":
		return os.ReadFile(file)
	case stdin != nil:
		return io.ReadAll(stdin)
	case file == "-" || len(pos) == 1:
		return io.ReadAll(os.Stdin)
	}
	return nil, fmt.Errorf("no JSON body: pass --file f.json, - or pipe stdin")
}

func parseComputer(b []byte) (Computer, error) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		return Computer{}, err
	}
	for _, f := range computerNumFields {
		var n float64
		if v, ok := m[f]; !ok {
			return Computer{}, fmt.Errorf("missing field %q", f)
		} else if err := json.Unmarshal(v, &n); err != nil {
			return Computer{}, fmt.Errorf("field %q: not a number", f)
		}
	}
	var c Computer
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	return c, d.Decode(&c)
}

func setUsage(agent string, args []string, stdin io.Reader) error {
	if !agentRe.MatchString(agent) {
		return fmt.Errorf("agent %q: want <service>/<account>", agent)
	}
	raw, err := loadUsageRaw()
	if err != nil {
		return err
	}
	var u AgentUsage
	if v, ok := raw[agent]; ok {
		if err := json.Unmarshal(v, &u); err != nil {
			return err
		}
	}
	fs := flag.NewFlagSet("set usage", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	pct := func(dst *float64) func(string) error {
		return func(s string) error {
			v, err := strconv.ParseFloat(s, 64)
			if err == nil && (v < 0 || v > 100) {
				err = fmt.Errorf("want 0..100")
			}
			*dst = v
			return err
		}
	}
	reset := func(dst *time.Time) func(string) error {
		return func(s string) (err error) { *dst, err = time.Parse(time.RFC3339, s); return }
	}
	fs.Func("5h", "", pct(&u.FiveHour.UsedPct))
	fs.Func("5h-reset", "", reset(&u.FiveHour.ResetsAt))
	fs.Func("7d", "", pct(&u.SevenDay.UsedPct))
	fs.Func("7d-reset", "", reset(&u.SevenDay.ResetsAt))
	file := fs.String("file", "", "")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("%v: %w", err, errUsage)
	}
	nflags := 0
	fs.Visit(func(*flag.Flag) { nflags++ })
	if *file != "" || len(fs.Args()) > 0 || nflags == 0 {
		if nflags > 1 || (nflags == 1 && *file == "") {
			return fmt.Errorf("--file/stdin cannot be combined with window flags: %w", errUsage)
		}
		body, err := readBody(*file, fs.Args(), stdin)
		if err != nil {
			return err
		}
		d := json.NewDecoder(bytes.NewReader(body))
		d.DisallowUnknownFields()
		u = AgentUsage{}
		if err := d.Decode(&u); err != nil {
			return fmt.Errorf("usage body: %v", err)
		}
	}
	b, err := json.Marshal(u)
	if err != nil {
		return err
	}
	raw[agent] = b
	return saveUsage(raw)
}

func rm(kind string, args []string) error {
	if kind == "usage" {
		if len(args) != 1 {
			return errUsage
		}
		raw, err := loadUsageRaw()
		if err != nil {
			return err
		}
		if _, ok := raw[args[0]]; !ok || strings.HasPrefix(args[0], "_") {
			return fmt.Errorf("usage %q not found", args[0])
		}
		delete(raw, args[0])
		return saveUsage(raw)
	}
	p, err := loadPolicy()
	if err != nil {
		return err
	}
	if _, err := os.Stat(policyFile()); err != nil {
		return err // nothing to remove from the built-in default
	}
	switch {
	case kind == "agent" && len(args) == 1:
		n := len(p.Agents)
		p.Agents = without(p.Agents, args[0])
		if len(p.Agents) == n {
			return fmt.Errorf("agent %q not found", args[0])
		}
	case kind == "computer" && len(args) == 1:
		if _, ok := p.Computers[args[0]]; !ok {
			return fmt.Errorf("computer %q not found", args[0])
		}
		delete(p.Computers, args[0])
	case kind == "classagent" && len(args) == 1:
		if _, ok := p.ClassAgents[args[0]]; !ok {
			return fmt.Errorf("classagent %q not found", args[0])
		}
		delete(p.ClassAgents, args[0])
	case kind == "classagent" && len(args) == 2:
		pats := p.ClassAgents[args[0]]
		p.ClassAgents[args[0]] = without(pats, args[1])
		if len(p.ClassAgents[args[0]]) == len(pats) {
			return fmt.Errorf("classagent %s %q not found", args[0], args[1])
		}
	default:
		return errUsage
	}
	return savePolicy(p)
}
