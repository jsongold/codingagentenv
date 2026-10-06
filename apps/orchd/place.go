package main

import (
	"fmt"
	"net/http"
	"path"
	"slices"
	"sort"
	"strings"
	"time"
)

// PlaceSpec is what `orchd place` was asked for (#97, #98).
type PlaceSpec struct {
	Class   string   `json:"class"`
	Self    string   `json:"self,omitempty"`    // the Orchestrator's own agent, matched by rule agent "self"
	Exclude []string `json:"exclude,omitempty"` // computers to skip (e.g. one that could not start)
}

type Placement struct {
	Agent    string   `json:"agent,omitempty"`
	Computer string   `json:"computer,omitempty"`
	Rule     int      `json:"rule"`
	Reason   []string `json:"reason"`
	Runner   *Runner  `json:"runner,omitempty"` // policy.runners[service of Agent]
}

// PlaceOutput is what `orchd place` prints on every outcome (placed, deferred, no rule fits, cad not
// ready): all keys always present so callers branch on runner (== Runner{} when nothing was placed)
// instead of on exit code. One struct, so the shape can't drift between outcomes.
type PlaceOutput struct {
	Agent      string   `json:"agent"`
	Computer   string   `json:"computer"`
	Rule       int      `json:"rule"` // -1 when nothing was placed
	Mode       string   `json:"mode"`
	ModeSource string   `json:"modeSource"`
	CadAddr    string   `json:"cadAddr"`
	Runner     Runner   `json:"runner"`      // Runner{} when nothing was placed
	Reason     []string `json:"reason"`      // [] when none
	DeferUntil string   `json:"defer_until"` // RFC3339, "" when none
}

// place evaluates policy.rules top to bottom; the first rule with a usable agent wins (#97).
// Pure: same policy, usage, spec and localSlots give the same answer.
// Returns 200, 409 (some rule was blocked only by usage windows; deferUntil set) or 422
// (HTTP codes kept from the former cad endpoint; main maps them to exit 0/3/4).
func place(pol Policy, usage map[string]AgentUsage, s PlaceSpec, localSlots int) (out Placement, status int, deferUntil time.Time) {
	drop := func(format string, a ...interface{}) { out.Reason = append(out.Reason, fmt.Sprintf(format, a...)) }
	limit, est := 100-pol.Placement.ReservePct, pol.Classes[s.Class].EstPct
	pick := func(a, computer string, i int, rn Runner) {
		out.Agent, out.Computer, out.Rule, out.Runner = a, computer, i, &rn
	}
	agents := append([]string(nil), pol.Agents...)
	sort.Strings(agents)
	for i, r := range pol.Rules {
		if len(r.Class) > 0 && !slices.Contains(r.Class, s.Class) {
			drop("rule %d: class", i)
			continue
		}
		if slices.Contains(s.Exclude, r.Computer) {
			drop("rule %d: computer %s excluded", i, r.Computer)
			continue
		}
		if !slices.Contains(pol.Computers, r.Computer) && r.Computer != "local" {
			drop("rule %d: computer %q unknown", i, r.Computer)
			continue
		}
		if r.Computer == "local" && localSlots < 1 {
			drop("rule %d: no local slot", i)
			continue
		}
		cands := agents
		if r.Agent == "self" {
			if !slices.Contains(agents, s.Self) {
				drop("rule %d: self unknown", i)
				continue
			}
			cands = []string{s.Self}
		}
		matched := false
		for _, a := range cands {
			if ok, _ := path.Match(r.Agent, a); !ok && r.Agent != "self" {
				continue
			}
			matched = true
			rn, ok := pol.runner(strings.SplitN(a, "/", 2)[0], r.Computer)
			if !ok { // the Orchestrator could not launch it
				drop("%s: no runner", a)
				continue
			}
			if rn.Mode == "subagent" && a != s.Self { // a subagent always runs as the Orchestrator's account
				drop("%s: subagent runs only as self", a)
				continue
			}
			u, ok := usage[a]
			if ok && u.Stale { // e.g. cad just restarted from its snapshot
				drop("%s: usage stale", a)
				if pol.Placement.StaleUsage == "block" {
					continue
				}
				pick(a, r.Computer, i, rn)
				return out, http.StatusOK, time.Time{}
			}
			if !ok {
				drop("%s: usage unknown", a)
				pick(a, r.Computer, i, rn)
				return out, http.StatusOK, time.Time{}
			}
			over, free := false, time.Time{} // free: when it fits again = latest resetsAt of exceeded windows
			for _, w := range []struct {
				name string
				w    *UsageWindow
			}{{"5h", u.FiveHour}, {"7d", u.SevenDay}, {"monthly", u.Monthly}} {
				if w.w != nil && (w.w.RateLimited || w.w.UsedPct+est > limit) { // nil: the account has no such window
					drop("%s: %s window", a, w.name)
					over = true
					if w.w.ResetsAt.After(free) {
						free = w.w.ResetsAt
					}
				}
			}
			if !over {
				pick(a, r.Computer, i, rn)
				return out, http.StatusOK, time.Time{}
			}
			if deferUntil.IsZero() || free.Before(deferUntil) {
				deferUntil = free
			}
		}
		if !matched {
			drop("rule %d: no agent matches %q", i, r.Agent)
		}
	}
	if !deferUntil.IsZero() {
		return out, http.StatusConflict, deferUntil
	}
	return out, http.StatusUnprocessableEntity, time.Time{}
}

// UsageWindow / AgentUsage mirror cad's "usage" topic (GET /v1/usage); only what place reads.
type UsageWindow struct {
	UsedPct     float64   `json:"usedPct"`
	ResetsAt    time.Time `json:"resetsAt"`
	RateLimited bool      `json:"rateLimited,omitempty"`
}

type AgentUsage struct {
	FiveHour *UsageWindow `json:"fiveHour"` // nil (JSON null): the account has no such window
	SevenDay *UsageWindow `json:"sevenDay"`
	Monthly  *UsageWindow `json:"monthly"` // opencode only
	Stale    bool         `json:"stale,omitempty"`
	Error    string       `json:"error,omitempty"`
}

// usableUsage turns cad's usage into what place filters on: agents with Error are dropped (place
// reports them as "usage unknown"), Stale ones are kept for place to report and windows whose reset has passed count as 0%.
// A nil window stays nil (place does not filter on it).
func usableUsage(m map[string]AgentUsage, now time.Time) map[string]AgentUsage {
	out := map[string]AgentUsage{}
	for a, u := range m {
		if u.Error != "" {
			continue
		}
		for _, w := range []**UsageWindow{&u.FiveHour, &u.SevenDay, &u.Monthly} {
			if *w != nil && !(*w).ResetsAt.After(now) {
				*w = &UsageWindow{ResetsAt: (*w).ResetsAt} // copy: the decoded value may be shared
			}
		}
		out[a] = u
	}
	return out
}
