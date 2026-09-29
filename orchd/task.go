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

// shellIn runs name with stdin (nil = none) and returns stdout; the error carries stderr. dispatch uses it.
var shellIn = func(stdin io.Reader, name string, args ...string) (string, error) {
	c := exec.Command(name, args...)
	var errb bytes.Buffer
	c.Stdin, c.Stderr = stdin, &errb
	out, err := c.Output()
	if err != nil {
		return "", fmt.Errorf("%s: %v: %s", name, err, strings.TrimSpace(errb.String()))
	}
	return string(out), nil
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

// dispatchCmd runs the placement's runner (the JSON place printed; "-" = stdin): each {key} in runner.cmd
// becomes the runner record's same-named string field, runner.stdin (a path) is piped in, and the command's
// stdout is printed. No branching on runner.mode: what differs between destinations lives in the record.
func dispatchCmd(args []string, stdin io.Reader, w io.Writer) (int, error) {
	fs := flag.NewFlagSet("dispatch", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	placement := fs.String("placement", "", "")
	if err := fs.Parse(args); err != nil || fs.NArg() > 0 || *placement == "" {
		return 2, fmt.Errorf("dispatch: bad args %q", args)
	}
	raw := []byte(*placement)
	if *placement == "-" {
		var err error
		if raw, err = io.ReadAll(stdin); err != nil {
			return 1, err
		}
	}
	var pl struct {
		Runner json.RawMessage `json:"runner"`
	}
	err := json.Unmarshal(raw, &pl)
	if err != nil {
		return 2, fmt.Errorf("--placement: %v (want the JSON orchd place printed)", err)
	}
	if len(pl.Runner) == 0 { // "runner" key missing entirely: same as runner {}
		pl.Runner = []byte("{}")
	}
	var rn Runner
	var rec map[string]any
	if err := errors.Join(json.Unmarshal(pl.Runner, &rn), json.Unmarshal(pl.Runner, &rec)); err != nil {
		return 2, fmt.Errorf("--placement: %v (want the JSON orchd place printed)", err)
	}
	if len(rec) == 0 { // nothing was placed (orchd place: runner {}): nothing to run
		return 0, printJSON(w, map[string]any{})
	}
	if len(rn.Cmd) == 0 {
		return 2, errors.New("--placement: runner has no cmd")
	}
	argv := make([]string, len(rn.Cmd))
	for i := range rn.Cmd {
		argv[i] = cmdKey.ReplaceAllStringFunc(rn.Cmd[i], func(k string) string {
			v, ok := rec[k[1:len(k)-1]].(string)
			if !ok || v == "" {
				err = errors.Join(err, fmt.Errorf("runner.cmd: %s has no value in the runner record", k))
			}
			return v
		})
	}
	if err != nil {
		return 2, err
	}
	var in io.Reader
	if rn.Stdin != "" {
		f, err := os.Open(rn.Stdin)
		if err != nil {
			return 1, err
		}
		defer f.Close()
		in = f
	}
	out, err := shellIn(in, argv[0], argv[1:]...)
	if err != nil {
		return 1, err
	}
	_, err = io.WriteString(w, out)
	return 0, err
}

var cmdKey = regexp.MustCompile(`\{[A-Za-z0-9_]+\}`)

// cloudSession: env CLAUDE_CLOUD_SESSION > the namespace registry's cloudWorkerSession.
func cloudSession(ns NS) string {
	if s := os.Getenv("CLAUDE_CLOUD_SESSION"); s != "" {
		return s
	}
	return ns.CloudWorkerSession
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
