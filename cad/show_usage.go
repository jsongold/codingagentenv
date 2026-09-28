package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"text/tabwriter"
	"time"
)

// errShowUsageArgs: `cad show --usage` takes only --json and --local.
var errShowUsageArgs = fmt.Errorf("show --usage: unknown flag (only --json, --local allowed): %w", errUsage)

// usageCollector collects usage in-process; a var so tests can inject a fixture instead of
// running real claude/codex.
var usageCollector = collectUsage

// showUsageCmd implements `cad show --usage [--json] [--local]`: default output is a table sorted
// by agent, --json prints the raw usage map. Data comes from the running cad's "usage" topic
// (CAD_ADDR/CAD_TOKEN, ns "default"); if that is unreachable (or --local is given) it falls back to
// collecting directly in-process.
func showUsageCmd(args []string, w io.Writer) error {
	jsonOut, local := false, false
	for _, a := range args {
		switch a {
		case "--json":
			jsonOut = true
		case "--local":
			local = true
		default:
			return errShowUsageArgs
		}
	}
	p, err := loadPolicy()
	if err != nil {
		return err
	}
	m := fetchUsage(p, local)
	if jsonOut {
		return printJSON(w, m)
	}
	return printUsageTable(w, p.Agents, m, time.Now())
}

// fetchUsage tries the running cad first, unless local is set, and otherwise collects in-process.
func fetchUsage(p Policy, local bool) UsageMap {
	if !local {
		if b, err := cadGetRaw("usage", "default"); err == nil {
			var m UsageMap
			if err := json.Unmarshal(b, &m); err == nil {
				return m
			}
		}
		fmt.Fprintln(os.Stderr, "cad: cad not running: collected locally")
	}
	return usageCollector(p.Agents, usageEvery())
}

// printUsageTable prints one aligned row per agent in policyAgents plus any extra keys only in m,
// sorted by agent name. now is passed in so tests can fix the relative time columns.
func printUsageTable(w io.Writer, policyAgents []string, m UsageMap, now time.Time) error {
	agents := append([]string(nil), policyAgents...)
	seen := make(map[string]bool, len(agents))
	for _, a := range agents {
		seen[a] = true
	}
	for a := range m {
		if !seen[a] {
			agents = append(agents, a)
			seen[a] = true
		}
	}
	sort.Strings(agents)

	tw := tabwriter.NewWriter(w, 0, 2, 3, ' ', 0)
	fmt.Fprintln(tw, "AGENT\t5H\tRESETS(5H)\t7D\tRESETS(7D)\tFETCHED\tNOTE")
	for _, a := range agents {
		u, ok := m[a]
		if !ok {
			fmt.Fprintf(tw, "%s\t-\t-\t-\t-\t-\tusage unknown\n", a)
			continue
		}
		note := ""
		switch {
		case u.Error != "":
			note = u.Error
		case u.Stale:
			note = "stale"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			a,
			usagePct(u.FiveHour), usageResets(u.FiveHour, now),
			usagePct(u.SevenDay), usageResets(u.SevenDay, now),
			usageFetched(u.FetchedAt, now),
			note)
	}
	return tw.Flush()
}

func usagePct(win *UsageWindow) string {
	if win == nil {
		return "-"
	}
	return fmt.Sprintf("%.0f%%", win.UsedPct)
}

// usageResets shows a relative time ("15:04 (in 2h03m)") for resets within 24h, else a local
// date+time ("01-02 15:04").
func usageResets(win *UsageWindow, now time.Time) string {
	if win == nil {
		return "-"
	}
	t := win.ResetsAt.Local()
	if d := win.ResetsAt.Sub(now); d > 0 && d < 24*time.Hour {
		return fmt.Sprintf("%s (in %s)", t.Format("15:04"), usageDur(d))
	}
	return t.Format("01-02 15:04")
}

func usageDur(d time.Duration) string {
	h, m := int(d/time.Hour), int(d/time.Minute)%60
	if h > 0 {
		return fmt.Sprintf("%dh%02dm", h, m)
	}
	return fmt.Sprintf("%dm", m)
}

func usageFetched(t time.Time, now time.Time) string {
	if t.IsZero() {
		return "-"
	}
	d := now.Sub(t)
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	default:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
}
