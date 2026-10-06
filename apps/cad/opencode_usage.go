package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const opencodeTimeout = 10 * time.Second

// opencodeUsageURL is OpenCode Go's usage endpoint. It is not in the public docs (found in the
// opencode source); verified 2026-09-28 to answer {"usage":{"rolling"|"weekly"|"monthly":
// {"status":"ok"|"rate-limited","percent":N,"resetsAt":RFC3339}}}. Tests point it at httptest.
var opencodeUsageURL = "https://opencode.ai/zen/go/v1/usage"

// opencodeAuth maps an opencode agent to its auth.json: <id> -> ~/.aienv/.store/<id>/opencode,
// default -> $XDG_DATA_HOME/opencode (else ~/.local/share/opencode).
func opencodeAuth(agent string) (string, error) {
	if !agentRe.MatchString(agent) { // keeps the id a single path element
		return "", errors.New("bad agent name")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	id := strings.TrimPrefix(agent, "opencode/")
	if id != "default" {
		return filepath.Join(home, ".aienv", ".store", id, "opencode", "auth.json"), nil
	}
	data := os.Getenv("XDG_DATA_HOME")
	if data == "" {
		data = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(data, "opencode", "auth.json"), nil
}

// opencodeKey decodes only the two key fields: "opencode-go" if present, else "opencode".
// The key is never logged or returned in an error.
func opencodeKey(file string) (string, error) {
	b, err := os.ReadFile(file)
	if err != nil {
		return "", fmt.Errorf("read auth.json: %w", errors.Unwrap(err)) // no path in the reason
	}
	type entry struct {
		Key string `json:"key"`
	}
	var f struct {
		Go *entry `json:"opencode-go"`
		OC *entry `json:"opencode"`
	}
	if json.Unmarshal(b, &f) != nil {
		return "", errors.New("parse auth.json failed")
	}
	for _, e := range []*entry{f.Go, f.OC} {
		if e != nil && e.Key != "" {
			return e.Key, nil
		}
	}
	return "", errors.New("no opencode key in auth.json")
}

// opencodeUsageFor maps rolling -> FiveHour, weekly -> SevenDay, monthly -> Monthly; a window with
// status "rate-limited" gets RateLimited so place skips it regardless of percent.
func opencodeUsageFor(agent string, every time.Duration) AgentUsage {
	file, err := opencodeAuth(agent)
	if err != nil {
		return AgentUsage{Error: err.Error()}
	}
	key, err := opencodeKey(file)
	if err != nil {
		return AgentUsage{Error: err.Error()}
	}
	ctx, cancel := context.WithTimeout(context.Background(), min(opencodeTimeout, every))
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, opencodeUsageURL, nil)
	if err != nil {
		return AgentUsage{Error: "bad usage url"}
	}
	req.Header.Set("Authorization", "Bearer "+key)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return AgentUsage{Error: "opencode usage: request failed"} // err may carry the URL only, but keep it terse
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return AgentUsage{Error: fmt.Sprintf("http %d", resp.StatusCode)}
	}
	type win struct {
		Status   string    `json:"status"`
		Percent  float64   `json:"percent"`
		ResetsAt time.Time `json:"resetsAt"`
	}
	var v struct {
		Usage struct{ Rolling, Weekly, Monthly *win } `json:"usage"`
	}
	if json.NewDecoder(resp.Body).Decode(&v) != nil {
		return AgentUsage{Error: "opencode usage: bad response"}
	}
	conv := func(w *win) *UsageWindow {
		if w == nil {
			return nil
		}
		return &UsageWindow{UsedPct: w.Percent, ResetsAt: w.ResetsAt.UTC(), RateLimited: w.Status == "rate-limited"}
	}
	return AgentUsage{
		FiveHour:  conv(v.Usage.Rolling),
		SevenDay:  conv(v.Usage.Weekly),
		Monthly:   conv(v.Usage.Monthly),
		FetchedAt: time.Now().UTC(),
	}
}
