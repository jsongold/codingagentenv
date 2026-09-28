package main

import (
	"strings"
	"testing"
)

func TestWakeSkipsWhenModeNotSleep(t *testing.T) {
	taskEnv(t, reg)
	t.Setenv("ORCHD_MODE", "")
	calls := fakeShell(t, nil)
	code, m, errs := runTask(t, "wake")
	if code != 0 || m["skipped"] != true || m["mode"] != "auto" {
		t.Fatalf("code %d %v %s", code, m, errs)
	}
	if len(*calls) != 0 {
		t.Errorf("mode != sleep must not touch claude at all: %v", *calls)
	}
}

func TestWakeSendsFixedPromptWhenSleep(t *testing.T) {
	taskEnv(t, `{"default":{"cloudWorkerSession":"sess-1"}}`) // session only, like cad-2
	t.Setenv("ORCHD_MODE", "sleep")
	calls := fakeShell(t, map[string]string{"claude -p": `{"result":"ok"}`})
	code, m, errs := runTask(t, "wake")
	if code != 0 || m["result"] != "ok" {
		t.Fatalf("code %d %v %s", code, m, errs)
	}
	var claudeCalls int
	for _, c := range *calls {
		if c[1] != "claude" {
			t.Errorf("wake must call only claude: %v", c)
			continue
		}
		claudeCalls++
		got := strings.Join(append([]string{c[1], c[2]}, c[4:]...), " ")
		if got != "claude -p --cloud sess-1 --output-format json" {
			t.Errorf("claude call shape: %v", c)
		}
		if c[3] != wakePrompt {
			t.Errorf("prompt: got %q want %q", c[3], wakePrompt)
		}
	}
	if claudeCalls != 1 {
		t.Errorf("want exactly 1 claude call, got %d: %v", claudeCalls, *calls)
	}
}
