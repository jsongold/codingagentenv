package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const cliUsage = `usage:
  cad                                   run the server (env: CAD_ADDR, CAD_TOKEN, CAD_POLICY, CAD_SLOTS, CAD_USAGE_EVERY, CAD_CLAUDE_BIN, ...)
  cad get meta|<topic> -ns <namespace>  print the running cad's metadata as JSON (GET /v1/meta, /v1/<topic>; env: CAD_ADDR, CAD_TOKEN)
  cad show [collect|agents|computers|policy]  print records (no arg = the policy; rules/classes/runners: see orchd)
  cad show cost --computer [<name>] (--hour|--day|--month) [--cpus N] [--mem GiB]
                                     one machine's price for the period (month=730h); default shape is
                                     2 cpu/8GiB (e2-standard-2); no <name> = every computer, "local" is always 0
  cad show usage [--json] [--local]  subscription usage per agent (table, or --json); fetches the
                                     running cad's usage topic, falling back to collecting it locally
                                     if cad is not running (--local always collects locally)
  cad add agent <service/account>
  cad add computer <name> [--replace] [--file f.json | -]  (JSON body; piped stdin also works)
  cad rm agent <agent> | computer <name>
files: CAD_POLICY > .agent/policy.json. A running cad re-reads it on mtime change. Usage is collected by cad (topic "usage").
`

func without(xs []string, x string) []string {
	var out []string
	for _, v := range xs {
		if v != x {
			out = append(out, v)
		}
	}
	return out
}

var agentRe = regexp.MustCompile(`^[a-z0-9-]+/[a-z0-9-]+$`)

// computerNumFields must be present (as numbers) in a computer body.
var computerNumFields = []string{"vcpuHourUSD", "gibHourUSD", "minBillSec", "coldStartSec", "maxMemMB", "maxCpus", "maxMin"}

// isCLI reports whether args[0] selects the record CLI instead of the server.
func isCLI(args []string) bool {
	if len(args) == 0 {
		return false
	}
	switch args[0] {
	case "get", "show", "add", "rm", "help", "-h", "-help", "--help":
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
		if errors.Is(err, errNS) || errors.Is(err, errCostArgs) || errors.Is(err, errShowUsageArgs) {
			return 2
		}
		return 1
	}
	return 0
}

var (
	errUsage    = errors.New("bad usage")
	errNS       = fmt.Errorf("-ns <namespace> required (%s): %w", nsRe, errUsage)
	errCostArgs = fmt.Errorf("show cost: --computer required, plus exactly one of --hour/--day/--month; --cpus/--mem must be > 0: %w", errUsage)
)

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
		return show(kind, rest, stdout)
	case "add":
		return add(kind, rest, stdin)
	case "get":
		return getCmd(args[1:], stdout)
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

func show(kind string, args []string, w io.Writer) error {
	if kind == "cost" {
		return showCost(args, w)
	}
	if kind == "usage" {
		return showUsageCmd(args, w)
	}
	p, err := loadPolicy()
	if err != nil {
		return err
	}
	switch kind {
	case "agents":
		return printJSON(w, p.Agents)
	case "computers":
		return printJSON(w, p.Computers)
	case "collect":
		return printJSON(w, p.Collect)
	case "", "policy":
		return printJSON(w, p)
	}
	return errUsage
}

// costEntry is one row of `cad show cost`.
type costEntry struct {
	Computer string  `json:"computer"`
	USD      float64 `json:"usd"`
}

// showCost prints the price of running one machine of a given shape continuously for a period
// (hour/day/month; month = 730h). Args: --computer (required target selector, so `show` can grow
// other targets later), an optional <name> to filter to one computer, exactly one of
// --hour|--day|--month, and optional --cpus/--mem (default 2 cpu / 8 GiB, an e2-standard-2 shape).
// Flags and the optional <name> may appear in any order.
func showCost(args []string, w io.Writer) error {
	var computer, hour, day, month bool
	var name string
	cpus, mem := 2.0, 8.0
	for i := 0; i < len(args); i++ {
		switch a := args[i]; a {
		case "--computer":
			computer = true
		case "--hour":
			hour = true
		case "--day":
			day = true
		case "--month":
			month = true
		case "--cpus", "--mem":
			i++
			if i >= len(args) {
				return errCostArgs
			}
			v, err := strconv.ParseFloat(args[i], 64)
			if err != nil {
				return errCostArgs
			}
			if a == "--cpus" {
				cpus = v
			} else {
				mem = v
			}
		default:
			if strings.HasPrefix(a, "-") || name != "" {
				return errCostArgs
			}
			name = a
		}
	}
	period, hours := "", 0.0
	for _, pp := range []struct {
		on    bool
		name  string
		hours float64
	}{{hour, "hour", 1}, {day, "day", 24}, {month, "month", 730}} {
		if pp.on {
			if period != "" {
				return errCostArgs
			}
			period, hours = pp.name, pp.hours
		}
	}
	if !computer || period == "" || cpus <= 0 || mem <= 0 {
		return errCostArgs
	}

	p, err := loadPolicy()
	if err != nil {
		return err
	}
	price := func(c Computer) float64 {
		usd := (c.VCPUHourUSD*cpus + c.GiBHourUSD*mem) * hours
		return math.Round(usd*10000) / 10000
	}
	var rows []costEntry
	switch {
	case name == "local":
		rows = []costEntry{{"local", price(p.Computers["local"])}} // free even if absent from policy
	case name != "":
		c, ok := p.Computers[name]
		if !ok {
			return fmt.Errorf("unknown computer %q", name)
		}
		rows = []costEntry{{name, price(c)}}
	default:
		rows = make([]costEntry, 0, len(p.Computers)+1)
		hasLocal := false
		for cn, c := range p.Computers {
			rows = append(rows, costEntry{cn, price(c)})
			hasLocal = hasLocal || cn == "local"
		}
		if !hasLocal {
			rows = append(rows, costEntry{"local", 0})
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].USD != rows[j].USD {
			return rows[i].USD < rows[j].USD
		}
		return rows[i].Computer < rows[j].Computer
	})
	return printJSON(w, struct {
		Period string      `json:"period"`
		Hours  float64     `json:"hours"`
		CPUs   float64     `json:"cpus"`
		MemGiB float64     `json:"memGiB"`
		Prices []costEntry `json:"prices"`
	}{period, hours, cpus, mem, rows})
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
	default:
		return errUsage
	}
	return savePolicy(p)
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

func rm(kind string, args []string) error {
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
	default:
		return errUsage
	}
	return savePolicy(p)
}
