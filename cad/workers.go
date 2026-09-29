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
	"syscall"
	"time"
)

type Worker struct {
	ID         string            `json:"id"`
	Provider   string            `json:"provider"`
	State      string            `json:"state"`
	StartedAt  time.Time         `json:"startedAt"`
	LastSeenAt time.Time         `json:"lastSeenAt"`
	Labels     map[string]string `json:"labels,omitempty"`
	// ExitCode is set by a provider once a worker has finished (#96);
	// omitted for still-running workers.
	ExitCode *int `json:"exitCode,omitempty"`
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
		ExitCode            *int
	}
	ks := make([]k, len(l))
	for i, w := range l {
		ks[i] = k{w.ID, w.Provider, w.State, w.StartedAt, w.Labels, w.ExitCode}
	}
	return ks
}

const (
	workerPollInterval  = 10 * time.Second // #95: cad pulls worker state from providers
	workerListTimeout   = 5 * time.Second
	workerKillGrace     = 2 * time.Second // extra time to force-close pipes if a grandchild still holds them
	workerFailThreshold = 3               // consecutive failures before a provider's workers flip to "unknown"
)

// rawWorker is what a provider's `list` script prints (see agent/providers/<name>/list).
type rawWorker struct {
	ID        string            `json:"id"`
	State     string            `json:"state"`
	StartedAt time.Time         `json:"startedAt"`
	Labels    map[string]string `json:"labels"`
	ExitCode  *int              `json:"exitCode"`
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
// The script gets its own process group so a timeout kills any children it
// spawned too; WaitDelay bounds Wait() even if a grandchild still holds the
// output pipes open (e.g. it detached before the group kill landed).
func runList(script string) ([]rawWorker, error) {
	ctx, cancel := context.WithTimeout(context.Background(), workerListTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, script)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = workerKillGrace
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

// markFailure records a failed poll for provider and, once it has failed
// workerFailThreshold times in a row, flips its cached workers to "unknown".
func markFailure(provider string) {
	workerStore.mu.Lock()
	defer workerStore.mu.Unlock()
	workerStore.fails[provider]++
	if workerStore.fails[provider] >= workerFailThreshold {
		for i := range workerStore.byProvider[provider] {
			workerStore.byProvider[provider][i].State = "unknown"
		}
	}
}

// pollWorkers refreshes the cache for every allowed provider that has an
// executable list script. A failing provider (including one whose script
// disappeared or lost its executable bit) keeps its previous list; that
// list's entries flip to "unknown" once the provider has failed
// workerFailThreshold times in a row. Providers no longer in the allowed set
// are evicted from the cache so stale workers don't linger forever.
func pollWorkers() {
	dir := providersDir()
	allowed := currentPolicy().Providers.Allowed
	allowedSet := make(map[string]bool, len(allowed))
	for _, p := range allowed {
		allowedSet[p] = true
	}
	workerStore.mu.Lock()
	for p := range workerStore.byProvider {
		if !allowedSet[p] {
			delete(workerStore.byProvider, p)
			delete(workerStore.fails, p)
		}
	}
	workerStore.mu.Unlock()

	for _, provider := range allowed {
		script := filepath.Join(dir, provider, "list")
		if !isExecutable(script) {
			markFailure(provider)
			continue
		}
		list, err := runList(script)
		if err != nil {
			log.Printf("cad: worker provider %s: %v", provider, err)
			markFailure(provider)
			continue
		}
		now := time.Now().UTC()
		workers := make([]Worker, len(list))
		for i, rw := range list {
			workers[i] = Worker{ID: rw.ID, Provider: provider, State: rw.State, StartedAt: rw.StartedAt, LastSeenAt: now, Labels: rw.Labels, ExitCode: rw.ExitCode}
		}
		workerStore.mu.Lock()
		workerStore.fails[provider] = 0
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

func init() { register("workers", pollEvery, collectWorkers) }
