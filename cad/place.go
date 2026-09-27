package main

import (
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"path"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"
)

// PlaceSpec is the POST /v1/place body (ADR-0010). Other fields are ignored.
type PlaceSpec struct {
	Class string `json:"class"`
	Self  string `json:"self,omitempty"` // the Orchestrator's own agent, matched by rule agent "self"
}

type Placement struct {
	Agent    string   `json:"agent,omitempty"`
	Computer string   `json:"computer,omitempty"`
	Rule     int      `json:"rule"`
	Reason   []string `json:"reason"`
	Runner   *Runner  `json:"runner,omitempty"` // policy.runners[service of Agent]
}

// place evaluates policy.rules top to bottom; the first rule with a usable agent wins (ADR-0010).
// Pure: same policy, usage, spec and localSlots give the same answer.
// Returns 200, 409 (some rule was blocked only by usage windows; deferUntil set) or 422.
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
		if _, ok := pol.Computers[r.Computer]; !ok && r.Computer != "local" {
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
			rn, ok := pol.Runners[strings.SplitN(a, "/", 2)[0]]
			if !ok { // the Orchestrator could not launch it
				drop("%s: no runner", a)
				continue
			}
			if rn.Mode == "subagent" && a != s.Self { // a subagent always runs as the Orchestrator's account
				drop("%s: subagent runs only as self", a)
				continue
			}
			u, ok := usage[a]
			if !ok {
				drop("%s: usage unknown", a)
				pick(a, r.Computer, i, rn)
				return out, http.StatusOK, time.Time{}
			}
			over, free := false, time.Time{} // free: when it fits again = latest resetsAt of exceeded windows
			for _, w := range []struct {
				name string
				w    *UsageWindow
			}{{"5h", u.FiveHour}, {"7d", u.SevenDay}} {
				if w.w != nil && w.w.UsedPct+est > limit { // nil: the account has no such window
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

func without(xs []string, x string) []string {
	var out []string
	for _, v := range xs {
		if v != x {
			out = append(out, v)
		}
	}
	return out
}

// topic decodes the published value of name into v; false if it is not published (yet).
func (h *hub) topic(name string, v interface{}) bool {
	for _, e := range h.current() {
		if e.Topic == name {
			return json.Unmarshal(e.Data, v) == nil
		}
	}
	return false
}

// localSlots reads slots from the published capacity topic (CAD_SLOTS already applied there).
func (h *hub) localSlots() int {
	var c Capacity
	h.topic("capacity", &c)
	return c.Slots
}

// nsRe validates the required ns query parameter (ADR-0010; one namespace per cad for now).
var nsRe = regexp.MustCompile(`^[a-z0-9-]+$`)

// requireNS answers 400 and returns false when ?ns= is missing or malformed.
func requireNS(w http.ResponseWriter, r *http.Request) bool {
	if !nsRe.MatchString(r.URL.Query().Get("ns")) {
		http.Error(w, "ns query parameter required (^[a-z0-9-]+$)", http.StatusBadRequest)
		return false
	}
	return true
}

func (h *hub) postPlace(w http.ResponseWriter, r *http.Request) {
	// ns is required (ADR-0010) but not used yet: there is one policy per cad.
	if !requireNS(w, r) {
		return
	}
	var s PlaceSpec
	err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&s)
	pol := currentPolicy()
	if _, ok := pol.Classes[s.Class]; err != nil || !ok || (s.Self != "" && !agentRe.MatchString(s.Self)) {
		classes := slices.Sorted(maps.Keys(pol.Classes))
		http.Error(w, fmt.Sprintf(`want {"class":..,"self"?:"<service>/<account>"} with class in %v`, classes), http.StatusBadRequest)
		return
	}
	var u UsageMap
	h.topic("usage", &u)
	p, status, until := place(pol, usableUsage(u, time.Now()), s, h.localSlots())
	if status == http.StatusOK {
		writeJSON(w, p)
		return
	}
	body := map[string]interface{}{"reason": p.Reason}
	if status == http.StatusConflict {
		body["defer_until"] = until
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(body)
}
