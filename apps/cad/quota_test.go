package main

import (
	"testing"
	"time"
)

func TestResetExpiredQuotas(t *testing.T) {
	h := newHub()
	now := time.Now().UTC()
	past, future := now.Add(-time.Minute), now.Add(time.Hour)
	h.setQuota(Quota{Reviewer: "due", State: "exhausted", ResetAt: &past, Source: "api"})
	h.setQuota(Quota{Reviewer: "later", State: "exhausted", ResetAt: &future, Source: "api"})
	h.setQuota(Quota{Reviewer: "unknown", State: "exhausted", Source: "api"}) // no resetAt: stays until reported ok
	rev := h.current()[len(h.current())-1].Rev

	h.resetExpiredQuotas(now)

	want := map[string]string{"due": "ok", "later": "exhausted", "unknown": "exhausted"}
	for name, state := range want {
		q := h.quotas[name]
		if q.State != state {
			t.Errorf("%s: state %q, want %q", name, q.State, state)
		}
	}
	if q := h.quotas["due"]; q.ResetAt != nil || q.LastHitAt == nil || q.Source != "api" {
		t.Errorf("due after reset: %+v (want resetAt cleared, lastHitAt and source kept)", q)
	}
	if last := h.current()[len(h.current())-1]; last.Topic != "quota" || last.Rev <= rev {
		t.Errorf("reset not published: %+v", last)
	}

	rev = h.current()[len(h.current())-1].Rev
	h.resetExpiredQuotas(now) // nothing due: no new event
	if r := h.current()[len(h.current())-1].Rev; r != rev {
		t.Errorf("idempotent reset published rev %d (was %d)", r, rev)
	}
}
