package main

// wake: ADR-0015. A periodic nudge during mode sleep, sent to the cloud worker session so the CCO
// picks the next step itself. Outside sleep it is a no-op (print skipped, exit 0).

import (
	_ "embed"
	"flag"
	"fmt"
	"io"
)

//go:embed wake.md
var wakePrompt string

func wakeCmd(args []string, w io.Writer) (int, error) {
	fs := flag.NewFlagSet("wake", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	ns := fs.String("ns", "default", "")
	if err := fs.Parse(args); err != nil || fs.NArg() > 0 {
		return 2, fmt.Errorf("wake: bad args %q", args)
	}
	if !nsRe.MatchString(*ns) {
		return 2, fmt.Errorf("--ns %q: want %s", *ns, nsRe)
	}
	mode, _, err := resolveMode("", *ns)
	if err != nil {
		return 1, err
	}
	if mode != "sleep" {
		return 0, printJSON(w, map[string]any{"skipped": true, "mode": mode})
	}
	nsv, err := resolveNS(*ns, "", "")
	if err != nil {
		return 1, err
	}
	session := cloudSession(nsv)
	if session == "" {
		return 2, fmt.Errorf("no cloud session for ns %s: set CLAUDE_CLOUD_SESSION or cloudWorkerSession in %s (create one with `claude --cloud`)", *ns, namespacesFile())
	}
	return cloudSend(session, wakePrompt, w)
}
