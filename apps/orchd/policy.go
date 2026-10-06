package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

// Policy is apps/orchd/policy.json (see home.go). Agents and computer names are copies of apps/cad/config.json's
// (orchd does not read cad's files; keep them in sync by hand for now).
type Policy struct {
	Agents    []string        `json:"agents"`
	Computers []string        `json:"computers"` // names only: a rule's computer must be one
	CadAddr   string          `json:"cadAddr"`   // cad for mode "auto" (see cadAddr in mode.go)
	Rules     []Rule          `json:"rules"`     // ordered decision list; first match wins (= mode "auto")
	Modes     map[string]Mode `json:"modes"`     // mode name -> its rule list, used instead of Rules (see mode.go)
	Placement struct {
		ReservePct float64 `json:"reservePct"` // usage headroom kept free per window
		StaleUsage string  `json:"staleUsage"` // "pass" (default: a stale agent is placed as if usage were unknown) or "block" (skipped)
	} `json:"placement"`
	Classes map[string]Class  `json:"classes"` // the task classes place accepts (the Orchestrator classifies by criteria)
	Runners map[string]Runner `json:"runners"` // service -> how the Orchestrator launches it (returned by place)
}

type Mode struct {
	CadAddr string `json:"cadAddr"` // cad this mode asks for usage/capacity ("" = default)
	Rules   []Rule `json:"rules"`
}

// Class: Criteria tells the Orchestrator when to pick it; EstPct is the usage % one task consumes.
type Class struct {
	Criteria string  `json:"criteria"`
	EstPct   float64 `json:"estPct"`
}

// Runner is everything needed to launch on one destination: Cmd is an argv template whose {key} is the
// record's same-named field (dispatch substitutes and execs it, piping the file Stdin in). Mode says how
// the Orchestrator treats it ("subagent", "process", "cloud", "vm"); dispatch does not read it.
type Runner struct {
	Mode      string   `json:"mode,omitempty"` // omitempty: an empty Runner{} (no rule fit) must marshal as {}
	Cmd       []string `json:"cmd,omitempty"`
	Stdin     string   `json:"stdin,omitempty"`     // path piped to Cmd's stdin
	Session   string   `json:"session,omitempty"`   // filled by place: the cloud worker session ({session})
	ConfigDir string   `json:"configDir,omitempty"` // filled by place: CLAUDE_CONFIG_DIR for a claude/<id> agent ({configDir})
	Model     string   `json:"model,omitempty"`
	Instance  string   `json:"instance,omitempty"` // mode vm: one instance name, or a comma-separated list (1-3, same kind; vm.go picks a stopped one)
	Zone      string   `json:"zone,omitempty"`
	Project   string   `json:"project,omitempty"`
	Image     string   `json:"image,omitempty"`
}

// Rule: agents matching Agent (path.Match over Agents; "self" = the spec's self) run on Computer, for the listed classes (none = any).
type Rule struct {
	Agent    string   `json:"agent"`
	Computer string   `json:"computer"`
	Class    []string `json:"class,omitempty"`
}

// loadPolicy reads the file on every call (the CLI is short-lived) and checks what place relies on.
func loadPolicy() (Policy, error) {
	var p Policy
	b, err := os.ReadFile(policyFile())
	if err == nil {
		err = json.Unmarshal(b, &p)
	}
	if err == nil && len(p.Classes) == 0 {
		err = errors.New(`no "classes" (want classes{name:{criteria,estPct}} — see #97)`)
	}
	if s := p.Placement.StaleUsage; err == nil && s != "" && s != "pass" && s != "block" {
		err = fmt.Errorf("placement.staleUsage %q: want pass or block", s)
	}
	for s, r := range p.Runners {
		switch {
		case err != nil:
		case r.Mode == "vm" && (r.Instance == "" || r.Zone == "" || r.Project == "" || r.Image == "" || r.Model == ""):
			err = fmt.Errorf("runners.%s: vm needs instance, zone, project, image and model", s)
		case r.Mode != "subagent" && r.Mode != "vm" && len(r.Cmd) == 0:
			err = fmt.Errorf("runners.%s: %s needs cmd", s, r.Mode)
		case r.Mode != "subagent" && r.Mode != "process" && r.Mode != "cloud" && r.Mode != "vm":
			err = fmt.Errorf("runners.%s: mode must be subagent, process, cloud or vm", s)
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
