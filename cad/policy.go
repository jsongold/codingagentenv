package main

import (
	"encoding/json"
	"os"
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
}

var (
	polMu  sync.Mutex
	pol    Policy
	polMod time.Time
)

// currentPolicy re-reads CAD_POLICY when its mtime changed; on error the last good policy is returned.
func currentPolicy() (Policy, error) {
	path := os.Getenv("CAD_POLICY")
	if path == "" {
		path = ".agent/policy.json"
	}
	polMu.Lock()
	defer polMu.Unlock()
	st, err := os.Stat(path)
	if err != nil {
		return pol, err
	}
	if !st.ModTime().Equal(polMod) {
		b, err := os.ReadFile(path)
		if err != nil {
			return pol, err
		}
		var p Policy
		if err := json.Unmarshal(b, &p); err != nil {
			return pol, err
		}
		pol, polMod = p, st.ModTime()
	}
	return pol, nil
}

func init() {
	register("policy", func() (interface{}, error) { return currentPolicy() })
}
