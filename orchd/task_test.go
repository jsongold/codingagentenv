package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
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
	old, oldIn := shell, shellIn
	t.Cleanup(func() { shell, shellIn = old, oldIn })
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
	shellIn = func(stdin io.Reader, name string, args ...string) (string, error) { // calls[i][0] = stdin
		in := []byte{}
		if stdin != nil {
			in, _ = io.ReadAll(stdin)
		}
		return shell(string(in), name, args...)
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

func TestDispatchSubstitutesCmd(t *testing.T) {
	taskEnv(t, reg)
	pf := filepath.Join(t.TempDir(), "wake.md")
	os.WriteFile(pf, []byte("wake up"), 0o644)
	calls := fakeShell(t, map[string]string{"claude -p": `{"result":"ok"}`})
	pl := `{"runner":{"mode":"cloud","cmd":["claude","-p","--cloud","{session}","--model={model}"],"session":"s 1","model":"m","stdin":"` + pf + `"}}`
	code, m, errs := runTask(t, "dispatch", "--placement", pl)
	if code != 0 || m["result"] != "ok" {
		t.Fatalf("code %d %v %s", code, m, errs)
	}
	if want := []string{"wake up", "claude", "-p", "--cloud", "s 1", "--model=m"}; len(*calls) != 1 || !slices.Equal((*calls)[0], want) {
		t.Errorf("calls %q want %q", *calls, want)
	}
}

func TestDispatchBadInput(t *testing.T) {
	taskEnv(t, reg)
	calls := fakeShell(t, nil)
	for _, pl := range []string{
		`{"runner":{"cmd":["claude","--cloud","{session}"]}}`,              // missing key
		`{"runner":{"cmd":["claude","--cloud","{session}"],"session":""}}`, // empty value
		`{"runner":{"mode":"subagent"}}`,                                   // no cmd
		`{`,
	} {
		if code, _, errs := runTask(t, "dispatch", "--placement", pl); code != 2 {
			t.Errorf("%s: code %d (%s)", pl, code, errs)
		}
	}
	if code, _, _ := runTask(t, "dispatch", "--issue", "7", "--placement", `{}`); code != 2 {
		t.Errorf("--issue is gone: code %d", code)
	}
	if len(*calls) != 0 {
		t.Errorf("nothing may run: %v", *calls)
	}
}

func TestPlaceSleepFillsSession(t *testing.T) { // cad-2's timer: no --class, session-only registry, no git/gh
	taskEnv(t, `{"default":{"cloudWorkerSession":"sess-1"}}`)
	fakeCad(t, seedUsage(), 0)
	calls := fakeShell(t, nil)
	code, m, errs := runTask(t, "place", "--mode", "sleep")
	if code != 0 {
		t.Fatalf("code %d %s", code, errs)
	}
	rn := m["runner"].(map[string]any)
	if m["computer"] != "claude-cloud" || rn["session"] != "sess-1" || rn["stdin"] != "/app/orchd/wake.md" {
		t.Errorf("out %v", m)
	}
	if len(*calls) != 0 {
		t.Errorf("no git/gh: %v", *calls)
	}
}
