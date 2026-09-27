package main

import "time"

// resetExpiredQuotas flips exhausted reviewers back to ok once their resetAt has passed.
// It goes through setQuota, so the change is published like any other quota write.
func (h *hub) resetExpiredQuotas(now time.Time) {
	h.qmu.Lock()
	var due []Quota
	for _, q := range h.quotas {
		if q.State == "exhausted" && q.ResetAt != nil && !q.ResetAt.After(now) {
			due = append(due, q)
		}
	}
	h.qmu.Unlock()
	// ponytail: a POST landing between the unlock and setQuota is overwritten; a check-and-set in
	// events.go closes that window if it ever matters.
	for _, q := range due {
		h.setQuota(Quota{Reviewer: q.Reviewer, State: "ok", Source: q.Source})
	}
}

// runQuotaResets calls resetExpiredQuotas on every tick. Wiring: main.go needs
// `go h.runQuotaResets(2 * time.Second)` next to `go h.run(...)` (main.go is outside this change).
func (h *hub) runQuotaResets(every time.Duration) {
	for now := range time.Tick(every) {
		h.resetExpiredQuotas(now)
	}
}
