package main

// orchd vm status: one worker VM's status through the Compute Engine REST API (no gcloud, no ssh), so it
// also runs inside the cad image on cad-2.
// References (Compute Engine API v1):
//   instances.get https://cloud.google.com/compute/docs/reference/rest/v1/instances
//   token from the metadata server: https://cloud.google.com/compute/docs/access/authenticate-workloads

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"
)

var vmCall = 30 * time.Second // per HTTP call

// computeURL: ORCHD_COMPUTE_URL overrides the API base (tests use an httptest server).
func computeURL() string {
	if u := os.Getenv("ORCHD_COMPUTE_URL"); u != "" {
		return strings.TrimSuffix(u, "/")
	}
	return "https://compute.googleapis.com/compute/v1"
}

// accessToken: the default service account's token from the GCE metadata server (host overridable with
// GCE_METADATA_HOST, as in Google's client libraries); off GCE (the Mac) `gcloud auth print-access-token`.
var accessToken = func() (string, error) {
	host := os.Getenv("GCE_METADATA_HOST")
	if host == "" {
		host = "metadata.google.internal"
	}
	req, _ := http.NewRequest("GET", "http://"+host+"/computeMetadata/v1/instance/service-accounts/default/token", nil)
	req.Header.Set("Metadata-Flavor", "Google")
	var tok struct {
		AccessToken string `json:"access_token"`
	}
	if resp, err := (&http.Client{Timeout: 3 * time.Second}).Do(req); err == nil {
		defer resp.Body.Close()
		if resp.StatusCode == 200 && json.NewDecoder(resp.Body).Decode(&tok) == nil && tok.AccessToken != "" {
			return tok.AccessToken, nil
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), vmCall)
	defer cancel()
	out, err := shellCtx(ctx, "", "gcloud", "auth", "print-access-token")
	if err != nil {
		return "", fmt.Errorf("no access token (metadata server, then gcloud): %v", err)
	}
	return strings.TrimSpace(out), nil
}

type gceInstance struct {
	Status string `json:"status"`
}

// gceCall sends one request to <computeURL>/projects/<p>/zones/<z>/<path> and decodes the JSON answer into out.
func gceCall(tok string, rn Runner, method, path string, body, out any) error {
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	ctx, cancel := context.WithTimeout(context.Background(), vmCall)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, computeURL()+"/projects/"+rn.Project+"/zones/"+rn.Zone+"/"+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("%s %s: %s %s", method, path, resp.Status, bytes.TrimSpace(b[:min(300, len(b))]))
	}
	return json.Unmarshal(b, out)
}

// safeArg: what an instance / zone / project name may contain (they go into the API path).
var safeArg = regexp.MustCompile(`^[A-Za-z0-9._/:@-]+$`)

// vmCmd: "orchd vm status --instance <i> --zone <z> --project <p>", the only vm subcommand so far.
func vmCmd(args []string, w io.Writer) (int, error) {
	if len(args) == 0 || args[0] != "status" {
		return 2, fmt.Errorf("vm: want status")
	}
	fs := flag.NewFlagSet("vm status", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	instance, zone, project := fs.String("instance", "", ""), fs.String("zone", "", ""), fs.String("project", "", "")
	if err := fs.Parse(args[1:]); err != nil || fs.NArg() > 0 {
		return 2, fmt.Errorf("vm status: bad args %q", args[1:])
	}
	return vmStatus(Runner{Instance: *instance, Zone: *zone, Project: *project}, w)
}

// vmStatus prints one worker VM's current status (read-only: instances.get), replacing `gcloud compute instances
// describe` in skills/orchestrate: the monitor Subagent for runner "vm" polls this to tell a preempted or finished
// VM (TERMINATED, with no PR yet) from one still working (issue #67).
func vmStatus(rn Runner, w io.Writer) (int, error) {
	for k, v := range map[string]string{"instance": rn.Instance, "zone": rn.Zone, "project": rn.Project} {
		if !safeArg.MatchString(v) {
			return 2, fmt.Errorf("--%s %q: want %s", k, v, safeArg)
		}
	}
	tok, err := accessToken()
	if err != nil {
		return 1, err
	}
	var in gceInstance
	if err := gceCall(tok, rn, "GET", "instances/"+rn.Instance, nil, &in); err != nil {
		return 1, fmt.Errorf("%s: %v", rn.Instance, err)
	}
	return 0, printJSON(w, map[string]any{"instance": rn.Instance, "status": in.Status})
}
