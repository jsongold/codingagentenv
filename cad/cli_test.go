package main

import (
	"bytes"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// cliEnv points CAD_POLICY at a temp dir and returns its path.
func cliEnv(t *testing.T) string {
	p := filepath.Join(t.TempDir(), "p.json")
	t.Setenv("CAD_POLICY", p)
	return p
}

func run(t *testing.T, stdin string, args ...string) (string, int) {
	t.Helper()
	var in *strings.Reader
	if stdin != "" {
		in = strings.NewReader(stdin)
	}
	var out, errb bytes.Buffer
	var code int
	if in != nil {
		code = runCLI(args, in, &out, &errb)
	} else {
		code = runCLI(args, nil, &out, &errb)
	}
	return out.String() + errb.String(), code
}

const localJSON = `{"vcpuHourUSD":0,"gibHourUSD":0,"minBillSec":0,"coldStartSec":0,"caps":{"db":true},"maxMemMB":16384,"maxCpus":8,"maxMin":0}`

func TestCLIAddShowRoundtrip(t *testing.T) {
	p := cliEnv(t)
	os.WriteFile(p, []byte(`{"gate":{"memoryMB":1500},"classes":{"x":{"estPct":1}}}`), 0o644) // the daemon requires classes; the CLI keeps them
	for _, a := range [][]string{
		{"add", "agent", "claude/test"},
		{"add", "computer", "local", "-"},
	} {
		if out, code := run(t, localJSON, a...); code != 0 {
			t.Fatalf("%v: %d %s", a, code, out)
		}
	}
	out, _ := run(t, "", "show", "agents")
	if !strings.Contains(out, `"claude/test"`) {
		t.Fatalf("show agents: %s", out)
	}
	out, _ = run(t, "", "show")
	var all Policy
	if err := json.Unmarshal([]byte(out), &all); err != nil {
		t.Fatal(err, out)
	}
	if all.Computers["local"].MaxMemMB != 16384 || all.Gate.MemoryMB != 1500 {
		t.Fatalf("show: %+v", all)
	}
	// The daemon reads the same file.
	if got := currentPolicy(); len(got.Agents) != 1 || got.Source != p {
		t.Fatalf("daemon view: %+v", got)
	}
	if b, _ := os.ReadFile(p); strings.Contains(string(b), `"source"`) {
		t.Fatalf("source written to file")
	}
}

func TestCLIRejects(t *testing.T) {
	p := cliEnv(t)
	run(t, "", "add", "agent", "claude/test")
	before, _ := os.ReadFile(p)
	for _, c := range []struct {
		stdin string
		args  []string
	}{
		{"", []string{"add", "agent", "claude/test"}},              // duplicate
		{"", []string{"add", "agent", "Claude/x"}},                 // bad name
		{"{not json", []string{"add", "computer", "x", "-"}},       // invalid JSON
		{`{"vcpuHourUSD":1}`, []string{"add", "computer", "x"}},    // missing fields
		{"", []string{"rm", "agent", "claude/none"}},               // not found
		{"", []string{"set", "usage", "claude/test", "--5h", "1"}}, // removed: usage is collected
	} {
		if out, code := run(t, c.stdin, c.args...); code != 1 {
			t.Errorf("%v: want exit 1, got %d %s", c.args, code, out)
		}
	}
	after, _ := os.ReadFile(p)
	if !bytes.Equal(before, after) {
		t.Fatalf("policy changed by rejected commands")
	}
	run(t, localJSON, "add", "computer", "local")
	if _, code := run(t, localJSON, "add", "computer", "local"); code != 1 {
		t.Fatal("duplicate computer accepted")
	}
	if out, code := run(t, localJSON, "add", "computer", "local", "--replace"); code != 0 {
		t.Fatal(out)
	}
	// A corrupt file is reported, not overwritten.
	os.WriteFile(p, []byte("{bad"), 0o644)
	if _, code := run(t, "", "add", "agent", "claude/other"); code != 1 {
		t.Fatal("add over corrupt policy succeeded")
	}
	if b, _ := os.ReadFile(p); string(b) != "{bad" {
		t.Fatalf("corrupt policy overwritten: %s", b)
	}
}

func TestCLIRm(t *testing.T) {
	cliEnv(t)
	run(t, "", "add", "agent", "claude/a")
	run(t, "", "add", "agent", "claude/b")
	run(t, localJSON, "add", "computer", "local")
	for _, a := range [][]string{
		{"rm", "agent", "claude/a"},
		{"rm", "computer", "local"},
	} {
		if out, code := run(t, "", a...); code != 0 {
			t.Fatalf("%v: %s", a, out)
		}
	}
	pol, _ := loadPolicy()
	if len(pol.Agents) != 1 || pol.Agents[0] != "claude/b" || len(pol.Computers) != 0 {
		t.Fatalf("after rm: %+v", pol)
	}
}

func TestIsCLI(t *testing.T) {
	if isCLI(nil) || isCLI([]string{"-addr"}) || !isCLI([]string{"show"}) || !isCLI([]string{"-h"}) {
		t.Fatal("isCLI")
	}
}

func TestCLIShowSections(t *testing.T) {
	t.Setenv("CAD_POLICY", filepath.Join("..", ".agent", "policy.json"))
	for sec, want := range map[string]string{"classes": `"light-edit"`, "runners": `"subagent"`, "collect": `"60s"`, "rules": `"self"`} {
		if out, code := run(t, "", "show", sec); code != 0 || !strings.Contains(out, want) {
			t.Errorf("show %s: %d %s", sec, code, out)
		}
	}
}

func TestCLIShowCost(t *testing.T) {
	cliEnv(t)
	run(t, localJSON, "add", "computer", "local")
	run(t, `{"vcpuHourUSD":0.0258,"gibHourUSD":0.00645,"minBillSec":60,"coldStartSec":90,"maxMemMB":8192,"maxCpus":2,"maxMin":0}`, "add", "computer", "gce-spot")

	// hour/day/month, default shape (2 cpu / 8 GiB).
	for _, tc := range []struct {
		flag string
		want string
	}{
		{"--hour", `"period": "hour"`},
		{"--day", `"period": "day"`},
		{"--month", `"period": "month"`},
	} {
		out, code := run(t, "", "show", "cost", "--computer", tc.flag)
		if code != 0 || !strings.Contains(out, tc.want) {
			t.Fatalf("%s: %d %s", tc.flag, code, out)
		}
	}

	// filter to one computer; flags may surround the name.
	out, code := run(t, "", "show", "cost", "gce-spot", "--computer", "--day")
	if code != 0 || !strings.Contains(out, `"computer": "gce-spot"`) || strings.Contains(out, "local") {
		t.Fatalf("filter: %d %s", code, out)
	}
	out, code = run(t, "", "show", "cost", "--computer", "--day", "gce-spot")
	if code != 0 || !strings.Contains(out, `"computer": "gce-spot"`) {
		t.Fatalf("filter (flags first): %d %s", code, out)
	}

	// --cpus/--mem change the shape.
	out, code = run(t, "", "show", "cost", "gce-spot", "--computer", "--month", "--cpus", "4", "--mem", "16")
	if code != 0 {
		t.Fatalf("shape: %d %s", code, out)
	}
	var got struct {
		Prices []costEntry `json:"prices"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err, out)
	}
	if want := (0.0258*4 + 0.00645*16) * 730; len(got.Prices) != 1 || got.Prices[0].USD != math.Round(want*10000)/10000 {
		t.Fatalf("shape price: %+v want %v", got.Prices, want)
	}

	// local is always priced 0.
	out, _ = run(t, "", "show", "cost", "local", "--computer", "--hour")
	if !strings.Contains(out, `"usd": 0`) {
		t.Fatalf("local: %s", out)
	}

	// sorted by price ascending, then name.
	out, _ = run(t, "", "show", "cost", "--computer", "--day")
	if i, j := strings.Index(out, "local"), strings.Index(out, "gce-spot"); i == -1 || j == -1 || i > j {
		t.Fatalf("sort order: %s", out)
	}

	// bad usage -> exit 2.
	for _, args := range [][]string{
		{"show", "cost", "--day"},                              // missing --computer
		{"show", "cost", "--computer"},                         // missing period
		{"show", "cost"},                                       // missing both
		{"show", "cost", "--computer", "--day", "--hour"},      // duplicate period
		{"show", "cost", "--computer", "--day", "--cpus", "0"}, // bad cpus
		{"show", "cost", "--computer", "--day", "--mem", "-1"}, // bad mem
	} {
		if _, code := run(t, "", args...); code != 2 {
			t.Errorf("%v: want exit 2, got %d", args, code)
		}
	}

	// unknown computer -> exit 1.
	if _, code := run(t, "", "show", "cost", "nope", "--computer", "--day"); code != 1 {
		t.Fatalf("unknown computer: want exit 1, got %d", code)
	}
}

func TestCLIRejectsBadRunner(t *testing.T) {
	p := cliEnv(t)
	os.WriteFile(p, []byte(`{"runners":{"opencode":{"mode":"process"}}}`), 0o644)
	if out, code := run(t, "", "show", "runners"); code != 1 || !strings.Contains(out, "process needs cmd") {
		t.Fatalf("%d %s", code, out)
	}
}

// The daemon rejects a policy without classes (naming the old placement.estPct) and keeps the last good one.
func TestPolicyRequiresClasses(t *testing.T) {
	p := cliEnv(t)
	os.WriteFile(p, []byte(`{"classes":{"x":{"estPct":1}}}`), 0o644)
	if _, ok := currentPolicy().Classes["x"]; !ok {
		t.Fatal("good policy not loaded")
	}
	old := []byte(`{"placement":{"reservePct":15,"estPct":{"gate-heavy":4}}}`)
	os.WriteFile(p, old, 0o644)
	os.Chtimes(p, time.Now(), time.Now().Add(time.Minute))
	if _, ok := currentPolicy().Classes["x"]; !ok {
		t.Fatal("last good policy not kept")
	}
	if err := requireClasses(old, Policy{}); err == nil || !strings.Contains(err.Error(), "placement.estPct was replaced") {
		t.Fatalf("%v", err)
	}
}
