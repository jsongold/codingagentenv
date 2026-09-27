package main

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type UsageWindow struct {
	UsedPct  float64   `json:"usedPct"`
	ResetsAt time.Time `json:"resetsAt"`
}

// AgentUsage is one agent's subscription usage (ADR-0010 window filter).
type AgentUsage struct {
	FiveHour UsageWindow `json:"fiveHour"`
	SevenDay UsageWindow `json:"sevenDay"`
}

var (
	useMu   sync.Mutex
	use     = map[string]AgentUsage{}
	usePath string
	useMod  time.Time
)

// parseUsage reads {"<agent>": AgentUsage}; keys starting with "_" (e.g. "_provisional") are ignored.
func parseUsage(b []byte) (map[string]AgentUsage, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, err
	}
	u := map[string]AgentUsage{}
	for k, v := range raw {
		if strings.HasPrefix(k, "_") {
			continue
		}
		var a AgentUsage
		if err := json.Unmarshal(v, &a); err != nil {
			return nil, err
		}
		u[k] = a
	}
	return u, nil
}

// currentUsage resolves CAD_USAGE > ./.agent/usage.json (if present) > empty (all unknown),
// re-reading on mtime change like currentPolicy. A bad file is logged and the last good one kept.
// ponytail: .agent/usage.json is provisional hand-written data; a statusline writer will replace it.
func currentUsage() map[string]AgentUsage {
	useMu.Lock()
	defer useMu.Unlock()
	path := os.Getenv("CAD_USAGE")
	if path == "" {
		path = ".agent/usage.json"
		if _, err := os.Stat(path); err != nil {
			use, usePath = map[string]AgentUsage{}, ""
			return use
		}
	}
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	st, err := os.Stat(path)
	if err == nil && (path != usePath || !st.ModTime().Equal(useMod)) {
		var b []byte
		var u map[string]AgentUsage
		if b, err = os.ReadFile(path); err == nil {
			if u, err = parseUsage(b); err == nil {
				use, usePath, useMod = u, path, st.ModTime()
			}
		}
	}
	if err != nil {
		log.Printf("cad: usage %s: %v (serving last good)", path, err)
	}
	return use
}
