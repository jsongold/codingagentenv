package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// fakeShell answers a command by the answers key it starts with ("<name> <args...>") and records every call.
func fakeShell(t *testing.T, answers map[string]string) *[][]string {
	t.Helper()
	calls := &[][]string{}
	old, oldBg := shell, startBg
	t.Cleanup(func() { shell, startBg = old, oldBg })
	shell = func(dir, name string, args ...string) (string, error) {
		*calls = append(*calls, append([]string{dir, name}, args...))
		full := strings.Join(append([]string{name}, args...), " ")
		for key, a := range answers {
			if strings.HasPrefix(full, key) {
				if a == "ERR" {
					return "", errors.New(key + " failed")
				}
				return a, nil
			}
		}
		return "", nil
	}
	startBg = func(dir, log, name string, args ...string) (int, error) {
		*calls = append(*calls, append([]string{"BG", dir, log, name}, args...))
		return 42, nil
	}
	return calls
}

func taskEnv(t *testing.T, reg string) {
	t.Helper()
	d := t.TempDir()
	f := filepath.Join(d, "ns.json")
	os.WriteFile(f, []byte(reg), 0o644)
	t.Setenv("ORCHD_NAMESPACES", f)
	t.Setenv("ORCHD_STATE_DIR", d)
	t.Setenv("ORCHD_POLICY", "policy.json")
	t.Setenv("CLAUDE_CLOUD_SESSION", "")
}

const reg = `{"default":{"repo":"o/r","path":"/src/r","cloudWorkerSession":"sess-1"}}`

func runTask(t *testing.T, args ...string) (int, map[string]any, string) {
	t.Helper()
	var out, errb bytes.Buffer
	code := run(args, &out, &errb)
	var m map[string]any
	json.Unmarshal(out.Bytes(), &m)
	return code, m, errb.String()
}

func TestPickOldestWithoutWip(t *testing.T) {
	taskEnv(t, reg)
	calls := fakeShell(t, map[string]string{"gh issue list": `[
		{"number":3,"title":"t3","body":"b3","createdAt":"2026-09-03T00:00:00Z","labels":[{"name":"ai"}]},
		{"number":0,"title":"t0","body":"b0","createdAt":"2026-08-01T00:00:00Z","labels":[{"name":"ai-failed"}]},
		{"number":1,"title":"t1","body":"b1","createdAt":"2026-09-01T00:00:00Z","labels":[{"name":"wip"}]},
		{"number":2,"title":"t2","body":"b2","createdAt":"2026-09-02T00:00:00Z","labels":[]}]`})
	code, m, errs := runTask(t, "pick")
	if code != 0 {
		t.Fatalf("code %d: %s", code, errs)
	}
	if is := m["issue"].(map[string]any); is["n"] != 2.0 || is["title"] != "t2" || is["body"] != "b2" {
		t.Errorf("issue %v", is)
	}
	cs := m["classes"].([]any)
	if c := cs[0].(map[string]any); c["name"] == "" || c["criteria"] == "" || len(cs) < 2 {
		t.Errorf("classes %v", cs)
	}
	if !slices.Contains((*calls)[0], "-label:wip -label:ai-failed sort:created-asc") {
		t.Errorf("list must filter claimed issues server-side: %v", (*calls)[0])
	}
	if got := strings.Join((*calls)[1][1:], " "); got != "gh issue edit 2 --repo o/r --add-label wip" {
		t.Errorf("claim: %s", got)
	}
}

func TestPickNone(t *testing.T) {
	taskEnv(t, reg)
	calls := fakeShell(t, map[string]string{"gh issue list": `[{"number":1,"createdAt":"x","labels":[{"name":"wip"}]}]`})
	if code, m, _ := runTask(t, "pick"); code != 0 || m["none"] != true || len(*calls) != 1 {
		t.Errorf("code %d %v calls %v", code, m, *calls)
	}
}

func TestPickRepoFromGit(t *testing.T) { // unknown ns: path = git toplevel, repo = gh repo view there
	taskEnv(t, `{}`)
	calls := fakeShell(t, map[string]string{"git rev-parse --show-toplevel": "/w/x\n", "gh repo view": "me/x\n", "gh issue list": "[]"})
	runTask(t, "pick", "--ns", "other")
	if c := (*calls)[1]; c[0] != "/w/x" || c[1] != "gh" {
		t.Errorf("repo view call %v", c)
	}
	if got := strings.Join((*calls)[2][1:6], " "); got != "gh issue list --repo me/x" {
		t.Errorf("list call %s", got)
	}
}

const issueJSON = `{"title":"Fix it","body":"details"}`

func TestDispatchSubagent(t *testing.T) {
	taskEnv(t, reg)
	calls := fakeShell(t, map[string]string{"gh issue view": issueJSON, "git -C /src/r rev-parse": "ERR"})
	code, m, errs := runTask(t, "dispatch", "--issue", "7", "--placement", `{"agent":"claude/a","runner":{"mode":"subagent"}}`)
	if code != 0 {
		t.Fatalf("code %d: %s", code, errs)
	}
	if m["runner"] != "subagent" || m["worktree"] != "/src/r-task-7" {
		t.Errorf("out %v", m)
	}
	p, _ := m["prompt"].(string)
	for _, want := range []string{"o/r の Issue #7", "Fix it", "details", "task/7", "Closes #7"} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt lacks %q:\n%s", want, p)
		}
	}
	if got := strings.Join((*calls)[len(*calls)-1][1:], " "); got != "git -C /src/r worktree add -q -b task/7 /src/r-task-7 origin/main" {
		t.Errorf("worktree: %s", got)
	}
}

func TestDispatchProcess(t *testing.T) {
	taskEnv(t, reg)
	calls := fakeShell(t, map[string]string{"gh issue view": issueJSON, "git -C /src/r rev-parse": "ERR"})
	pl := `{"runner":{"mode":"process","cmd":"opencode run --model {model}","model":"m1"}}`
	code, m, errs := runTask(t, "dispatch", "--issue", "7", "--placement", pl)
	if code != 0 || m["started"] != true || m["pid"] != 42.0 || m["worktree"] != "/src/r-task-7" {
		t.Fatalf("code %d %v %s", code, m, errs)
	}
	bg := (*calls)[len(*calls)-1]
	if got := strings.Join(append([]string{bg[1]}, bg[3:7]...), " "); got != "/src/r-task-7 opencode run --model m1" || !strings.Contains(bg[7], "Closes #7") {
		t.Errorf("bg call %v", bg)
	}
}

func TestDispatchCloud(t *testing.T) {
	taskEnv(t, reg)
	t.Setenv("CLAUDE_CLOUD_SESSION", "sess-env") // env wins over the registry
	calls := fakeShell(t, map[string]string{"gh issue view": issueJSON, "claude -p": `{"result":"ok"}`})
	code, m, errs := runTask(t, "dispatch", "--issue", "7", "--placement", `{"runner":{"mode":"cloud","cmd":"claude --cloud"}}`)
	if code != 0 || m["result"] != "ok" {
		t.Fatalf("code %d %v %s", code, m, errs)
	}
	c := (*calls)[len(*calls)-1]
	if got := strings.Join(append(slices.Clone(c[1:3]), c[4:]...), " "); got != "claude -p --cloud sess-env --output-format json" || !strings.Contains(c[3], "o/r の Issue #7") {
		t.Errorf("claude call %v", c)
	}
	for _, c := range *calls {
		if c[1] == "git" {
			t.Errorf("cloud must not create a local worktree: %v", c)
		}
	}
}

func TestDispatchBadInput(t *testing.T) {
	taskEnv(t, `{"default":{"repo":"o/r","path":"/src/r"}}`)
	fakeShell(t, map[string]string{"gh issue view": issueJSON})
	for _, args := range [][]string{
		{"--issue", "x", "--placement", `{"runner":{"mode":"subagent"}}`},
		{"--issue", "7", "--placement", `{`},
		{"--issue", "7", "--placement", `{"runner":{"mode":"hatchet"}}`},
		{"--issue", "7", "--placement", `{"runner":{"mode":"cloud"}}`}, // no session anywhere
	} {
		if code, _, errs := runTask(t, append([]string{"dispatch"}, args...)...); code != 2 {
			t.Errorf("%v: code %d (%s)", args, code, errs)
		}
	}
}

func TestDispatchGhFailure(t *testing.T) {
	taskEnv(t, reg)
	fakeShell(t, map[string]string{"gh issue view": "ERR"})
	if code, _, _ := runTask(t, "dispatch", "--issue", "7", "--placement", `{"runner":{"mode":"subagent"}}`); code != 1 {
		t.Errorf("code %d", code)
	}
}

func TestStatus(t *testing.T) {
	taskEnv(t, reg)
	calls := fakeShell(t, map[string]string{"gh pr list": `[
		{"url":"u/9","state":"OPEN","body":"Closes #70"},
		{"url":"u/8","state":"CLOSED","body":"Closes #7"},
		{"url":"u/10","state":"OPEN","body":"fix: x\n\ncloses #7"}]`})
	code, m, errs := runTask(t, "status", "--issue", "7", "--pid", strconv.Itoa(os.Getpid()))
	if code != 0 || m["pr"] != "u/10" || m["state"] != "OPEN" || m["running"] != true {
		t.Fatalf("code %d %v %s", code, m, errs)
	}
	if got := strings.Join((*calls)[0][1:], " "); !strings.Contains(got, "--search Closes #7") {
		t.Errorf("call %s", got)
	}
	fakeShell(t, map[string]string{"gh pr list": `[{"url":"u/9","state":"OPEN","body":"Closes #70"}]`})
	if _, m, _ := runTask(t, "status", "--issue", "7"); m["pr"] != nil || m["state"] != nil {
		t.Errorf("no PR: %v", m)
	}
}

func TestDispatchPromptFileCloud(t *testing.T) {
	taskEnv(t, `{"default":{"cloudWorkerSession":"sess-1"}}`) // session only, like cad-2
	d := t.TempDir()
	pf := filepath.Join(d, "wake.md")
	os.WriteFile(pf, []byte("wake up"), 0o644)
	calls := fakeShell(t, map[string]string{"claude -p": `{"result":"ok"}`})
	code, m, errs := runTask(t, "dispatch", "--prompt-file", pf, "--placement", `{"runner":{"mode":"cloud"}}`)
	if code != 0 || m["result"] != "ok" {
		t.Fatalf("code %d %v %s", code, m, errs)
	}
	if len(*calls) != 1 {
		t.Fatalf("want exactly 1 call (no git/gh), got %v", *calls)
	}
	c := (*calls)[0]
	if got := strings.Join(append([]string{c[1], c[2]}, c[4:]...), " "); got != "claude -p --cloud sess-1 --output-format json" || c[3] != "wake up" {
		t.Errorf("claude call %v", c)
	}
}

func TestDispatchPromptFileNonCloud(t *testing.T) {
	taskEnv(t, `{"default":{"cloudWorkerSession":"sess-1"}}`)
	d := t.TempDir()
	pf := filepath.Join(d, "wake.md")
	os.WriteFile(pf, []byte("wake up"), 0o644)
	fakeShell(t, nil)
	if code, _, errs := runTask(t, "dispatch", "--prompt-file", pf, "--placement", `{"runner":{"mode":"subagent"}}`); code != 2 {
		t.Errorf("code %d (%s)", code, errs)
	}
}

func TestDispatchReusesBranchOfEarlierRun(t *testing.T) { // the branch exists (earlier dispatch), the worktree does not
	taskEnv(t, reg)
	calls := fakeShell(t, map[string]string{"gh issue view": issueJSON})
	if code, _, errs := runTask(t, "dispatch", "--issue", "7", "--placement", `{"runner":{"mode":"subagent"}}`); code != 0 {
		t.Fatalf("code %d %s", code, errs)
	}
	if got := strings.Join((*calls)[len(*calls)-1][1:], " "); got != "git -C /src/r worktree add -q /src/r-task-7 task/7" {
		t.Errorf("worktree: %s", got)
	}
}
