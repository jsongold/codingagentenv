package main

// dispatch for runner mode "vm": hand the task to a stopped worker VM (deploy/gcp/create-worker.sh; runner.instance
// is a comma-separated list, one to three VMs of the same kind) through the Compute Engine REST API: pick the
// first TERMINATED one, put the task in its instance metadata (key worker-task), then start it. The VM's startup
// script (deploy/gcp/worker-startup.sh) reads the task on boot and runs the worker container; the VM powers itself
// off after the container exits. No gcloud and no ssh, so it also runs inside the cad image on cad-2.
// References (Compute Engine API v1):
//   instances.get / setMetadata (fingerprint; the item list is replaced whole) / start: "Only a stopped virtual
//     machine can be started" https://cloud.google.com/compute/docs/reference/rest/v1/instances
//   operations: https://cloud.google.com/compute/docs/reference/rest/v1/zoneOperations/get
//   token from the metadata server: https://cloud.google.com/compute/docs/access/authenticate-workloads
//   Spot: a stopped Spot VM may fail to start when the zone lacks capacity
//     https://cloud.google.com/compute/docs/instances/create-use-spot
//   states (STOPPING / PENDING_STOP -> TERMINATED) https://cloud.google.com/compute/docs/instances/instance-life-cycle

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// vmBudget bounds waiting out STOPPING, setMetadata and the start together; each HTTP call gets vmCall at most, so
// a dispatch returns within ~4 min (the Orchestrator's tool-call limit). Tests shorten them.
var (
	vmBudget = 170 * time.Second
	vmCall   = 30 * time.Second
	vmPoll   = 5 * time.Second
	// vmReserve: a worker-task younger than this counts as another dispatch still starting the VM.
	vmReserve = 5 * time.Minute
	sleep     = time.Sleep
)

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

type metaItem struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type gceInstance struct {
	Status   string `json:"status"`
	Metadata struct {
		Fingerprint string     `json:"fingerprint"`
		Items       []metaItem `json:"items"`
	} `json:"metadata"`
}

type gceOperation struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Error  *struct {
		Errors []struct{ Code, Message string } `json:"errors"`
	} `json:"error"`
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

// safeArg: values of the task line (space separated in metadata, read by the VM's startup script).
var safeArg = regexp.MustCompile(`^[A-Za-z0-9._/:@-]+$`)

// dispatchVM returns exit 5 ("computer unavailable") when every candidate VM is busy (not stopped) or none can be
// started in time, so the Orchestrator re-places with `orchd place --exclude <computer>`.
func dispatchVM(rn Runner, repo string, n int, w io.Writer) (int, error) {
	instances := strings.Split(rn.Instance, ",") // one kind (deploy/gcp/create-worker.sh), up to 3 VMs
	for k, v := range map[string]string{"zone": rn.Zone, "project": rn.Project, "image": rn.Image, "model": rn.Model, "repo": repo} {
		if !safeArg.MatchString(v) {
			return 2, fmt.Errorf("--placement: runner.%s %q: want %s", k, v, safeArg)
		}
	}
	for _, inst := range instances {
		if !safeArg.MatchString(inst) {
			return 2, fmt.Errorf("--placement: runner.instance %q: want %s", rn.Instance, safeArg)
		}
	}
	t0 := time.Now()
	deadline := t0.Add(vmBudget)
	unavailable := func(format string, a ...any) (int, error) {
		return 5, fmt.Errorf("computer unavailable: "+format, a...)
	}
	tok, err := accessToken()
	if err != nil {
		return unavailable("%v", err)
	}
	// usable: a TERMINATED instance is still not free if its startup-script predates worker-task, or a
	// worker-task younger than vmReserve means another dispatch is between its setMetadata and start.
	usable := func(cur gceInstance) (bool, string) {
		upgraded := false
		for _, it := range cur.Metadata.Items {
			switch it.Key {
			case "startup-script": // an old script (IAP ssh era) never reads worker-task: the task would not run
				upgraded = strings.Contains(it.Value, "worker-task")
			case "worker-task": // "<n>-<unix> ...": a fresh one is another dispatch between its setMetadata and start
				// ponytail: time-based reservation; a task that finished within vmReserve also reads as busy.
				id, _, _ := strings.Cut(it.Value, " ")
				if ts, err := strconv.ParseInt(id[strings.LastIndex(id, "-")+1:], 10, 64); err == nil && time.Since(time.Unix(ts, 0)) < vmReserve {
					return false, fmt.Sprintf("reserved by task %s", id)
				}
			}
		}
		if !upgraded {
			return false, "its startup-script does not read worker-task; update it (deploy/gcp/README.md)"
		}
		return true, ""
	}
	// selectCandidate returns the next TERMINATED-and-usable instance not in exclude (a later one may still be
	// free even if an earlier one is reserved or on an old script). If none, it waits out the first self-stopping
	// (STOPPING/PENDING_STOP) instance and re-checks it once TERMINATED -- another actor may have started it
	// meanwhile, which is still busy, not usable. name=="" with err==nil means no candidate remains right now.
	busy := []string{}
	selectCandidate := func(exclude map[string]bool) (name string, in gceInstance, err error) {
		var stopping string
		var stoppingIn gceInstance
		for _, inst := range instances {
			if exclude[inst] {
				continue
			}
			var cur gceInstance
			if err := gceCall(tok, rn, "GET", "instances/"+inst, nil, &cur); err != nil {
				return "", gceInstance{}, err // e.g. the VM does not exist yet
			}
			switch {
			case cur.Status == "TERMINATED":
				if ok, reason := usable(cur); ok {
					return inst, cur, nil
				} else {
					busy = append(busy, inst+": "+reason)
				}
			case cur.Status == "STOPPING" || cur.Status == "PENDING_STOP":
				if stopping == "" {
					stopping, stoppingIn = inst, cur
				}
			default:
				busy = append(busy, inst+" "+cur.Status)
			}
		}
		if stopping == "" {
			return "", gceInstance{}, nil
		}
		name, in = stopping, stoppingIn
		for in.Status == "STOPPING" || in.Status == "PENDING_STOP" {
			if time.Now().After(deadline) {
				return "", gceInstance{}, fmt.Errorf("%s still %s after %s", name, in.Status, vmBudget)
			}
			sleep(vmPoll)
			if err := gceCall(tok, rn, "GET", "instances/"+name, nil, &in); err != nil {
				return "", gceInstance{}, err
			}
		}
		if in.Status != "TERMINATED" { // e.g. another actor started it while we waited
			busy = append(busy, name+": became "+in.Status+" while waiting")
			return "", gceInstance{}, nil
		}
		if ok, reason := usable(in); !ok {
			busy = append(busy, name+": "+reason)
			return "", gceInstance{}, nil
		}
		return name, in, nil
	}
	// wait polls an operation until DONE; an operation error (e.g. ZONE_RESOURCE_POOL_EXHAUSTED) is returned.
	wait := func(op gceOperation) error {
		for op.Status != "DONE" {
			if time.Now().After(deadline) {
				return fmt.Errorf("operation %s not done within %s", op.Name, vmBudget)
			}
			sleep(vmPoll)
			if err := gceCall(tok, rn, "GET", "operations/"+op.Name, nil, &op); err != nil {
				return err
			}
		}
		if op.Error != nil && len(op.Error.Errors) > 0 {
			return fmt.Errorf("%s: %s", op.Error.Errors[0].Code, op.Error.Errors[0].Message)
		}
		return nil
	}
	// Pick a candidate and reserve it via setMetadata; the fingerprint makes a concurrent dispatch racing for the
	// same VM fail here (412) instead of overwriting the task. Retry with another candidate rather than giving up
	// the whole dispatch, so simultaneous tasks can use the added capacity.
	var name string
	var in gceInstance
	var task string
	var items []metaItem
	var op gceOperation
	exclude := map[string]bool{}
	for {
		var serr error
		name, in, serr = selectCandidate(exclude)
		if serr != nil {
			return unavailable("%v", serr)
		}
		if name == "" { // every instance of the kind is busy, reserved or on an old script
			return unavailable("all of %s busy: %s", rn.Instance, strings.Join(busy, ", "))
		}
		// The task: "<id> <issue> <repo> <model> <image>". The id lets the VM skip a task it already ran (reboot).
		task = fmt.Sprintf("%d-%d %d %s %s %s", n, t0.Unix(), n, repo, rn.Model, rn.Image)
		items = []metaItem{{"worker-task", task}}
		for _, it := range in.Metadata.Items {
			if it.Key != "worker-task" {
				items = append(items, it)
			}
		}
		op = gceOperation{}
		if err := gceCall(tok, rn, "POST", "instances/"+name+"/setMetadata", map[string]any{"fingerprint": in.Metadata.Fingerprint, "items": items}, &op); err != nil {
			if time.Now().After(deadline) {
				return unavailable("setMetadata %s: %v", name, err)
			}
			busy = append(busy, name+": setMetadata: "+err.Error())
			exclude[name] = true
			continue
		}
		if err := wait(op); err != nil {
			return unavailable("setMetadata %s: %v", name, err)
		}
		break
	}
	ipath := "instances/" + name
	op = gceOperation{}
	err = gceCall(tok, rn, "POST", ipath+"/start", nil, &op)
	if err == nil {
		err = wait(op)
	}
	if err != nil { // e.g. no Spot capacity in the zone, or a failed poll of an accepted start
		// Settle on the instance: already starting = started (else another computer would run the issue too).
		// Still stopped: take the task back (best effort, fresh fingerprint) so a later manual start does not run it.
		var now gceInstance
		gerr := gceCall(tok, rn, "GET", ipath, nil, &now)
		if gerr == nil && (now.Status == "PROVISIONING" || now.Status == "STAGING" || now.Status == "RUNNING") {
			err = nil
		} else if gerr == nil && now.Status == "TERMINATED" {
			items = items[:0]
			for _, it := range now.Metadata.Items {
				if it.Key != "worker-task" || it.Value != task {
					items = append(items, it)
				}
			}
			var cop gceOperation
			gceCall(tok, rn, "POST", ipath+"/setMetadata", map[string]any{"fingerprint": now.Metadata.Fingerprint, "items": items}, &cop)
		}
		if err != nil {
			return unavailable("start %s: %v", name, err)
		}
	}
	// zone/project: every candidate shares the runner's, so a monitor can run "orchd vm status" from this output alone.
	return 0, printJSON(w, map[string]any{"started": true, "instance": name, "zone": rn.Zone, "project": rn.Project,
		"container": "opencode-worker-" + strconv.Itoa(n), "task": task, "startSec": math.Round(time.Since(t0).Seconds()*10) / 10})
}

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
