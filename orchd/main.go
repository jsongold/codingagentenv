// orchd decides where a task runs (ADR-0011): it reads rules/classes/runners from orchd/policy.json
// and asks cad (HTTP only) for usage and capacity. Deleting orchd/ and tools/orchd leaves cad intact.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
)

const usageText = `usage:
  orchd place [--class <c>] [--self <service/account>] [--ns default] [--mode <m>] [--exclude <computer>[,...]]
      always prints all of {agent, computer, rule, mode, modeSource, cadAddr, runner, reason, defer_until},
      every outcome (placed / deferred / no rule fits / cad not ready), exit 0. rule = index in the mode's
      rule list, -1 when nothing was placed. runner = Runner{} (no keys) when nothing was placed, else the
      first rule with a usable agent's runner. reason: [] when none. defer_until: RFC3339, "" when none.
      No --class: no per-class usage estimate. runner.session (when runner.cmd uses {session}) =
      CLAUDE_CLOUD_SESSION > the namespace registry's cloudWorkerSession
  orchd mode set <m> [--ns <ns>] [--by <who>]   persist a mode (no --ns = all namespaces)
  orchd mode clear [--ns <ns>]                  remove that mode file
  orchd mode show [--ns <ns>]                   print the effective mode and where it came from
      mode precedence: --mode > <state>/mode/<ns>.json > <state>/mode/_global.json > ORCHD_MODE > auto
      "auto" = policy.rules (local is the last resort); others = policy.modes.<m>.rules (urgent = local-first)
  orchd pick [--ns default] [--repo o/r] [--path dir]
      claim the oldest open issue labeled ai without wip/ai-failed (adds wip); print {issue:{n,title,body}, classes:[{name,criteria}]}
      or {none, reason}. repo/path: flags > namespace registry > git toplevel of the CWD + gh repo view
  orchd dispatch --placement <json|->
      run the place output's runner: each {key} in runner.cmd (argv, no shell) = the runner's same-named
      field, runner.stdin (a path) piped in; print the command's stdout. runner {} (nothing was placed):
      print {} and exit 0, run nothing. runner with keys but no cmd, or missing {key}: exit 2
  orchd status --issue <n> [--pid <pid>] [--ns default] [--repo o/r] [--path dir]
      print {issue, pr, state, running?}: the PR whose body says "Closes #n" (OPEN wins), and with --pid
      whether the dispatched process still runs. One quick gh call; supervisors poll it
  orchd vm status --instance <i> --zone <z> --project <p>
      print {instance, status}: one instances.get (Compute API, no gcloud). For a monitor Subagent to tell a
      preempted/finished vm runner (TERMINATED, no PR yet) from one still working (skills/orchestrate)
  orchd show [rules|classes|runners]    print policy sections (no arg = all three)
  stale usage (cad restarted from its snapshot): policy placement.staleUsage "pass" (default; placed
      as if unknown, reason "<agent>: usage stale") or "block" (skipped)
files (CWD-independent): app dir = $ORCHD_HOME > dir above orchd's bin/ (if it has policy.json) > .
  policy: ORCHD_POLICY > <app>/policy.json; state: ORCHD_STATE_DIR > <app>/state
  namespaces: ORCHD_NAMESPACES > <app>/../cad/config/namespaces.json > its .example.json
env: ORCHD_MODE; CAD_ADDR (> mode cadAddr > top-level cadAddr for auto > 127.0.0.1:7878), CAD_TOKEN;
  vm: ORCHD_COMPUTE_URL (Compute API base), GCE_METADATA_HOST (token; off GCE: gcloud auth print-access-token)
exit codes:
  0  place (always, including deferred / no rule fits / cad not ready: check runner == {}) / picked
     (also when none) / dispatched (including runner {}, which runs nothing) / shown
  1  cad unreachable (after 3 retries 2s apart) / cad error / bad policy file / gh, git or the dispatched command failed /
     vm status: Compute API call failed (e.g. no access token, instance not found)
  2  bad input (unknown class or mode, bad --self or --ns, bad --issue or --placement, runner with keys
     but no cmd, or a {key} without value, unknown command)
`

var (
	agentRe = regexp.MustCompile(`^[a-z0-9-]+/[a-z0-9-]+$`)
	nsRe    = regexp.MustCompile(`^[a-z0-9-]+$`)
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) int {
	code, err := cmd(args, stdout)
	if err != nil {
		fmt.Fprintf(stderr, "orchd: %v\n", err)
		if code == 2 {
			fmt.Fprint(stderr, usageText)
		}
	}
	return code
}

func cmd(args []string, w io.Writer) (int, error) {
	if len(args) == 0 {
		return 2, errors.New("no command")
	}
	switch args[0] {
	case "help", "-h", "-help", "--help":
		fmt.Fprint(w, usageText)
		return 0, nil
	case "place":
		return placeCmd(args[1:], w)
	case "mode":
		return modeCmd(args[1:], w)
	case "pick":
		return pickCmd(args[1:], w)
	case "dispatch":
		return dispatchCmd(args[1:], os.Stdin, w)
	case "status":
		return statusCmd(args[1:], w)
	case "vm":
		return vmCmd(args[1:], w)
	case "show":
		if len(args) > 2 {
			return 2, errors.New("show takes at most one section")
		}
		p, err := loadPolicy()
		if err != nil {
			return 1, err
		}
		all := map[string]interface{}{"rules": p.Rules, "classes": p.Classes, "runners": p.Runners}
		if len(args) == 1 {
			return 0, printJSON(w, all)
		}
		if v, ok := all[args[1]]; ok {
			return 0, printJSON(w, v)
		}
		return 2, fmt.Errorf("show %q: want rules, classes or runners", args[1])
	}
	return 2, fmt.Errorf("unknown command %q", args[0])
}

func placeCmd(args []string, w io.Writer) (int, error) {
	fs := flag.NewFlagSet("place", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	class, self, ns := fs.String("class", "", ""), fs.String("self", "", ""), fs.String("ns", "default", "")
	modeFlag, exclude := fs.String("mode", "", ""), fs.String("exclude", "", "")
	if err := fs.Parse(args); err != nil || fs.NArg() > 0 {
		return 2, fmt.Errorf("place: bad args %q", args)
	}
	pol, err := loadPolicy()
	if err != nil {
		return 1, err
	}
	switch _, ok := pol.Classes[*class]; {
	case *class != "" && !ok: // no --class: no per-class estimate, just a usable agent
		return 2, fmt.Errorf("--class %q: want one of %v", *class, slices.Sorted(maps.Keys(pol.Classes)))
	case *self != "" && !agentRe.MatchString(*self):
		return 2, fmt.Errorf("--self %q: want <service>/<account>", *self)
	case !nsRe.MatchString(*ns):
		return 2, fmt.Errorf("--ns %q: want %s", *ns, nsRe)
	}
	mode, modeSource, err := resolveMode(*modeFlag, *ns)
	if err != nil {
		return 1, err
	}
	if pol.Rules, err = modeRules(pol, mode); err != nil {
		return 2, err
	}
	addr := cadAddr(pol, mode)
	out := PlaceOutput{Rule: -1, Mode: mode, ModeSource: modeSource, CadAddr: addr, Reason: []string{}}
	switch ready, err := cadReady(addr); {
	case err != nil:
		return 1, err
	case !ready: // cad restarted and has no usage yet: try again shortly instead of placing blind
		out.Reason = []string{"cad not ready"}
		out.DeferUntil = time.Now().Add(cadNotReadyDefer).UTC().Format(time.RFC3339)
		return 0, printJSON(w, out)
	}
	var usage map[string]AgentUsage
	var capacity struct {
		Slots int `json:"slots"`
	}
	if err := cadGet(addr, "usage", *ns, &usage); err != nil {
		return 1, err
	}
	if err := cadGet(addr, "capacity", *ns, &capacity); err != nil {
		return 1, err
	}
	p, status, until := place(pol, usableUsage(usage, time.Now()), PlaceSpec{Class: *class, Self: *self, Exclude: strings.Split(*exclude, ",")}, capacity.Slots)
	out.Agent, out.Computer = p.Agent, p.Computer
	if p.Reason != nil {
		out.Reason = p.Reason
	}
	switch status {
	case http.StatusOK:
		out.Rule = p.Rule
		if strings.Contains(strings.Join(p.Runner.Cmd, " "), "{session}") { // registry only: no git/gh
			reg, err := readNS(*ns)
			if err != nil {
				return 1, err
			}
			p.Runner.Session = cloudSession(reg)
		}
		if p.Runner.Stdin != "" && !filepath.IsAbs(p.Runner.Stdin) { // policy.json stores it relative to itself
			p.Runner.Stdin = filepath.Join(filepath.Dir(policyFile()), p.Runner.Stdin)
		}
		out.Runner = *p.Runner
	case http.StatusConflict: // every fitting agent is over a usage window
		out.DeferUntil = until.UTC().Format(time.RFC3339)
	} // else: no rule fits (422) - out already has rule -1, runner {}, defer_until ""
	return 0, printJSON(w, out)
}

// cadGet decodes cad's GET /v1/<topic>?ns= into v. 404 = not collected yet: v stays empty,
// as the former in-cad place treated it (usage unknown, 0 slots).
func cadGet(addr, topic, ns string, v interface{}) error {
	req, err := http.NewRequest("GET", "http://"+addr+"/v1/"+topic+"?ns="+ns, nil)
	if err != nil {
		return err
	}
	if t := os.Getenv("CAD_TOKEN"); t != "" {
		req.Header.Set("Authorization", "Bearer "+t)
	}
	res, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	if err != nil {
		return fmt.Errorf("cad unreachable at %s: %v", addr, err)
	}
	defer res.Body.Close()
	switch res.StatusCode {
	case http.StatusOK:
		return json.NewDecoder(res.Body).Decode(v)
	case http.StatusNotFound:
		return nil
	}
	b, _ := io.ReadAll(io.LimitReader(res.Body, 512))
	return fmt.Errorf("cad GET /v1/%s: %s: %s", topic, res.Status, b)
}

func printJSON(w io.Writer, v interface{}) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err == nil {
		_, err = fmt.Fprintf(w, "%s\n", b)
	}
	return err
}

const cadNotReadyDefer = 2 * time.Minute

var cadRetryEvery = 2 * time.Second // tests shorten it

// cadReady asks GET /healthz?ready: 200 = ready, 503 = up but no usage yet. A connection error
// (cad restarting) is retried 3 times cadRetryEvery apart before giving up.
func cadReady(addr string) (bool, error) {
	c := &http.Client{Timeout: 5 * time.Second}
	for i := 0; ; i++ {
		res, err := c.Get("http://" + addr + "/healthz?ready")
		if err != nil {
			if i < 3 {
				time.Sleep(cadRetryEvery)
				continue
			}
			return false, fmt.Errorf("cad unreachable at %s: %v", addr, err)
		}
		res.Body.Close()
		switch res.StatusCode {
		case http.StatusOK:
			return true, nil
		case http.StatusServiceUnavailable:
			return false, nil
		}
		return false, fmt.Errorf("cad GET /healthz?ready: %s", res.Status)
	}
}
