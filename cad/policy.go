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
	Computers map[string]Computer `json:"computers"` // records only (`cad show cost`)
	Agents    []string            `json:"agents"`    // e.g. "claude/default", "claude/3f9a1c0e"; usage is collected for these
	Collect   struct {
		Usage struct {
			Every string `json:"every"` // time.ParseDuration; CAD_USAGE_EVERY overrides
		} `json:"usage"`
	} `json:"collect"`
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

// currentPolicy resolves policyFile() (see home.go), or the built-in default when no env names a file and it is absent,
// re-reading the file when its mtime changes. A bad file is logged and the last good policy kept.
func currentPolicy() Policy {
	polMu.Lock()
	defer polMu.Unlock()
	path := policyFile()
	if _, err := os.Stat(path); err != nil && os.Getenv("CAD_CONFIG") == "" && os.Getenv("CAD_POLICY") == "" {
		pol, polPath = defaultPolicy(), ""
		return pol
	}
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	st, err := os.Stat(path)
	if err == nil && (path != polPath || !st.ModTime().Equal(polMod)) {
		polPath, polMod = path, st.ModTime() // a bad file is logged once per change, not on every call
		var b []byte
		var p Policy
		if b, err = os.ReadFile(path); err == nil {
			if err = json.Unmarshal(b, &p); err == nil {
				p.Source = path
				pol = p
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
