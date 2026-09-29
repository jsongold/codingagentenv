package main

// pick and dispatch: the Orchestrator's steps around place (skills/orchestrate). orchd shells out to
// gh / git / opencode / claude; shell and startBg are package vars so tests replace them.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"syscall"
)

// shell runs name in dir (""= CWD) and returns stdout; the error carries stderr.
var shell = func(dir, name string, args ...string) (string, error) {
	return shellCtx(context.Background(), dir, name, args...)
}

// shellCtx is shell that is killed when ctx ends.
func shellCtx(ctx context.Context, dir, name string, args ...string) (string, error) {
	c := exec.CommandContext(ctx, name, args...)
	c.Dir = dir
	var out, errb bytes.Buffer
	c.Stdout, c.Stderr = &out, &errb
	if err := c.Run(); err != nil {
		return "", fmt.Errorf("%s %s: %v: %s", name, strings.Join(args[:min(2, len(args))], " "), err, strings.TrimSpace(errb.String()))
	}
	return out.String(), nil
}

// startBg starts name detached in dir (own process group, stdout/stderr appended to log) and returns its pid.
var startBg = func(dir, log, name string, args ...string) (int, error) {
	f, err := os.OpenFile(log, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	c := exec.Command(name, args...)
	c.Dir, c.Stdout, c.Stderr = dir, f, f
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := c.Start(); err != nil {
		return 0, err
	}
	return c.Process.Pid, c.Process.Release()
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

// resolveNS: --repo/--path > the registry entry > the git toplevel of the CWD and its `gh repo view`.
func resolveNS(name, repo, path string) (NS, error) {
	ns, err := readNS(name)
	if err != nil {
		return ns, err
	}
	if repo != "" {
		ns.Repo = repo
	}
	if path != "" {
		ns.Path = path
	}
	if rest, ok := strings.CutPrefix(ns.Path, "~/"); ok {
		home, _ := os.UserHomeDir()
		ns.Path = filepath.Join(home, rest)
	}
	if ns.Path == "" {
		ns.Path, err = shell("", "git", "rev-parse", "--show-toplevel")
		ns.Path = strings.TrimSpace(ns.Path)
	}
	if err == nil && ns.Repo == "" {
		ns.Repo, err = shell(ns.Path, "gh", "repo", "view", "--json", "nameWithOwner", "-q", ".nameWithOwner")
		ns.Repo = strings.TrimSpace(ns.Repo)
	}
	return ns, err
}

type ghIssue struct {
	Number    int     `json:"number"`
	Title     string  `json:"title"`
	Body      string  `json:"body"`
	CreatedAt string  `json:"createdAt"`
	Labels    []label `json:"labels"`
}

type label struct {
	Name string `json:"name"`
}

// taskFlags parses the flags pick and dispatch share, plus extra string flags.
func taskFlags(name string, args []string, extra ...string) (map[string]*string, error) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	v := map[string]*string{"ns": fs.String("ns", "default", ""), "repo": fs.String("repo", "", ""), "path": fs.String("path", "", "")}
	for _, k := range extra {
		v[k] = fs.String(k, "", "")
	}
	if err := fs.Parse(args); err != nil || fs.NArg() > 0 {
		return nil, fmt.Errorf("%s: bad args %q", name, args)
	}
	if !nsRe.MatchString(*v["ns"]) {
		return nil, fmt.Errorf("--ns %q: want %s", *v["ns"], nsRe)
	}
	return v, nil
}

// pickCmd claims the oldest open issue labeled ai without wip or ai-failed (adds wip) and prints it with the classes to choose from.
func pickCmd(args []string, w io.Writer) (int, error) {
	f, err := taskFlags("pick", args)
	if err != nil {
		return 2, err
	}
	pol, err := loadPolicy()
	if err != nil {
		return 1, err
	}
	ns, err := resolveNS(*f["ns"], *f["repo"], *f["path"])
	if err != nil {
		return 1, err
	}
	out, err := shell("", "gh", "issue", "list", "--repo", ns.Repo, "--label", "ai", "--state", "open", "--search", "-label:wip -label:ai-failed sort:created-asc", "--limit", "200", "--json", "number,title,body,createdAt,labels")
	var issues []ghIssue
	if err == nil {
		err = json.Unmarshal([]byte(out), &issues)
	}
	if err != nil {
		return 1, err
	}
	issues = slices.DeleteFunc(issues, func(i ghIssue) bool {
		return slices.Contains(i.Labels, label{"wip"}) || slices.Contains(i.Labels, label{"ai-failed"})
	})
	if len(issues) == 0 {
		return 0, printJSON(w, map[string]any{"none": true, "reason": "no open issue labeled ai without wip or ai-failed"})
	}
	i := slices.MinFunc(issues, func(a, b ghIssue) int { return strings.Compare(a.CreatedAt, b.CreatedAt) })
	if _, err := shell("", "gh", "issue", "edit", strconv.Itoa(i.Number), "--repo", ns.Repo, "--add-label", "wip"); err != nil {
		return 1, err
	}
	classes := []map[string]string{}
	for _, name := range slices.Sorted(maps.Keys(pol.Classes)) {
		classes = append(classes, map[string]string{"name": name, "criteria": pol.Classes[name].Criteria})
	}
	return 0, printJSON(w, map[string]any{"issue": map[string]any{"n": i.Number, "title": i.Title, "body": i.Body}, "classes": classes})
}

func taskPrompt(repo string, n int, title, body, branch string) string {
	if strings.TrimSpace(body) == "" {
		body = "（なし）"
	}
	return fmt.Sprintf(`repo %s の Issue #%d を解決して PR を出す。
タイトル: %s

## Issue 本文
%s

## 手順
- 作業ブランチ: %s（無ければ origin/main から作る）
- 変更は Issue の範囲だけ。repo の CLAUDE.md / README にあるテストを通す
- commit → push → `+"`gh pr create --base main`"+`。PR 本文に `+"`Closes #%d`"+` を必ず入れる
- 最後に PR の URL だけを 1 行で出力する
`, repo, n, title, strings.TrimSpace(body), branch, n)
}

// dispatchCmd hands issue n (or, with --prompt-file, a fixed prompt) to the placement's runner
// (the JSON place printed; "-" = stdin). --prompt-file only supports runner.mode "cloud" and reads
// the namespace from the registry only (readNS): the sleep-timer caller (cad-2) has no git/gh checkout.
func dispatchCmd(args []string, stdin io.Reader, w io.Writer) (int, error) {
	f, err := taskFlags("dispatch", args, "issue", "placement", "prompt-file")
	if err != nil {
		return 2, err
	}
	if (*f["issue"] == "") == (*f["prompt-file"] == "") {
		return 2, fmt.Errorf("dispatch: want exactly one of --issue or --prompt-file")
	}
	raw := []byte(*f["placement"])
	if *f["placement"] == "-" {
		if raw, err = io.ReadAll(stdin); err != nil {
			return 1, err
		}
	}
	var pl struct {
		Runner Runner `json:"runner"`
	}
	if err := json.Unmarshal(raw, &pl); err != nil {
		return 2, fmt.Errorf("--placement: %v (want the JSON orchd place printed)", err)
	}
	rn := pl.Runner

	if *f["prompt-file"] != "" {
		if rn.Mode != "cloud" {
			return 2, fmt.Errorf("--prompt-file: runner.mode %q: only cloud is supported", rn.Mode)
		}
		prompt, err := os.ReadFile(*f["prompt-file"])
		if err != nil {
			return 1, err
		}
		ns, err := readNS(*f["ns"])
		if err != nil {
			return 1, err
		}
		if cloudSession(ns) == "" {
			return 2, fmt.Errorf("no cloud session for ns %s: set CLAUDE_CLOUD_SESSION or cloudWorkerSession in %s (create one with `claude --cloud`)", *f["ns"], namespacesFile())
		}
		return cloudSend(cloudSession(ns), string(prompt), w)
	}

	n, err := strconv.Atoi(*f["issue"])
	if err != nil || n <= 0 {
		return 2, fmt.Errorf("--issue %q: want an issue number", *f["issue"])
	}
	if rn.Mode != "subagent" && rn.Mode != "process" && rn.Mode != "cloud" && rn.Mode != "vm" {
		return 2, fmt.Errorf("--placement: runner.mode %q: want subagent, process, cloud or vm", rn.Mode)
	}
	ns, err := resolveNS(*f["ns"], *f["repo"], *f["path"])
	if err != nil {
		return 1, err
	}
	if rn.Mode == "vm" { // the worker image reads the issue itself (deploy/worker-run.sh)
		return dispatchVM(rn, ns.Repo, n, w)
	}
	if rn.Mode == "cloud" && cloudSession(ns) == "" {
		return 2, fmt.Errorf("no cloud session for ns %s: set CLAUDE_CLOUD_SESSION or cloudWorkerSession in %s (create one with `claude --cloud`)", *f["ns"], namespacesFile())
	}
	out, err := shell("", "gh", "issue", "view", strconv.Itoa(n), "--repo", ns.Repo, "--json", "title,body")
	var is ghIssue
	if err == nil {
		err = json.Unmarshal([]byte(out), &is)
	}
	if err != nil {
		return 1, err
	}
	branch := "task/" + strconv.Itoa(n)
	prompt := taskPrompt(ns.Repo, n, is.Title, is.Body, branch)

	if rn.Mode == "cloud" {
		return cloudSend(cloudSession(ns), prompt, w)
	}
	// The worktree sits next to the NS repo so the directory-based aienv bindings of its parent apply.
	wt := filepath.Join(filepath.Dir(ns.Path), filepath.Base(ns.Path)+"-task-"+strconv.Itoa(n))
	if err := taskWorktree(ns.Path, wt, branch); err != nil {
		return 1, err
	}
	if rn.Mode == "subagent" {
		return 0, printJSON(w, map[string]any{"runner": "subagent", "worktree": wt, "prompt": prompt})
	}
	cmdline := rn.Cmd
	if cmdline == "" {
		cmdline = "opencode run --model {model}"
	}
	argv := strings.Fields(strings.ReplaceAll(cmdline, "{model}", rn.Model))
	if err := os.MkdirAll(stateDir(), 0o755); err != nil {
		return 1, err
	}
	log := filepath.Join(stateDir(), "task-"+strconv.Itoa(n)+".log")
	pid, err := startBg(wt, log, argv[0], append(argv[1:], prompt)...)
	if err != nil {
		return 1, errors.Join(fmt.Errorf("start %s", argv[0]), err)
	}
	return 0, printJSON(w, map[string]any{"started": true, "worktree": wt, "pid": pid, "log": log})
}

// cloudSession: env CLAUDE_CLOUD_SESSION > the namespace registry's cloudWorkerSession.
func cloudSession(ns NS) string {
	if s := os.Getenv("CLAUDE_CLOUD_SESSION"); s != "" {
		return s
	}
	return ns.CloudWorkerSession
}

// cloudSend runs prompt on the cloud worker session (dispatch's runner mode "cloud") and writes its
// raw JSON output to w.
func cloudSend(session, prompt string, w io.Writer) (int, error) {
	out, err := shell("", "claude", "-p", prompt, "--cloud", session, "--output-format", "json")
	if err != nil {
		return 1, err
	}
	_, err = io.WriteString(w, out)
	return 0, err
}

// statusCmd reports the PR whose body says "Closes #n" (open first, then merged/closed) and, with
// --pid, whether that dispatched process is still running. One quick gh call, for supervisors to poll.
func statusCmd(args []string, w io.Writer) (int, error) {
	f, err := taskFlags("status", args, "issue", "pid")
	if err != nil {
		return 2, err
	}
	n, err := strconv.Atoi(*f["issue"])
	if err != nil || n <= 0 {
		return 2, fmt.Errorf("--issue %q: want an issue number", *f["issue"])
	}
	ns, err := resolveNS(*f["ns"], *f["repo"], *f["path"])
	if err != nil {
		return 1, err
	}
	out, err := shell("", "gh", "pr", "list", "--repo", ns.Repo, "--state", "all", "--search", fmt.Sprintf("Closes #%d", n), "--json", "url,state,body")
	var prs []struct{ URL, State, Body string }
	if err == nil {
		err = json.Unmarshal([]byte(out), &prs)
	}
	if err != nil {
		return 1, err
	}
	closes := regexp.MustCompile(fmt.Sprintf(`(?i)\b(close[sd]?|fix(e[sd])?|resolve[sd]?) #%d\b`, n))
	res := map[string]any{"issue": n, "pr": nil, "state": nil}
	for _, p := range prs { // gh lists newest first; an open PR wins over older closed ones
		if closes.MatchString(p.Body) && (res["pr"] == nil || p.State == "OPEN") {
			res["pr"], res["state"] = p.URL, p.State
		}
	}
	if *f["pid"] != "" {
		pid, err := strconv.Atoi(*f["pid"])
		if err != nil {
			return 2, fmt.Errorf("--pid %q: want a number", *f["pid"])
		}
		res["running"] = syscall.Kill(pid, 0) == nil
	}
	return 0, printJSON(w, res)
}

// taskWorktree creates wt on a new branch off origin/main, or reuses what a failed or interrupted
// earlier dispatch of the same issue left: the worktree itself, or just its branch.
func taskWorktree(repo, wt, branch string) error {
	if _, err := os.Stat(wt); err == nil {
		return nil
	}
	git := func(a ...string) error { _, err := shell("", "git", append([]string{"-C", repo}, a...)...); return err }
	if err := git("fetch", "-q", "origin"); err != nil {
		return err
	}
	if git("rev-parse", "-q", "--verify", "refs/heads/"+branch) == nil {
		return git("worktree", "add", "-q", wt, branch)
	}
	return git("worktree", "add", "-q", "-b", branch, wt, "origin/main")
}
