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
	"regexp"
	"slices"
	"time"
)

const usageText = `usage:
  orchd place --class <c> [--self <service/account>] [--ns default] [--mode <m>]
      print {agent, computer, rule, reason, runner, mode, modeSource, cadAddr}: the first rule of the
      mode's rule list with a usable agent (rule = index in that list)
  orchd mode set <m> [--ns <ns>] [--by <who>]   persist a mode (no --ns = all namespaces)
  orchd mode clear [--ns <ns>]                  remove that mode file
  orchd mode show [--ns <ns>]                   print the effective mode and where it came from
      mode precedence: --mode > <state>/mode/<ns>.json > <state>/mode/_global.json > ORCHD_MODE > auto
      "auto" = policy.rules (local is the last resort); others = policy.modes.<m>.rules (urgent = local-first)
  orchd show [rules|classes|runners]    print policy sections (no arg = all three)
files (CWD-independent): app dir = $ORCHD_HOME > dir above orchd's bin/ (if it has policy.json) > .
  policy: ORCHD_POLICY > <app>/policy.json; state: ORCHD_STATE_DIR > <app>/state
env: ORCHD_MODE; CAD_ADDR (> mode cadAddr > top-level cadAddr for auto > 127.0.0.1:7878), CAD_TOKEN
exit codes:
  0  placed (or shown)
  1  cad unreachable / cad error / bad policy file
  2  bad input (unknown class or mode, bad --self or --ns, unknown command)
  3  deferred: every fitting agent is over a usage window; prints {defer_until, reason}
  4  no rule fits; prints {reason}
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
	modeFlag := fs.String("mode", "", "")
	if err := fs.Parse(args); err != nil || fs.NArg() > 0 {
		return 2, fmt.Errorf("place: bad args %q", args)
	}
	pol, err := loadPolicy()
	if err != nil {
		return 1, err
	}
	switch _, ok := pol.Classes[*class]; {
	case !ok:
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
	p, status, until := place(pol, usableUsage(usage, time.Now()), PlaceSpec{Class: *class, Self: *self}, capacity.Slots)
	switch status {
	case http.StatusOK:
		return 0, printJSON(w, struct {
			Placement
			Mode       string `json:"mode"`
			ModeSource string `json:"modeSource"`
			CadAddr    string `json:"cadAddr"`
		}{p, mode, modeSource, addr})
	case http.StatusConflict:
		return 3, printJSON(w, map[string]interface{}{"defer_until": until, "reason": p.Reason})
	}
	return 4, printJSON(w, map[string]interface{}{"reason": p.Reason})
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
