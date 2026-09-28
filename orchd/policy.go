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
	Rules     []Rule                     `json:"rules"`     // ordered decision list; first match wins
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

// Runner: "subagent" (a Task subagent of the Orchestrator) or "process" (run Cmd; {model} is Model).
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
		case r.Mode == "process" && r.Cmd == "":
			err = fmt.Errorf("runners.%s: process needs cmd", s)
		case r.Mode != "subagent" && r.Mode != "process":
			err = fmt.Errorf("runners.%s: mode must be subagent or process", s)
		}
	}
	if err != nil {
		return Policy{}, fmt.Errorf("policy %s: %v", policyFile(), err)
	}
	return p, nil
}
