package main

import (
	"encoding/json"
	"fmt"
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
	Computers map[string]Computer `json:"computers"` // records only; place reads just the names
	Agents    []string            `json:"agents"`    // e.g. "claude/default", "claude/3f9a1c0e"
	Rules     []Rule              `json:"rules"`     // ordered decision list; first match wins
	Placement struct {
		ReservePct float64 `json:"reservePct"` // usage headroom kept free per window
	} `json:"placement"`
	Classes map[string]Class  `json:"classes"` // the task classes place accepts (the Orchestrator classifies by criteria)
	Runners map[string]Runner `json:"runners"` // service -> how the Orchestrator launches it (returned by place)
	Collect struct {
		Usage struct {
			Every string `json:"every"` // time.ParseDuration; CAD_USAGE_EVERY overrides
		} `json:"usage"`
	} `json:"collect"`
	Source string `json:"source"` // file path, or "builtin"
}

// Class: Criteria tells the Orchestrator when to pick it; EstPct is the usage % one task consumes.
type Class struct {
	Criteria string  `json:"criteria"`
	EstPct   float64 `json:"estPct"`
}

// Runner: "subagent" (a Task subagent of the Orchestrator) or "process" (run Cmd; {model} is Model).
type Runner struct {
	Mode  string `json:"mode"`
	Cmd   string `json:"cmd,omitempty"`
	Model string `json:"model,omitempty"`
}

func (p Policy) check() error {
	for s, r := range p.Runners {
		switch {
		case r.Mode == "process" && r.Cmd == "":
			return fmt.Errorf("runners.%s: process needs cmd", s)
		case r.Mode != "subagent" && r.Mode != "process":
			return fmt.Errorf("runners.%s: mode must be subagent or process", s)
		}
	}
	return nil
}

// Rule: agents matching Agent (path.Match over Agents; "self" = the spec's self) run on Computer, for the listed classes (none = any).
type Rule struct {
	Agent    string   `json:"agent"`
	Computer string   `json:"computer"`
	Class    []string `json:"class,omitempty"`
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
				err = p.check()
			}
			if err == nil {
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
	register("policy", pollEvery, func() (interface{}, error) { return currentPolicy(), nil })
}
