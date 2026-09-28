package main

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"time"
)

// snapshotMaxAge: an older usage snapshot is ignored on start (the numbers would mislead more than help).
const snapshotMaxAge = 24 * time.Hour

// stateDir: CAD_STATE_DIR > <appDir>/state (appDir is CAD_HOME when set; gitignored).
func stateDir() string {
	if d := os.Getenv("CAD_STATE_DIR"); d != "" {
		return d
	}
	return filepath.Join(appDir(), "state")
}

func snapshotFile() string { return filepath.Join(stateDir(), "usage-snapshot.json") }

// saveUsageSnapshot writes m atomically (temp file + rename), so a crash never leaves a torn file.
func saveUsageSnapshot(m UsageMap) error {
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(stateDir(), 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(stateDir(), ".usage-snapshot-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name()) // no-op after a successful rename
	if _, err = f.Write(b); err == nil {
		err = f.Close()
	} else {
		f.Close()
	}
	if err != nil {
		return err
	}
	return os.Rename(f.Name(), snapshotFile())
}

// loadUsageSnapshot returns the saved usage with every agent marked Stale (fetchedAt kept), or
// false when there is none, it is unreadable, or it was written more than maxAge before now.
func loadUsageSnapshot(now time.Time, maxAge time.Duration) (UsageMap, bool) {
	st, err := os.Stat(snapshotFile())
	if err != nil || now.Sub(st.ModTime()) > maxAge {
		return nil, false
	}
	b, err := os.ReadFile(snapshotFile())
	var m UsageMap
	if err == nil {
		err = json.Unmarshal(b, &m)
	}
	if err != nil || m == nil {
		log.Printf("cad: ignoring usage snapshot: %v", err)
		return nil, false
	}
	for a, u := range m {
		u.Stale = true
		m[a] = u
	}
	return m, true
}

// usageCollected is called after each real usage collection: it persists the map and marks cad ready.
func (h *hub) usageCollected(v interface{}) {
	if m, ok := v.(UsageMap); ok {
		if err := saveUsageSnapshot(m); err != nil {
			log.Printf("cad: save usage snapshot: %v", err)
		}
	}
	h.readySrc.Store("collected")
}

// loadSnapshot publishes the saved usage (all stale) so cad is ready right after a restart;
// the first real collection overwrites it.
func (h *hub) loadSnapshot() {
	if m, ok := loadUsageSnapshot(time.Now(), snapshotMaxAge); ok {
		h.publish("usage", m)
		h.readySrc.CompareAndSwap(nil, "snapshot")
		log.Printf("cad: loaded usage snapshot (%d agents, stale)", len(m))
	}
}
