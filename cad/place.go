package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path"
	"regexp"
	"slices"
	"sort"
	"time"
)

// PlaceSpec is the POST /v1/place body (ADR-0010). Other fields are ignored.
type PlaceSpec struct {
	Class string `json:"class"`
}

type Placement struct {
	Agent    string   `json:"agent,omitempty"`
	Computer string   `json:"computer,omitempty"`
	Rule     int      `json:"rule"`
	Reason   []string `json:"reason"`
}

// place evaluates policy.rules top to bottom; the first rule with a usable agent wins (ADR-0010).
// Pure: same policy, usage, spec and localSlots give the same answer.
// Returns 200, 409 (some rule was blocked only by usage windows; deferUntil set) or 422.
func place(pol Policy, usage map[string]AgentUsage, s PlaceSpec, localSlots int) (out Placement, status int, deferUntil time.Time) {
	drop := func(format string, a ...interface{}) { out.Reason = append(out.Reason, fmt.Sprintf(format, a...)) }
	limit, est := 100-pol.Placement.ReservePct, pol.Placement.EstPct[s.Class]
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
		matched := false
		for _, a := range agents {
			if ok, _ := path.Match(r.Agent, a); !ok {
				continue
			}
			matched = true
			u, ok := usage[a]
			if !ok {
				drop("%s: usage unknown", a)
				out.Agent, out.Computer, out.Rule = a, r.Computer, i
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
				out.Agent, out.Computer, out.Rule = a, r.Computer, i
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
	if err != nil || !slices.Contains(knownClasses, s.Class) {
		http.Error(w, fmt.Sprintf(`want {"class":..} with class in %v`, knownClasses), http.StatusBadRequest)
		return
	}
	var u UsageMap
	h.topic("usage", &u)
	p, status, until := place(currentPolicy(), usableUsage(u, time.Now()), s, h.localSlots())
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
