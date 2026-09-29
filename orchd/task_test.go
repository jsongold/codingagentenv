package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// fakeShell answers a command by the answers key it starts with ("<name> <args...>") and records every call
// (calls[i][0] = stdin).
func fakeShell(t *testing.T, answers map[string]string) *[][]string {
	t.Helper()
	calls := &[][]string{}
	old := shellIn
	t.Cleanup(func() { shellIn = old })
	shellIn = func(stdin io.Reader, name string, args ...string) (string, error) {
		in := []byte{}
		if stdin != nil {
			in, _ = io.ReadAll(stdin)
		}
		*calls = append(*calls, append([]string{string(in), name}, args...))
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

// orchd place prints runner {} when nothing was placed (deferred / no rule fits / cad not ready);
// dispatch must run nothing and just echo {}, not treat it as an error.
func TestDispatchRunnerEmptyRunsNothing(t *testing.T) {
	taskEnv(t, reg)
	calls := fakeShell(t, nil)
	for _, pl := range []string{`{"agent":"","runner":{}}`, `{}`} {
		code, m, errs := runTask(t, "dispatch", "--placement", pl)
		if code != 0 || len(m) != 0 {
			t.Errorf("%s: code %d %v (%s)", pl, code, m, errs)
		}
	}
	if len(*calls) != 0 {
		t.Errorf("nothing may run: %v", *calls)
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
	if want := filepath.Join(filepath.Dir(policyFile()), "wake.md"); m["computer"] != "claude-cloud" || rn["session"] != "sess-1" || rn["stdin"] != want {
		t.Errorf("out %v", m)
	}
	if len(*calls) != 0 {
		t.Errorf("no git/gh: %v", *calls)
	}
}

// runner.cmd's {configDir} (claude@claude-cloud) is filled from the placed claude/<id> agent,
// mirroring cad's usage-collector mapping (cad/usage.go usageStore).
func TestPlaceSleepFillsConfigDir(t *testing.T) {
	taskEnv(t, `{"default":{"cloudWorkerSession":"sess-1"}}`)
	fakeCad(t, seedUsage(), 0)
	fakeShell(t, nil)
	home := t.TempDir()
	t.Setenv("HOME", home)
	code, m, errs := runTask(t, "place", "--mode", "sleep")
	if code != 0 {
		t.Fatalf("code %d %s", code, errs)
	}
	rn := m["runner"].(map[string]any)
	if want := filepath.Join(home, ".aienv", ".store", "a12e00a7"); rn["configDir"] != want {
		t.Errorf("configDir %v want %s", rn["configDir"], want)
	}
}

// runner.stdin in policy.json is relative to the policy file's own directory, not the CWD or a
// hardcoded app dir: ORCHD_POLICY pointing elsewhere must resolve stdin next to it.
func TestPlaceStdinRelativeToPolicyDir(t *testing.T) {
	taskEnv(t, reg)
	fakeCad(t, seedUsage(), 0)
	fakeShell(t, nil)
	d := t.TempDir()
	pol, err := os.ReadFile("policy.json")
	if err != nil {
		t.Fatal(err)
	}
	pf := filepath.Join(d, "policy.json")
	os.WriteFile(pf, pol, 0o644)
	t.Setenv("ORCHD_POLICY", pf)
	code, m, errs := runTask(t, "place", "--mode", "sleep")
	if code != 0 {
		t.Fatalf("code %d %s", code, errs)
	}
	rn := m["runner"].(map[string]any)
	if want := filepath.Join(d, "wake.md"); rn["stdin"] != want {
		t.Errorf("stdin %v want %s", rn["stdin"], want)
	}
}
