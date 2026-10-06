package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const codexTimeout = 20 * time.Second

// codexWin is one rate-limit window as the app-server reports it (resetsAt: unix seconds).
type codexWin struct {
	UsedPercent        float64 `json:"usedPercent"`
	WindowDurationMins int     `json:"windowDurationMins"`
	ResetsAt           int64   `json:"resetsAt"`
}

// codexHome maps a codex agent to its CODEX_HOME ("" = default, no CODEX_HOME) and the dir it resolves to.
func codexHome(agent string) (env, dir string, err error) {
	if !agentRe.MatchString(agent) { // keeps the id a single path element
		return "", "", errors.New("bad agent name")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", err
	}
	id := strings.TrimPrefix(agent, "codex/")
	if id == "default" {
		return "", filepath.Join(home, ".codex"), nil
	}
	dir = filepath.Join(home, ".aienv", ".store", id)
	return dir, dir, nil
}

// runCodexUsage starts `codex app-server` for one CODEX_HOME and asks account/rateLimits/read
// (no model request, ~0.5s). Tests replace codexRunner.
func runCodexUsage(ctx context.Context, home string) ([]codexWin, error) {
	bin, err := findBin("codex", "CAD_CODEX_BIN", "/opt/homebrew/bin/codex")
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "app-server")
	cmd.Dir = os.TempDir()
	for _, kv := range os.Environ() {
		switch k, _, _ := strings.Cut(kv, "="); k {
		case "OPENAI_API_KEY", "CODEX_ACCESS_TOKEN", "CODEX_HOME":
		default:
			cmd.Env = append(cmd.Env, kv)
		}
	}
	if home != "" {
		cmd.Env = append(cmd.Env, "CODEX_HOME="+home)
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = workerKillGrace
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("codex app-server: %w", err)
	}
	defer cmd.Wait()
	defer cancel() // runs before Wait: kills the server once we have the answer
	return codexRateLimits(ctx, in, out)
}

var codexRunner = runCodexUsage

// codexRateLimits speaks the app-server's newline-delimited JSON-RPC: initialize, initialized,
// account/rateLimits/read. Notifications and other ids are skipped; only the windows are decoded.
func codexRateLimits(ctx context.Context, w io.Writer, r io.Reader) ([]codexWin, error) {
	type result struct {
		wins []codexWin
		err  error
	}
	done := make(chan result, 1)
	go func() {
		wins, err := codexExchange(w, r)
		done <- result{wins, err}
	}()
	select {
	case res := <-done:
		return res.wins, res.err
	case <-ctx.Done():
		return nil, errors.New("codex app-server: timed out")
	}
}

func codexExchange(w io.Writer, r io.Reader) ([]codexWin, error) {
	send := func(m string) error { _, err := io.WriteString(w, m+"\n"); return err }
	sc := bufio.NewScanner(r)
	sc.Buffer(nil, 1<<20)
	await := func(id int) (json.RawMessage, error) {
		for sc.Scan() {
			var m struct {
				ID     *int            `json:"id"`
				Result json.RawMessage `json:"result"`
				Error  *struct {
					Code int `json:"code"`
				} `json:"error"`
			}
			if json.Unmarshal(sc.Bytes(), &m) != nil || m.ID == nil || *m.ID != id {
				continue
			}
			if m.Error != nil {
				return nil, fmt.Errorf("codex app-server: request %d failed (code %d)", id, m.Error.Code)
			}
			return m.Result, nil
		}
		return nil, errors.New("codex app-server: exited before answering")
	}
	if err := send(`{"id":1,"method":"initialize","params":{"clientInfo":{"name":"cad","version":"0.1.0"}}}`); err != nil {
		return nil, err
	}
	if _, err := await(1); err != nil {
		return nil, err
	}
	if err := send(`{"method":"initialized"}`); err != nil {
		return nil, err
	}
	if err := send(`{"id":2,"method":"account/rateLimits/read","params":{"excludeResetCreditDetails":true}}`); err != nil {
		return nil, err
	}
	res, err := await(2)
	if err != nil {
		return nil, err
	}
	type snap struct{ Primary, Secondary *codexWin }
	var v struct {
		RateLimits snap            `json:"rateLimits"`
		ByLimitID  map[string]snap `json:"rateLimitsByLimitId"`
	}
	if err := json.Unmarshal(res, &v); err != nil {
		return nil, errors.New("codex app-server: bad rateLimits")
	}
	if len(v.ByLimitID) == 0 {
		v.ByLimitID = map[string]snap{"": v.RateLimits}
	}
	var wins []codexWin
	for _, s := range v.ByLimitID {
		for _, w := range []*codexWin{s.Primary, s.Secondary} {
			if w != nil {
				wins = append(wins, *w)
			}
		}
	}
	return wins, nil
}

// codexWindows maps windows by length: 300 min -> FiveHour, 10080 min -> SevenDay; other lengths
// are ignored and a window no limit reports stays nil (e.g. plus accounts have no 5h window).
// Several limits with the same window length: the highest usedPercent wins (conservative).
func codexWindows(wins []codexWin) AgentUsage {
	var u AgentUsage
	for _, w := range wins {
		var dst **UsageWindow
		switch w.WindowDurationMins {
		case 300:
			dst = &u.FiveHour
		case 10080:
			dst = &u.SevenDay
		default:
			continue
		}
		if *dst == nil || w.UsedPercent > (*dst).UsedPct {
			*dst = &UsageWindow{UsedPct: w.UsedPercent, ResetsAt: time.Unix(w.ResetsAt, 0).UTC()}
		}
	}
	return u
}

// codexRollout is the fallback: the last token_count event of the newest session rollout.
// ponytail: reads the whole rollout file; tail-read if rollouts grow to hundreds of MB.
func codexRollout(dir string) (AgentUsage, error) {
	var newest string
	var newestMod time.Time
	filepath.WalkDir(filepath.Join(dir, "sessions"), func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasPrefix(d.Name(), "rollout-") || !strings.HasSuffix(d.Name(), ".jsonl") {
			return nil
		}
		if fi, err := d.Info(); err == nil && fi.ModTime().After(newestMod) {
			newest, newestMod = p, fi.ModTime()
		}
		return nil
	})
	if newest == "" {
		return AgentUsage{}, errors.New("no codex session rollout")
	}
	b, err := os.ReadFile(newest)
	if err != nil {
		return AgentUsage{}, errors.New("read codex rollout failed")
	}
	type win struct {
		UsedPercent   float64 `json:"used_percent"`
		WindowMinutes int     `json:"window_minutes"`
		ResetsAt      int64   `json:"resets_at"`
	}
	lines := bytes.Split(b, []byte("\n"))
	for i := len(lines) - 1; i >= 0; i-- {
		if !bytes.Contains(lines[i], []byte(`"token_count"`)) {
			continue
		}
		var l struct {
			Payload struct {
				Type       string `json:"type"`
				RateLimits *struct {
					Primary, Secondary *win
				} `json:"rate_limits"`
			} `json:"payload"`
		}
		if json.Unmarshal(lines[i], &l) != nil || l.Payload.Type != "token_count" || l.Payload.RateLimits == nil {
			continue
		}
		var wins []codexWin
		for _, w := range []*win{l.Payload.RateLimits.Primary, l.Payload.RateLimits.Secondary} {
			if w != nil {
				wins = append(wins, codexWin{w.UsedPercent, w.WindowMinutes, w.ResetsAt})
			}
		}
		u := codexWindows(wins)
		u.FetchedAt = newestMod.UTC()
		return u, nil
	}
	return AgentUsage{}, errors.New("no rate limits in codex rollout")
}

// codexUsageFor asks the app-server; if that fails it falls back to the newest rollout (Stale,
// since the numbers are from the last session) and only reports Error when both fail.
func codexUsageFor(agent string, every time.Duration) AgentUsage {
	env, dir, err := codexHome(agent)
	if err != nil {
		return AgentUsage{Error: err.Error()}
	}
	ctx, cancel := context.WithTimeout(context.Background(), min(codexTimeout, every))
	defer cancel()
	wins, runErr := codexRunner(ctx, env)
	if runErr == nil {
		u := codexWindows(wins)
		u.FetchedAt = time.Now().UTC()
		return u
	}
	u, err := codexRollout(dir)
	if err != nil {
		return AgentUsage{Error: runErr.Error()}
	}
	u.Stale = true
	return u
}
