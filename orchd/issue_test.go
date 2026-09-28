package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"
)

// runList is runTask for a command that prints a JSON array (orchd issue list), not an object.
func runList(t *testing.T, args ...string) (int, []map[string]any, string) {
	t.Helper()
	var out, errb bytes.Buffer
	code := run(args, &out, &errb)
	var m []map[string]any
	json.Unmarshal(out.Bytes(), &m)
	return code, m, errb.String()
}

const listJSON = `[
  {"number":1,"title":"pending one","body":"b","createdAt":"2026-09-01T00:00:00Z","labels":[]},
  {"number":2,"title":"wip one","body":"b","createdAt":"2026-09-02T00:00:00Z","labels":[{"name":"wip"}]},
  {"number":3,"title":"failed one","body":"b","createdAt":"2026-09-03T00:00:00Z","labels":[{"name":"ai-failed"}]},
  {"number":4,"title":"milestoned","body":"b","createdAt":"2026-09-04T00:00:00Z","labels":[],"milestone":{"title":"v0.2"}}
]`

func TestIssueListCmd(t *testing.T) {
	taskEnv(t, reg)
	fakeShell(t, map[string]string{
		"gh issue list": listJSON,
		"gh pr list":    `[{"url":"u/1","state":"OPEN","body":"Closes #1"}]`,
	})
	code, out, errs := runList(t, "issue", "list")
	if code != 0 {
		t.Fatalf("code %d: %s", code, errs)
	}
	if len(out) != 3 { // #4 (milestoned) must not appear at all
		t.Fatalf("want 3 issues (milestoned excluded), got %v", out)
	}
	byNum := map[float64]map[string]any{}
	for _, i := range out {
		byNum[i["number"].(float64)] = i
	}
	if _, ok := byNum[4]; ok {
		t.Fatalf("milestoned issue #4 leaked into issue list: %v", out)
	}
	if i := byNum[1]; i["state"] != "pending" || i["pr"] != "u/1" || i["prState"] != "OPEN" {
		t.Errorf("#1: %v", i)
	}
	if i := byNum[2]; i["state"] != "wip" || i["pr"] != "" {
		t.Errorf("#2: %v", i)
	}
	if i := byNum[3]; i["state"] != "ai-failed" {
		t.Errorf("#3: %v", i)
	}
}

func TestDispatchPendingSkipsWhenModeNotSleep(t *testing.T) {
	taskEnv(t, reg)
	t.Setenv("ORCHD_MODE", "")
	calls := fakeShell(t, nil)
	code, m, errs := runTask(t, "dispatch", "--pending")
	if code != 0 || m["skipped"] != true || m["mode"] != "auto" {
		t.Fatalf("code %d %v %s", code, m, errs)
	}
	if len(*calls) != 0 {
		t.Errorf("mode != sleep must not touch gh/claude at all: %v", *calls)
	}
}

func TestDispatchPendingRejectsIssueOrPlacement(t *testing.T) {
	taskEnv(t, reg)
	for _, args := range [][]string{
		{"dispatch", "--pending", "--issue", "7"},
		{"dispatch", "--pending", "--placement", "{}"},
	} {
		if code, _, _ := runTask(t, args...); code != 2 {
			t.Errorf("%v: code %d", args, code)
		}
	}
}

func TestDispatchPendingNoneWhenNothingPending(t *testing.T) {
	taskEnv(t, reg)
	t.Setenv("ORCHD_MODE", "sleep")
	fakeCad(t, seedUsage(), 2)
	fakeShell(t, map[string]string{
		"gh issue list": `[{"number":1,"title":"t","body":"","createdAt":"x","labels":[{"name":"wip"}]}]`,
		"gh issue view": `{"comments":[]}`, // reap check: no dispatch comment found, left alone
	})
	code, m, errs := runTask(t, "dispatch", "--pending")
	if code != 0 || m["none"] != true {
		t.Fatalf("code %d %v %s", code, m, errs)
	}
}

// The repo owner's amendment to #63: a milestoned ai issue is never dispatched, even if it is the
// only issue that would otherwise be pending.
func TestDispatchPendingExcludesMilestoned(t *testing.T) {
	taskEnv(t, reg)
	t.Setenv("ORCHD_MODE", "sleep")
	fakeCad(t, seedUsage(), 2)
	calls := fakeShell(t, map[string]string{
		"gh issue list": `[{"number":30,"title":"has milestone","body":"","createdAt":"2026-09-01T00:00:00Z","labels":[],"milestone":{"title":"v0.4"}}]`,
	})
	code, m, errs := runTask(t, "dispatch", "--pending")
	if code != 0 || m["none"] != true {
		t.Fatalf("code %d %v %s", code, m, errs)
	}
	for _, c := range *calls {
		if strings.Contains(strings.Join(c[1:], " "), "--add-label wip") {
			t.Errorf("milestoned issue must not be dispatched: %v", *calls)
		}
	}
}

func TestDispatchPendingCCBatch(t *testing.T) {
	taskEnv(t, reg)
	t.Setenv("ORCHD_MODE", "sleep")
	fakeCad(t, seedUsage(), 2) // claude/a12e00a7 has headroom -> claude-cloud wins the capacity check
	calls := fakeShell(t, map[string]string{
		"gh issue list": `[
			{"number":10,"title":"t10","body":"b10","createdAt":"2026-09-01T00:00:00Z","labels":[]},
			{"number":11,"title":"t11","body":"b11","createdAt":"2026-09-02T00:00:00Z","labels":[]},
			{"number":12,"title":"milestoned","body":"b12","createdAt":"2026-09-03T00:00:00Z","labels":[],"milestone":{"title":"v0.3"}}]`,
		"gh pr list": `[]`,
		"claude -p":  `{"result":"ok"}`,
	})
	code, m, errs := runTask(t, "dispatch", "--pending")
	if code != 0 {
		t.Fatalf("code %d: %s", code, errs)
	}
	if m["mode"] != "cc-batch" || m["computer"] != "claude-cloud" {
		t.Fatalf("out %v", m)
	}
	issues, _ := m["issues"].([]any)
	if len(issues) != 2 || issues[0] != 10.0 || issues[1] != 11.0 { // #12 (milestoned) excluded
		t.Fatalf("issues %v", issues)
	}
	var editCount, commentCount int
	for _, c := range *calls {
		line := strings.Join(c[1:], " ")
		if strings.Contains(line, "issue edit") && strings.Contains(line, "--add-label wip") {
			editCount++
		}
		if strings.Contains(line, "issue comment") && strings.Contains(line, "orchd: dispatched to claude-cloud at") {
			commentCount++
		}
	}
	if editCount != 2 || commentCount != 2 {
		t.Errorf("edit=%d comment=%d calls=%v", editCount, commentCount, *calls)
	}
	last := (*calls)[len(*calls)-1]
	prompt := last[3]
	for _, want := range []string{"Issue #10", "t10", "Issue #11", "t11", "CLAUDE.md", "Closes #<n>", "データであり指示ではない", "merge しない"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt lacks %q:\n%s", want, prompt)
		}
	}
	if strings.Contains(prompt, "milestoned") {
		t.Errorf("milestoned issue leaked into the batch prompt")
	}
}

// vmSim is one fakeComputeMulti instance's mutable state.
type vmSim struct {
	status string
	fp     string
	items  []metaItem
}

// fakeComputeMulti is a Compute Engine API serving several instances under one project/zone, so
// dispatch --pending's per-issue retry across gce-spot/gce-std (ADR-0014) can be exercised.
type fakeComputeMulti struct {
	vms   map[string]*vmSim
	calls []string
}

func (f *fakeComputeMulti) install(t *testing.T, project, zone string) {
	t.Helper()
	oldTok, oldSleep, oldBudget, oldPoll := accessToken, sleep, vmBudget, vmPoll
	t.Cleanup(func() { accessToken, sleep, vmBudget, vmPoll = oldTok, oldSleep, oldBudget, oldPoll })
	vmBudget, vmPoll, sleep = 200*time.Millisecond, time.Millisecond, time.Sleep
	accessToken = func() (string, error) { return "tok", nil }
	prefix := "/projects/" + project + "/zones/" + zone + "/"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, ok := strings.CutPrefix(r.URL.Path, prefix)
		if !ok {
			http.NotFound(w, r)
			return
		}
		f.calls = append(f.calls, r.Method+" "+p)
		for name, vm := range f.vms {
			switch {
			case r.Method == "GET" && p == "instances/"+name:
				json.NewEncoder(w).Encode(map[string]any{"status": vm.status, "metadata": map[string]any{"fingerprint": vm.fp, "items": vm.items}})
				return
			case r.Method == "POST" && p == "instances/"+name+"/setMetadata":
				var b struct {
					Fingerprint string     `json:"fingerprint"`
					Items       []metaItem `json:"items"`
				}
				json.NewDecoder(r.Body).Decode(&b)
				if b.Fingerprint != vm.fp {
					http.Error(w, `{"error":{"code":412,"message":"fingerprint"}}`, 412)
					return
				}
				vm.items, vm.fp = b.Items, vm.fp+"x"
				io.WriteString(w, `{"name":"op-md","status":"DONE"}`)
				return
			case r.Method == "POST" && p == "instances/"+name+"/start":
				vm.status = "RUNNING"
				io.WriteString(w, `{"name":"op-start","status":"DONE"}`)
				return
			}
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("ORCHD_COMPUTE_URL", srv.URL)
}

// noCCHeadroom: both claude agents over their 5h window (a reset still in the future, unlike the
// fixed seed windows which usableUsage would otherwise treat as already reset to 0%), so the capacity
// check falls through to opencode.
func noCCHeadroom() map[string]AgentUsage {
	u := seedUsage()
	over := &UsageWindow{UsedPct: 90, ResetsAt: time.Now().Add(time.Hour)}
	u["claude/a12e00a7"] = AgentUsage{FiveHour: over}
	u["claude/b1c8ef41"] = AgentUsage{FiveHour: over}
	return u
}

func TestDispatchPendingVMRetriesBusyComputer(t *testing.T) {
	taskEnv(t, reg)
	t.Setenv("ORCHD_MODE", "sleep")
	fakeCad(t, noCCHeadroom(), 2)
	calls := fakeShell(t, map[string]string{
		"gh issue list": `[{"number":20,"title":"t20","body":"b20","createdAt":"2026-09-01T00:00:00Z","labels":[]}]`,
		"gh pr list":    `[]`,
	})
	f := &fakeComputeMulti{vms: map[string]*vmSim{
		"worker-spot": {status: "RUNNING", fp: "fp1"}, // busy: exit 5, excluded
		"worker-std": {status: "TERMINATED", fp: "fp2", items: []metaItem{
			{"worker-task", "old"}, {"startup-script", "#!/bin/bash\nworker-task"}}},
	}}
	f.install(t, "suggestorder-dev", "us-central1-a")
	code, m, errs := runTask(t, "dispatch", "--pending")
	if code != 0 {
		t.Fatalf("code %d: %s", code, errs)
	}
	if m["mode"] != "vm" {
		t.Fatalf("out %v", m)
	}
	dispatched, _ := m["dispatched"].([]any)
	if len(dispatched) != 1 {
		t.Fatalf("dispatched %v", dispatched)
	}
	d := dispatched[0].(map[string]any)
	if d["number"] != 20.0 || d["computer"] != "gce-std" {
		t.Errorf("dispatched entry %v", d)
	}
	if skipped, _ := m["skipped"].([]any); len(skipped) != 0 {
		t.Errorf("skipped %v", skipped)
	}
	var wipAdded, commentAdded bool
	for _, c := range *calls {
		line := strings.Join(c[1:], " ")
		if strings.Contains(line, "issue edit 20") && strings.Contains(line, "--add-label wip") {
			wipAdded = true
		}
		if strings.Contains(line, "issue comment 20") && strings.Contains(line, "dispatched to gce-std") {
			commentAdded = true
		}
	}
	if !wipAdded || !commentAdded {
		t.Errorf("gh calls: %v", *calls)
	}
	if !slices.Contains(f.calls, "GET instances/worker-spot") {
		t.Errorf("expected orchd to check the busy computer first: %v", f.calls)
	}
}

func TestDispatchPendingVMSkipsWhenAllBusy(t *testing.T) {
	taskEnv(t, reg)
	t.Setenv("ORCHD_MODE", "sleep")
	fakeCad(t, noCCHeadroom(), 2)
	calls := fakeShell(t, map[string]string{
		"gh issue list": `[{"number":21,"title":"t21","body":"","createdAt":"2026-09-01T00:00:00Z","labels":[]}]`,
		"gh pr list":    `[]`,
	})
	f := &fakeComputeMulti{vms: map[string]*vmSim{
		"worker-spot": {status: "RUNNING", fp: "fp1"},
		"worker-std":  {status: "RUNNING", fp: "fp2"},
	}}
	f.install(t, "suggestorder-dev", "us-central1-a")
	code, m, errs := runTask(t, "dispatch", "--pending")
	if code != 0 {
		t.Fatalf("code %d: %s", code, errs)
	}
	if deferred, _ := m["deferred"].([]any); len(deferred) != 1 || deferred[0] != 21.0 {
		t.Errorf("deferred %v", m["deferred"])
	}
	if skipped, _ := m["skipped"].([]any); len(skipped) != 0 { // it fit; only the VMs were busy
		t.Errorf("skipped %v", skipped)
	}
	if dispatched, _ := m["dispatched"].([]any); len(dispatched) != 0 {
		t.Errorf("dispatched %v", dispatched)
	}
	for _, c := range *calls {
		if strings.Contains(strings.Join(c[1:], " "), "--add-label wip") {
			t.Errorf("nothing dispatched: wip must not be added: %v", *calls)
		}
	}
}

func TestDispatchComment(t *testing.T) {
	at := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	body := dispatchComment("gce-spot", at)
	if body != "orchd: dispatched to gce-spot at 2026-09-28T12:00:00Z" {
		t.Fatalf("body %q", body)
	}
	if c, got, ok := lastDispatch([]string{"unrelated comment", body}); !ok || c != "gce-spot" || !got.Equal(at) {
		t.Errorf("parse: %s %v %v", c, got, ok)
	}
	if _, _, ok := lastDispatch([]string{"nothing here"}); ok {
		t.Error("should not match a comment without the marker")
	}
}

func TestReapStale(t *testing.T) {
	taskEnv(t, reg)
	now := time.Now()
	old := dispatchComment("gce-spot", now.Add(-90*time.Minute))
	fresh := dispatchComment("gce-std", now.Add(-5*time.Minute))
	calls := fakeShell(t, map[string]string{
		"gh issue view 7": fmt.Sprintf(`{"comments":[{"body":%q}]}`, old),
		"gh issue view 8": fmt.Sprintf(`{"comments":[{"body":%q}]}`, fresh),
	})
	issues := []ghIssue{
		{Number: 7, Labels: []label{{"ai"}, {"wip"}}}, // stuck: over reapAfter
		{Number: 8, Labels: []label{{"ai"}, {"wip"}}}, // still fresh: left alone
		{Number: 9, Labels: []label{{"ai"}}},          // pending, not wip: untouched
	}
	if err := reapStale("o/r", issues, now); err != nil {
		t.Fatalf("reapStale: %v", err)
	}
	var reaped7, touched8, commented7 bool
	for _, c := range *calls {
		line := strings.Join(c[1:], " ")
		if strings.Contains(line, "issue edit 7") && strings.Contains(line, "--remove-label wip") && strings.Contains(line, "--add-label ai-failed") {
			reaped7 = true
		}
		if strings.Contains(line, "issue edit 8") || strings.Contains(line, "issue comment 8") {
			touched8 = true
		}
		if strings.Contains(line, "issue comment 7") {
			commented7 = true
		}
	}
	if !reaped7 || touched8 || !commented7 {
		t.Errorf("calls %v", *calls)
	}
}

func TestIssueClassDefaults(t *testing.T) {
	pol := seedPolicy(t)
	if c := issueClass(ghIssue{Labels: []label{{"class:needs-db"}}}, pol); c != "needs-db" {
		t.Errorf("labeled class: %s", c)
	}
	if c := issueClass(ghIssue{Labels: []label{{"class:not-a-real-class"}}}, pol); c != "gate-heavy" {
		t.Errorf("unknown class label falls back to default: %s", c)
	}
	if c := issueClass(ghIssue{}, pol); c != "gate-heavy" {
		t.Errorf("no class label: %s", c)
	}
}

// ghIssueList pushes the milestone filter to gh so --limit is not spent on milestoned issues.
func TestGhIssueListFiltersMilestoneServerSide(t *testing.T) {
	taskEnv(t, reg)
	t.Setenv("ORCHD_MODE", "sleep")
	fakeCad(t, seedUsage(), 2)
	calls := fakeShell(t, map[string]string{"gh issue list": `[]`, "gh pr list": `[]`})
	runList(t, "issue", "list")
	runTask(t, "dispatch", "--pending")
	var searches []string
	for _, c := range *calls {
		if strings.Join(c[1:4], " ") != "gh issue list" {
			continue
		}
		if i := slices.Index(c, "--limit"); i < 0 || c[i+1] != "1000" {
			t.Errorf("limit: %v", c)
		}
		if i := slices.Index(c, "--search"); i >= 0 {
			searches = append(searches, c[i+1])
		}
	}
	if !slices.Equal(searches, []string{"no:milestone", "no:milestone sort:created-asc"}) {
		t.Errorf("searches %v calls %v", searches, *calls)
	}
}

// An issue whose worker already opened (or merged) a Closes #n PR has no wip, but is not pending.
func TestDispatchPendingExcludesIssueWithLivePR(t *testing.T) {
	taskEnv(t, reg)
	t.Setenv("ORCHD_MODE", "sleep")
	fakeCad(t, seedUsage(), 2)
	fakeShell(t, map[string]string{
		"gh issue list": `[
			{"number":40,"title":"t40","body":"","createdAt":"2026-09-01T00:00:00Z","labels":[]},
			{"number":41,"title":"t41","body":"","createdAt":"2026-09-02T00:00:00Z","labels":[]},
			{"number":42,"title":"t42","body":"","createdAt":"2026-09-03T00:00:00Z","labels":[]}]`,
		"gh pr list": `[{"url":"u/40","state":"OPEN","body":"Closes #40"},
			{"url":"u/41","state":"MERGED","body":"Fixes #41"},
			{"url":"u/42","state":"CLOSED","body":"Closes #42"}]`,
		"claude -p": `{"result":"ok"}`,
	})
	code, m, errs := runTask(t, "dispatch", "--pending")
	if code != 0 {
		t.Fatalf("code %d: %s", code, errs)
	}
	if issues, _ := m["issues"].([]any); len(issues) != 1 || issues[0] != 42.0 {
		t.Errorf("only #42 (its PR was closed unmerged) is pending: %v", m)
	}
}

// The dispatch comment goes first: if it fails, wip is never added (a wip issue without the comment
// would never be reaped) and the issue stays pending for the next tick.
func TestDispatchPendingCommentFailureAddsNoWip(t *testing.T) {
	taskEnv(t, reg)
	t.Setenv("ORCHD_MODE", "sleep")
	fakeCad(t, seedUsage(), 2)
	calls := fakeShell(t, map[string]string{
		"gh issue list":    `[{"number":43,"title":"t43","body":"","createdAt":"2026-09-01T00:00:00Z","labels":[]}]`,
		"gh pr list":       `[]`,
		"gh issue comment": "ERR",
	})
	if code, _, _ := runTask(t, "dispatch", "--pending"); code == 0 {
		t.Errorf("want non-zero exit when no issue could be claimed")
	}
	for _, c := range *calls {
		line := strings.Join(c[1:], " ")
		if strings.Contains(line, "--add-label wip") || strings.HasPrefix(line, "claude") {
			t.Errorf("comment failed: no wip and no claude call expected: %v", *calls)
		}
	}
}

func usableVM(fp string) *vmSim {
	return &vmSim{status: "TERMINATED", fp: fp, items: []metaItem{{"startup-script", "#!/bin/bash\nworker-task"}}}
}

// opencodeAt: no claude headroom and opencode's 5h window at pct (reservePct 15 -> limit 85).
func opencodeAt(pct float64) map[string]AgentUsage {
	u := noCCHeadroom()
	u["opencode/996c87ae"] = AgentUsage{FiveHour: &UsageWindow{UsedPct: pct, ResetsAt: time.Now().Add(time.Hour)}}
	return u
}

// An issue whose class does not fit is skipped; a later issue that fits is still dispatched.
func TestDispatchPendingVMSkipsUnfitIssue(t *testing.T) {
	taskEnv(t, reg)
	t.Setenv("ORCHD_MODE", "sleep")
	fakeCad(t, opencodeAt(80), 2) // needs-db (6) -> 86 > 85; light-edit (1) -> 81 fits
	calls := fakeShell(t, map[string]string{
		"gh issue list": `[
			{"number":50,"title":"t50","body":"","createdAt":"2026-09-01T00:00:00Z","labels":[{"name":"class:needs-db"}]},
			{"number":51,"title":"t51","body":"","createdAt":"2026-09-02T00:00:00Z","labels":[{"name":"class:light-edit"}]}]`,
		"gh pr list": `[]`,
	})
	f := &fakeComputeMulti{vms: map[string]*vmSim{"worker-spot": usableVM("fp1"), "worker-std": usableVM("fp2")}}
	f.install(t, "suggestorder-dev", "us-central1-a")
	code, m, errs := runTask(t, "dispatch", "--pending")
	if code != 0 {
		t.Fatalf("code %d: %s", code, errs)
	}
	skipped, _ := m["skipped"].([]any)
	if len(skipped) != 1 || skipped[0].(map[string]any)["number"] != 50.0 || skipped[0].(map[string]any)["reason"] == nil {
		t.Errorf("skipped %v", m["skipped"])
	}
	dispatched, _ := m["dispatched"].([]any)
	if len(dispatched) != 1 || dispatched[0].(map[string]any)["number"] != 51.0 {
		t.Errorf("dispatched %v", m["dispatched"])
	}
	if deferred, _ := m["deferred"].([]any); len(deferred) != 0 {
		t.Errorf("deferred %v", deferred)
	}
	for _, c := range *calls {
		if line := strings.Join(c[1:], " "); strings.Contains(line, "issue edit 50") || strings.Contains(line, "issue comment 50") {
			t.Errorf("#50 must be left untouched: %v", *calls)
		}
	}
}

// Each dispatch in a run counts against the agent's usage before the next issue is placed.
func TestDispatchPendingVMCountsEarlierDispatch(t *testing.T) {
	taskEnv(t, reg)
	t.Setenv("ORCHD_MODE", "sleep")
	fakeCad(t, opencodeAt(80), 2) // gate-heavy (4): 80+4 fits, then 84+4 = 88 > 85
	calls := fakeShell(t, map[string]string{
		"gh issue list": `[
			{"number":60,"title":"t60","body":"","createdAt":"2026-09-01T00:00:00Z","labels":[]},
			{"number":61,"title":"t61","body":"","createdAt":"2026-09-02T00:00:00Z","labels":[]}]`,
		"gh pr list": `[]`,
	})
	// Both VMs are free, so without the running total #61 would be placed on gce-std.
	f := &fakeComputeMulti{vms: map[string]*vmSim{"worker-spot": usableVM("fp1"), "worker-std": usableVM("fp2")}}
	f.install(t, "suggestorder-dev", "us-central1-a")
	code, m, errs := runTask(t, "dispatch", "--pending")
	if code != 0 {
		t.Fatalf("code %d: %s", code, errs)
	}
	dispatched, _ := m["dispatched"].([]any)
	if len(dispatched) != 1 || dispatched[0].(map[string]any)["number"] != 60.0 {
		t.Errorf("dispatched %v", m["dispatched"])
	}
	skipped, _ := m["skipped"].([]any)
	if len(skipped) != 1 || skipped[0].(map[string]any)["number"] != 61.0 || skipped[0].(map[string]any)["defer_until"] == nil {
		t.Errorf("skipped %v", m["skipped"])
	}
	for _, c := range *calls {
		if strings.Contains(strings.Join(c[1:], " "), "issue edit 61") {
			t.Errorf("#61 must not get wip: %v", *calls)
		}
	}
	if f.vms["worker-std"].status != "TERMINATED" {
		t.Errorf("worker-std must not be started")
	}
}

func TestWithEstimate(t *testing.T) {
	w := &UsageWindow{UsedPct: 10}
	out := withEstimate(AgentUsage{FiveHour: w}, 4)
	if out.FiveHour.UsedPct != 14 || out.SevenDay != nil || w.UsedPct != 10 {
		t.Errorf("out %+v, original %+v", out.FiveHour, w)
	}
}
