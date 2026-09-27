package main

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Gate struct {
	MemoryMB  int     `json:"memoryMB"`
	CPUs      float64 `json:"cpus"`
	ReserveMB int     `json:"reserveMB"`
	MaxSlots  *int    `json:"maxSlots"`
}

type Policy struct {
	Version int  `json:"version"`
	Gate    Gate `json:"gate"`
	Review  struct {
		Reviewers          []string `json:"reviewers"`
		ExcludeImplementer bool     `json:"excludeImplementer"`
		RequireCI          bool     `json:"requireCI"`
	} `json:"review"`
	Providers struct {
		Allowed []string `json:"allowed"`
	} `json:"providers"`
	// Placement (ADR-0010). providers.allowed above still drives worker polling.
	Computers   map[string]Computer `json:"computers"`
	Agents      []string            `json:"agents"`      // e.g. "claude/default", "claude/3f9a1c0e"
	ClassAgents map[string][]string `json:"classAgents"` // class -> path.Match patterns over Agents
	Placement   struct {
		ReservePct float64            `json:"reservePct"` // usage headroom kept free per window
		EstPct     map[string]float64 `json:"estPct"`     // class -> estimated usage % one task consumes
	} `json:"placement"`
	Source string `json:"source"` // file path, or "builtin"
}

// Computer holds a placement target's static attributes (pricing, caps, limits; 0 limit = none).
type Computer struct {
	VCPUHourUSD  float64         `json:"vcpuHourUSD"`
	GiBHourUSD   float64         `json:"gibHourUSD"`
	MinBillSec   int             `json:"minBillSec"`
	Preemptible  bool            `json:"preemptible"`
	ColdStartSec int             `json:"coldStartSec"`
	Caps         map[string]bool `json:"caps"`
	MaxMemMB     int             `json:"maxMemMB"`
	MaxCPUs      float64         `json:"maxCpus"`
	MaxMin       int             `json:"maxMin"`
}

func defaultPolicy() Policy {
	p := Policy{Version: 1, Gate: Gate{MemoryMB: 1500, CPUs: 1, ReserveMB: 2048}, Source: "builtin"}
	p.Review.Reviewers = []string{"codex-bot", "codex-local", "claude-opus"}
	p.Review.ExcludeImplementer, p.Review.RequireCI = true, true
	p.Providers.Allowed = []string{"gce-spot", "cloud-run-jobs", "e2b", "local"}
	return p
}

var (
	polMu   sync.Mutex
	pol     = defaultPolicy()
	polPath string
	polMod  time.Time
)

// currentPolicy resolves CAD_POLICY > ./.agent/policy.json (if present) > built-in default,
// re-reading the file when its mtime changes. A bad file is logged and the last good policy kept.
func currentPolicy() Policy {
	polMu.Lock()
	defer polMu.Unlock()
	path := os.Getenv("CAD_POLICY")
	if path == "" {
		path = ".agent/policy.json"
		if _, err := os.Stat(path); err != nil {
			pol, polPath = defaultPolicy(), ""
			return pol
		}
	}
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	st, err := os.Stat(path)
	if err == nil && (path != polPath || !st.ModTime().Equal(polMod)) {
		var b []byte
		var p Policy
		if b, err = os.ReadFile(path); err == nil {
			if err = json.Unmarshal(b, &p); err == nil {
				p.Source = path
				pol, polPath, polMod = p, path, st.ModTime()
			}
		}
	}
	if err != nil {
		log.Printf("cad: policy %s: %v (serving %s)", path, err, pol.Source)
	}
	return pol
}

func init() {
	register("policy", func() (interface{}, error) { return currentPolicy(), nil })
}
