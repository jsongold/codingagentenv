package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

type UsageWindow struct {
	UsedPct  float64   `json:"usedPct"`
	ResetsAt time.Time `json:"resetsAt"`
}

// AgentUsage is one agent's subscription usage (ADR-0010 window filter). Error is set when the
// collector could not refresh it; Stale when the value it read is older than 2 intervals.
type AgentUsage struct {
	FiveHour  *UsageWindow `json:"fiveHour"` // nil (JSON null): the account has no such window
	SevenDay  *UsageWindow `json:"sevenDay"`
	FetchedAt time.Time    `json:"fetchedAt,omitzero"`
	Stale     bool         `json:"stale,omitempty"`
	Error     string       `json:"error,omitempty"`
}

// UsageMap is published on the "usage" topic. fetchedAt is excluded from the change key
// (like capacity's collectedAt) so a refresh with the same numbers emits no event.
type UsageMap map[string]AgentUsage

func (m UsageMap) changeKey() interface{} {
	k := make(map[string]AgentUsage, len(m))
	for a, u := range m {
		u.FetchedAt = time.Time{}
		k[a] = u
	}
	return k
}

const usageTimeout = 30 * time.Second

func usageEvery() time.Duration {
	if v := os.Getenv("CAD_USAGE_EVERY"); v != "" {
		d, err := time.ParseDuration(v)
		if err == nil && d > 0 {
			return d
		}
		log.Printf("cad: ignoring invalid CAD_USAGE_EVERY=%q", v)
	}
	return time.Minute
}

// usageStore maps a claude agent to its config dir ("" = default, no CLAUDE_CONFIG_DIR) and .claude.json.
func usageStore(agent string) (dir, file string, err error) {
	if !agentRe.MatchString(agent) { // keeps the id a single path element
		return "", "", errors.New("bad agent name")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", err
	}
	id := strings.TrimPrefix(agent, "claude/")
	if id == "default" {
		return "", filepath.Join(home, ".claude.json"), nil
	}
	dir = filepath.Join(home, ".aienv", ".store", id)
	return dir, filepath.Join(dir, ".claude.json"), nil
}

// claudeBin: CAD_CLAUDE_BIN > ~/.local/bin/claude > PATH, skipping the aienv shim (~/.aienv/bin).
func claudeBin() (string, error) {
	home, _ := os.UserHomeDir()
	return findBin("claude", "CAD_CLAUDE_BIN", filepath.Join(home, ".local", "bin", "claude"))
}

// findBin: $env > preferred > PATH, skipping the aienv shim dir (~/.aienv/bin), whose wrapper
// would pick an account itself.
func findBin(name, env, preferred string) (string, error) {
	if b := os.Getenv(env); b != "" {
		return b, nil
	}
	if isExecutable(preferred) {
		return preferred, nil
	}
	home, _ := os.UserHomeDir()
	shim := filepath.Join(home, ".aienv", "bin")
	for _, d := range filepath.SplitList(os.Getenv("PATH")) {
		if p := filepath.Join(d, name); filepath.Clean(d) != shim && isExecutable(p) {
			return p, nil
		}
	}
	return "", fmt.Errorf("%s binary not found (set %s)", name, env)
}

// runClaudeUsage runs `claude -p /usage`, which refreshes .cachedUsageUtilization in the store's
// .claude.json without a model request. Output is discarded. Tests replace usageRunner.
func runClaudeUsage(ctx context.Context, configDir string) error {
	bin, err := claudeBin()
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, bin, "-p", "/usage", "--output-format", "stream-json", "--verbose")
	cmd.Dir = os.TempDir() // keep the project settings/hooks of cad's cwd out of it
	for _, kv := range os.Environ() {
		switch k, _, _ := strings.Cut(kv, "="); k {
		case "ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "CLAUDE_CODE_OAUTH_TOKEN", "CLAUDE_CONFIG_DIR":
		default:
			cmd.Env = append(cmd.Env, kv)
		}
	}
	if configDir != "" {
		cmd.Env = append(cmd.Env, "CLAUDE_CONFIG_DIR="+configDir)
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = workerKillGrace
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("claude -p /usage: %w", err) // exit status only; stderr is not kept
	}
	return nil
}

var usageRunner = runClaudeUsage

// readUsage decodes only .cachedUsageUtilization; the rest of the file (account data) is never kept.
func readUsage(file string) (AgentUsage, error) {
	b, err := os.ReadFile(file)
	if err != nil {
		return AgentUsage{}, fmt.Errorf("read .claude.json: %w", errors.Unwrap(err)) // no path in the reason
	}
	type win struct {
		Utilization float64   `json:"utilization"`
		ResetsAt    time.Time `json:"resets_at"`
	}
	var f struct {
		C *struct {
			FetchedAtMs int64 `json:"fetchedAtMs"`
			Utilization struct {
				FiveHour *win `json:"five_hour"`
				SevenDay *win `json:"seven_day"`
			} `json:"utilization"`
		} `json:"cachedUsageUtilization"`
	}
	if err := json.Unmarshal(b, &f); err != nil {
		return AgentUsage{}, errors.New("parse .claude.json failed")
	}
	if f.C == nil {
		return AgentUsage{}, errors.New("cachedUsageUtilization missing")
	}
	conv := func(w *win) *UsageWindow {
		if w == nil {
			return nil
		}
		return &UsageWindow{w.Utilization, w.ResetsAt}
	}
	u := f.C.Utilization
	return AgentUsage{
		FiveHour:  conv(u.FiveHour),
		SevenDay:  conv(u.SevenDay),
		FetchedAt: time.UnixMilli(f.C.FetchedAtMs).UTC(),
	}, nil
}

// collectUsage refreshes every claude/* and codex/* agent concurrently, so the total is one
// command's time. Other services are left out (place reports them as "usage unknown").
func collectUsage(agents []string, every time.Duration) UsageMap {
	out := UsageMap{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, a := range agents {
		f := usageFor
		switch {
		case strings.HasPrefix(a, "claude/"):
		case strings.HasPrefix(a, "codex/"):
			f = codexUsageFor
		default:
			continue
		}
		wg.Go(func() {
			u := f(a, every)
			mu.Lock()
			out[a] = u
			mu.Unlock()
		})
	}
	wg.Wait()
	return out
}

// usageFor keeps a failed agent with Error set (and the last cached numbers, if the file has them).
func usageFor(agent string, every time.Duration) AgentUsage {
	dir, file, err := usageStore(agent)
	if err != nil {
		return AgentUsage{Error: err.Error()}
	}
	ctx, cancel := context.WithTimeout(context.Background(), min(usageTimeout, every))
	defer cancel()
	runErr := usageRunner(ctx, dir)
	u, err := readUsage(file)
	switch {
	case runErr != nil:
		u.Error = runErr.Error()
	case err != nil:
		u.Error = err.Error()
	}
	u.Stale = u.Error == "" && time.Since(u.FetchedAt) > 2*every
	return u
}

// usableUsage turns the published usage into what place filters on: agents with Error or Stale
// are dropped (place reports them as "usage unknown") and windows whose reset has passed count as 0%.
// A nil window stays nil (place does not filter on it).
func usableUsage(m UsageMap, now time.Time) map[string]AgentUsage {
	out := map[string]AgentUsage{}
	for a, u := range m {
		if u.Error != "" || u.Stale {
			continue
		}
		for _, w := range []**UsageWindow{&u.FiveHour, &u.SevenDay} {
			if *w != nil && !(*w).ResetsAt.After(now) {
				*w = &UsageWindow{0, (*w).ResetsAt} // copy: the published value is shared
			}
		}
		out[a] = u
	}
	return out
}

func init() {
	every := usageEvery()
	register("usage", every, func() (interface{}, error) { return collectUsage(currentPolicy().Agents, every), nil })
}
