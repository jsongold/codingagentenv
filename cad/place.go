package main

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"path"
	"sort"
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

// place is pure: same policy, spec and localSlots give the same answer. ok=false means 422.
func place(pol Policy, s PlaceSpec, localSlots int) (Placement, bool) {
	var out Placement
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
	sort.Strings(agents)
	if len(agents) == 0 {
		drop("class %q: no agent in policy.agents matches classAgents", s.Class)
	}

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
	// ponytail: cad has no usage topic yet; the window filter (and 409 defer_until) goes here once it does.
	drop("usage unknown: window filter skipped")

	for _, n := range keep {
		if n == "local" && len(keep) > 1 {
			drop("local-first: dropped %v", without(keep, "local"))
			keep = []string{"local"}
			break
		}
	}
	if len(agents) == 0 || len(keep) == 0 {
		return out, false
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
	// Agents are already sorted and computer keys don't depend on the agent, so the best
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
	return out, true
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

// localSlots reads slots from the published capacity topic (CAD_SLOTS already applied there).
func (h *hub) localSlots() int {
	for _, e := range h.current() {
		if e.Topic == "capacity" {
			var c Capacity
			if json.Unmarshal(e.Data, &c) == nil {
				return c.Slots
			}
		}
	}
	return 0
}

func (h *hub) postPlace(w http.ResponseWriter, r *http.Request) {
	// ns is required (ADR-0010) but not used yet: there is one policy per cad.
	if r.URL.Query().Get("ns") == "" {
		http.Error(w, "ns query parameter required", http.StatusBadRequest)
		return
	}
	var s PlaceSpec
	err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&s)
	switch st := s.Placement.Strategy; {
	case err != nil, s.Class == "", st != "" && st != "cheap" && st != "fast" && st != "safe" && st != "ranked":
		http.Error(w, `want {"class":..,"resources":{..},"placement":{"strategy":"cheap|fast|safe|ranked",..}}`, http.StatusBadRequest)
		return
	}
	p, ok := place(currentPolicy(), s, h.localSlots())
	if !ok {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		json.NewEncoder(w).Encode(map[string][]string{"reason": p.Reason})
		return
	}
	writeJSON(w, p)
}
