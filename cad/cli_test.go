package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// cliEnv points CAD_POLICY / CAD_USAGE at a temp dir and returns their paths.
func cliEnv(t *testing.T) (string, string) {
	d := t.TempDir()
	p, u := filepath.Join(d, "p.json"), filepath.Join(d, "u.json")
	t.Setenv("CAD_POLICY", p)
	t.Setenv("CAD_USAGE", u)
	return p, u
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
	p, _ := cliEnv(t)
	for _, a := range [][]string{
		{"add", "agent", "claude/test"},
		{"add", "classagent", "needs-db", "claude/*"},
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
	var all struct {
		Policy Policy                     `json:"policy"`
		Usage  map[string]json.RawMessage `json:"usage"`
	}
	if err := json.Unmarshal([]byte(out), &all); err != nil {
		t.Fatal(err, out)
	}
	if all.Policy.Computers["local"].MaxMemMB != 16384 || all.Policy.ClassAgents["needs-db"][0] != "claude/*" || all.Policy.Gate.MemoryMB != 1500 {
		t.Fatalf("show: %+v", all.Policy)
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
	p, _ := cliEnv(t)
	run(t, "", "add", "agent", "claude/test")
	before, _ := os.ReadFile(p)
	for _, c := range []struct {
		stdin string
		args  []string
	}{
		{"", []string{"add", "agent", "claude/test"}},              // duplicate
		{"", []string{"add", "agent", "Claude/x"}},                 // bad name
		{"", []string{"add", "classagent", "nope", "claude/*"}},    // unknown class
		{"", []string{"add", "classagent", "retry", "claude/["}},   // bad pattern
		{"{not json", []string{"add", "computer", "x", "-"}},       // invalid JSON
		{`{"vcpuHourUSD":1}`, []string{"add", "computer", "x"}},    // missing fields
		{"", []string{"rm", "agent", "claude/none"}},               // not found
		{"", []string{"set", "usage", "claude/test", "--5h", "x"}}, // bad pct
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

func TestCLISetUsagePartial(t *testing.T) {
	_, u := cliEnv(t)
	os.WriteFile(u, []byte(`{"_provisional":true,"codex/x":{"fiveHour":{"usedPct":5,"resetsAt":"2026-09-27T15:00:00Z"},"sevenDay":{"usedPct":6,"resetsAt":"2026-10-03T00:00:00Z"}}}`), 0o644)
	if out, code := run(t, "", "set", "usage", "codex/x", "--5h", "42"); code != 0 {
		t.Fatal(out)
	}
	if out, code := run(t, "", "set", "usage", "claude/new", "--7d", "7", "--7d-reset", "2026-10-04T00:00:00Z"); code != 0 {
		t.Fatal(out)
	}
	b, _ := os.ReadFile(u)
	if !strings.Contains(string(b), `"_provisional": true`) {
		t.Fatalf("_provisional dropped: %s", b)
	}
	got, err := parseUsage(b)
	if err != nil {
		t.Fatal(err)
	}
	x := got["codex/x"]
	if x.FiveHour.UsedPct != 42 || x.FiveHour.ResetsAt.IsZero() || x.SevenDay.UsedPct != 6 {
		t.Fatalf("partial update: %+v", x)
	}
	if n := got["claude/new"]; n.SevenDay.UsedPct != 7 || n.SevenDay.ResetsAt.Day() != 4 {
		t.Fatalf("new agent: %+v", n)
	}
	if out, code := run(t, `{"fiveHour":{"usedPct":1}}`, "set", "usage", "codex/x", "-"); code != 0 {
		t.Fatal(out)
	}
	if got := currentUsage()["codex/x"]; got.FiveHour.UsedPct != 1 || got.SevenDay.UsedPct != 0 {
		t.Fatalf("stdin replace: %+v", got)
	}
}

func TestCLIRm(t *testing.T) {
	_, u := cliEnv(t)
	run(t, "", "add", "agent", "claude/a")
	run(t, "", "add", "agent", "claude/b")
	run(t, "", "add", "classagent", "retry", "claude/*")
	run(t, "", "add", "classagent", "retry", "codex/*")
	run(t, localJSON, "add", "computer", "local")
	run(t, "", "set", "usage", "claude/a", "--5h", "1")
	os.WriteFile(u, append([]byte(`{"_provisional":true,`), mustRead(t, u)[1:]...), 0o644)
	for _, a := range [][]string{
		{"rm", "agent", "claude/a"},
		{"rm", "classagent", "retry", "codex/*"},
		{"rm", "computer", "local"},
		{"rm", "usage", "claude/a"},
	} {
		if out, code := run(t, "", a...); code != 0 {
			t.Fatalf("%v: %s", a, out)
		}
	}
	pol, _ := loadPolicy()
	if len(pol.Agents) != 1 || pol.Agents[0] != "claude/b" || len(pol.ClassAgents["retry"]) != 1 || len(pol.Computers) != 0 {
		t.Fatalf("after rm: %+v", pol)
	}
	if _, code := run(t, "", "rm", "usage", "_provisional"); code != 1 {
		t.Fatal("rm usage _provisional allowed")
	}
	if b := string(mustRead(t, u)); strings.Contains(b, "claude/a") || !strings.Contains(b, "_provisional") {
		t.Fatalf("usage after rm: %s", b)
	}
	if _, code := run(t, "", "rm", "classagent", "retry"); code != 0 {
		t.Fatal("rm whole class failed")
	}
}

func TestIsCLI(t *testing.T) {
	if isCLI(nil) || isCLI([]string{"-addr"}) || !isCLI([]string{"show"}) || !isCLI([]string{"-h"}) {
		t.Fatal("isCLI")
	}
}

func mustRead(t *testing.T, p string) []byte {
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
