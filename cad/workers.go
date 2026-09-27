package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type Worker struct {
	ID         string            `json:"id"`
	Provider   string            `json:"provider"`
	State      string            `json:"state"`
	StartedAt  time.Time         `json:"startedAt"`
	LastSeenAt time.Time         `json:"lastSeenAt"`
	Labels     map[string]string `json:"labels,omitempty"`
}

// WorkerList is published on the "workers" topic. lastSeenAt is excluded from
// the change key (like capacity's changeKey) so polling alone never emits an
// event when nothing actually changed.
type WorkerList []Worker

func (l WorkerList) changeKey() interface{} {
	type k struct {
		ID, Provider, State string
		StartedAt           time.Time
		Labels              map[string]string
	}
	ks := make([]k, len(l))
	for i, w := range l {
		ks[i] = k{w.ID, w.Provider, w.State, w.StartedAt, w.Labels}
	}
	return ks
}

const (
	workerPollInterval  = 10 * time.Second // ADR-0008: cad pulls worker state from providers
	workerListTimeout   = 5 * time.Second
	workerFailThreshold = 3 // consecutive failures before a provider's workers flip to "unknown"
)

// rawWorker is what a provider's `list` script prints (see agent/providers/<name>/list).
type rawWorker struct {
	ID        string            `json:"id"`
	State     string            `json:"state"`
	StartedAt time.Time         `json:"startedAt"`
	Labels    map[string]string `json:"labels"`
}

var workerStore = struct {
	mu         sync.Mutex
	byProvider map[string][]Worker
	fails      map[string]int
	lastPoll   time.Time
}{byProvider: map[string][]Worker{}, fails: map[string]int{}}

// providersDir resolves CAD_PROVIDERS_DIR, defaulting to ./agent/providers
// (the tools/cad wrapper sets it to <repo>/agent/providers when unset).
func providersDir() string {
	if d := os.Getenv("CAD_PROVIDERS_DIR"); d != "" {
		return d
	}
	return "agent/providers"
}

func isExecutable(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && !fi.IsDir() && fi.Mode()&0o111 != 0
}

// runList runs a provider's list script with a timeout and parses its stdout.
func runList(script string) ([]rawWorker, error) {
	ctx, cancel := context.WithTimeout(context.Background(), workerListTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, script)
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%s: %w (%s)", script, err, strings.TrimSpace(errOut.String()))
	}
	var list []rawWorker
	if err := json.Unmarshal(out.Bytes(), &list); err != nil {
		return nil, fmt.Errorf("%s: parse output: %w", script, err)
	}
	return list, nil
}

// pollWorkers refreshes the cache for every allowed provider that has an
// executable list script. A failing provider keeps its previous list; that
// list's entries flip to "unknown" once the provider has failed
// workerFailThreshold times in a row.
func pollWorkers() {
	dir := providersDir()
	for _, provider := range currentPolicy().Providers.Allowed {
		script := filepath.Join(dir, provider, "list")
		if !isExecutable(script) {
			continue
		}
		list, err := runList(script)
		workerStore.mu.Lock()
		if err != nil {
			log.Printf("cad: worker provider %s: %v", provider, err)
			workerStore.fails[provider]++
			if workerStore.fails[provider] >= workerFailThreshold {
				for i := range workerStore.byProvider[provider] {
					workerStore.byProvider[provider][i].State = "unknown"
				}
			}
			workerStore.mu.Unlock()
			continue
		}
		workerStore.fails[provider] = 0
		now := time.Now().UTC()
		workers := make([]Worker, len(list))
		for i, rw := range list {
			workers[i] = Worker{ID: rw.ID, Provider: provider, State: rw.State, StartedAt: rw.StartedAt, LastSeenAt: now, Labels: rw.Labels}
		}
		workerStore.byProvider[provider] = workers
		workerStore.mu.Unlock()
	}
}

func currentWorkers() WorkerList {
	workerStore.mu.Lock()
	defer workerStore.mu.Unlock()
	all := WorkerList{}
	for _, ws := range workerStore.byProvider {
		all = append(all, ws...)
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].Provider != all[j].Provider {
			return all[i].Provider < all[j].Provider
		}
		return all[i].ID < all[j].ID
	})
	return all
}

// collectWorkers is the registered collector: it self-throttles to
// workerPollInterval (reusing the hub's own tick, no extra goroutine) and
// otherwise just serves the cached state.
func collectWorkers() (interface{}, error) {
	workerStore.mu.Lock()
	due := time.Since(workerStore.lastPoll) >= workerPollInterval
	if due {
		workerStore.lastPoll = time.Now()
	}
	workerStore.mu.Unlock()
	if due {
		pollWorkers()
	}
	return currentWorkers(), nil
}

func init() { register("workers", collectWorkers) }
