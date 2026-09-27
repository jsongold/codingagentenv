package main

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"
)

// PlaceSpec is the POST /v1/place body (ADR-0010).
type PlaceSpec struct {
	Class     string `json:"class"`
	Resources struct {
		MemoryMB   int     `json:"memoryMB"`
		CPUs       float64 `json:"cpus"`
		TimeoutMin int     `json:"timeoutMin"`
	} `json:"resources"`
	Placement struct {
		Allow      []string `json:"allow"` // empty = every policy computer
		Strategy   string   `json:"strategy"`
		Provider   string   `json:"provider"` // explicit computer; only it is considered
		Order      []string `json:"order"`    // ranked strategy
		MaxCostUSD float64  `json:"maxCostUSD"`
	} `json:"placement"`
}

type Placement struct {
	Agent    string   `json:"agent,omitempty"`
	Computer string   `json:"computer,omitempty"`
	CostUSD  float64  `json:"costUSD"`
	Reason   []string `json:"reason"`
}

// classCaps: capability a class needs on its computer.
var classCaps = map[string]string{"needs-db": "db"}

func placeCost(c Computer, s PlaceSpec) float64 {
	min := math.Max(float64(s.Resources.TimeoutMin), float64(c.MinBillSec)/60)
	return (c.VCPUHourUSD*s.Resources.CPUs + c.GiBHourUSD*float64(s.Resources.MemoryMB)/1024) * min / 60
}

// place is pure: same policy, usage, spec and localSlots give the same answer.
// Returns 200, 409 (usage window emptied the agents; deferUntil set) or 422.
func place(pol Policy, usage map[string]AgentUsage, s PlaceSpec, localSlots int) (out Placement, status int, deferUntil time.Time) {
	drop := func(format string, a ...interface{}) { out.Reason = append(out.Reason, fmt.Sprintf(format, a...)) }

	var agents []string
	for _, a := range pol.Agents {
		for _, pat := range pol.ClassAgents[s.Class] {
			if ok, _ := path.Match(pat, a); ok {
				agents = append(agents, a)
				break
			}
		}
	}
	prio := func(a string) int { // service order from policy.agentPriority; unlisted services go last
		svc, _, _ := strings.Cut(a, "/")
		for i, p := range pol.AgentPriority {
			if p == svc {
				return i
			}
		}
		return len(pol.AgentPriority)
	}
	sort.Slice(agents, func(i, j int) bool {
		if pi, pj := prio(agents[i]), prio(agents[j]); pi != pj {
			return pi < pj
		}
		return agents[i] < agents[j]
	})
	if len(agents) == 0 {
		drop("class %q: no agent in policy.agents matches classAgents", s.Class)
	}

	// Window filter: agent-only, so it filters agents rather than pairs.
	limit, est := 100-pol.Placement.ReservePct, pol.Placement.EstPct[s.Class]
	windowed := len(agents) > 0
	var fit []string
	for _, a := range agents {
		u, ok := usage[a]
		if !ok {
			drop("%s: usage unknown", a)
			fit = append(fit, a)
			continue
		}
		over, free := false, time.Time{} // free: when it fits again = latest resetsAt of exceeded windows
		for _, w := range []struct {
			name string
			w    UsageWindow
		}{{"5h", u.FiveHour}, {"7d", u.SevenDay}} {
			if w.w.UsedPct+est > limit {
				drop("%s: %s window", a, w.name)
				over = true
				if w.w.ResetsAt.After(free) {
					free = w.w.ResetsAt
				}
			}
		}
		if !over {
			fit = append(fit, a)
		} else if deferUntil.IsZero() || free.Before(deferUntil) {
			deferUntil = free
		}
	}
	agents = fit

	names := s.Placement.Allow
	if len(names) == 0 {
		for n := range pol.Computers {
			names = append(names, n)
		}
	}
	if s.Placement.Provider != "" {
		names = []string{s.Placement.Provider}
	}
	names = append([]string(nil), names...)
	sort.Strings(names)

	var keep []string
	for i, n := range names {
		if i > 0 && names[i-1] == n {
			continue
		}
		c, ok := pol.Computers[n]
		r := s.Resources
		switch {
		case !ok:
			drop("%s: not in policy.computers", n)
		case c.MaxMemMB > 0 && r.MemoryMB > c.MaxMemMB, c.MaxCPUs > 0 && r.CPUs > c.MaxCPUs, c.MaxMin > 0 && r.TimeoutMin > c.MaxMin:
			drop("%s: resources exceed limits (mem %d, cpus %g, min %d)", n, c.MaxMemMB, c.MaxCPUs, c.MaxMin)
		case classCaps[s.Class] != "" && !c.Caps[classCaps[s.Class]]:
			drop("%s: lacks cap %s for %s", n, classCaps[s.Class], s.Class)
		case s.Placement.MaxCostUSD > 0 && placeCost(c, s) > s.Placement.MaxCostUSD:
			drop("%s: cost %.4f > maxCostUSD %.4f", n, placeCost(c, s), s.Placement.MaxCostUSD)
		case n == "local" && localSlots < 1:
			drop("local: no free slots")
		case s.Placement.Strategy == "safe" && c.Preemptible:
			drop("%s: preemptible (strategy safe)", n)
		default:
			keep = append(keep, n)
		}
	}
	for _, n := range keep {
		if n == "local" && len(keep) > 1 {
			drop("local-first: dropped %v", without(keep, "local"))
			keep = []string{"local"}
			break
		}
	}
	if len(keep) > 0 && len(agents) == 0 && windowed { // window filter emptied it
		return out, http.StatusConflict, deferUntil
	}
	if len(agents) == 0 || len(keep) == 0 {
		return out, http.StatusUnprocessableEntity, time.Time{}
	}

	rank := map[string]int{}
	for i, n := range s.Placement.Order {
		if _, ok := rank[n]; !ok {
			rank[n] = i + 1
		}
	}
	key := func(n string) []float64 {
		c := pol.Computers[n]
		cost, cold, pre := placeCost(c, s), float64(c.ColdStartSec), 0.0
		if c.Preemptible {
			pre = 1
		}
		switch s.Placement.Strategy {
		case "fast":
			return []float64{cold, cost}
		case "safe":
			return []float64{cost, cold}
		case "ranked":
			if r, ok := rank[n]; ok {
				return []float64{float64(r)}
			}
			return []float64{math.MaxInt32} // unranked go last
		default: // cheap
			return []float64{cost, pre, cold}
		}
	}
	// Agents are already in (priority, name) order and computer keys don't depend on the agent, so the best
	// pair is (first agent, best computer); ties fall back to the computer name (keep is sorted).
	sort.SliceStable(keep, func(i, j int) bool {
		a, b := key(keep[i]), key(keep[j])
		for k := range a {
			if a[k] != b[k] {
				return a[k] < b[k]
			}
		}
		return false
	})
	out.Agent, out.Computer, out.CostUSD = agents[0], keep[0], placeCost(pol.Computers[keep[0]], s)
	return out, http.StatusOK, time.Time{}
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
	switch st := s.Placement.Strategy; {
	case err != nil, s.Class == "", st != "" && st != "cheap" && st != "fast" && st != "safe" && st != "ranked":
		http.Error(w, `want {"class":..,"resources":{..},"placement":{"strategy":"cheap|fast|safe|ranked",..}}`, http.StatusBadRequest)
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
