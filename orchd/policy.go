package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

// Policy is the part of .agent/policy.json orchd reads. cad owns the rest (agents are shared).
type Policy struct {
	Agents    []string                   `json:"agents"`
	Computers map[string]json.RawMessage `json:"computers"` // names only: a rule's computer must exist
	Rules     []Rule                     `json:"rules"`     // ordered decision list; first match wins (= mode "auto")
	Modes     map[string]struct {
		Rules []Rule `json:"rules"`
	} `json:"modes"` // mode name -> its rule list, used instead of Rules (see mode.go)
	Placement struct {
		ReservePct float64 `json:"reservePct"` // usage headroom kept free per window
	} `json:"placement"`
	Classes map[string]Class  `json:"classes"` // the task classes place accepts (the Orchestrator classifies by criteria)
	Runners map[string]Runner `json:"runners"` // service -> how the Orchestrator launches it (returned by place)
}

// Class: Criteria tells the Orchestrator when to pick it; EstPct is the usage % one task consumes.
type Class struct {
	Criteria string  `json:"criteria"`
	EstPct   float64 `json:"estPct"`
}

// Runner: "subagent" (a Task subagent of the Orchestrator), "process" (run Cmd; {model} is Model)
// or "cloud" (the Orchestrator starts a cloud session with Cmd, e.g. claude --cloud).
type Runner struct {
	Mode  string `json:"mode"`
	Cmd   string `json:"cmd,omitempty"`
	Model string `json:"model,omitempty"`
}

// Rule: agents matching Agent (path.Match over Agents; "self" = the spec's self) run on Computer, for the listed classes (none = any).
type Rule struct {
	Agent    string   `json:"agent"`
	Computer string   `json:"computer"`
	Class    []string `json:"class,omitempty"`
}

func policyFile() string {
	for _, e := range []string{"ORCHD_POLICY", "CAD_POLICY"} {
		if p := os.Getenv(e); p != "" {
			return p
		}
	}
	return ".agent/policy.json"
}

// loadPolicy reads the file on every call (the CLI is short-lived) and checks what place relies on.
func loadPolicy() (Policy, error) {
	var p Policy
	b, err := os.ReadFile(policyFile())
	if err == nil {
		err = json.Unmarshal(b, &p)
	}
	if err == nil && len(p.Classes) == 0 {
		err = errors.New(`no "classes" (want classes{name:{criteria,estPct}} — see ADR-0010)`)
	}
	for s, r := range p.Runners {
		switch {
		case err != nil:
		case r.Mode != "subagent" && r.Cmd == "":
			err = fmt.Errorf("runners.%s: %s needs cmd", s, r.Mode)
		case r.Mode != "subagent" && r.Mode != "process" && r.Mode != "cloud":
			err = fmt.Errorf("runners.%s: mode must be subagent, process or cloud", s)
		}
	}
	if err != nil {
		return Policy{}, fmt.Errorf("policy %s: %v", policyFile(), err)
	}
	return p, nil
}

// runner: runners["<service>@<computer>"] first (e.g. claude@claude-cloud), then runners["<service>"].
func (p Policy) runner(svc, computer string) (Runner, bool) {
	if rn, ok := p.Runners[svc+"@"+computer]; ok {
		return rn, true
	}
	rn, ok := p.Runners[svc]
	return rn, ok
}
