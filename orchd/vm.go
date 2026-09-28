package main

// dispatch for runner mode "vm": hand the task to a stopped worker VM (deploy/gcp/create-worker.sh) through the
// Compute Engine REST API: put it in the instance metadata (key worker-task), then start the VM. The VM's startup
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
	sleep    = time.Sleep
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

// dispatchVM returns exit 5 ("computer unavailable") when the VM is busy (not stopped) or cannot be started in
// time, so the Orchestrator re-places with `orchd place --exclude <computer>`.
func dispatchVM(rn Runner, repo string, n int, w io.Writer) (int, error) {
	for k, v := range map[string]string{"instance": rn.Instance, "zone": rn.Zone, "project": rn.Project, "image": rn.Image, "model": rn.Model, "repo": repo} {
		if !safeArg.MatchString(v) {
			return 2, fmt.Errorf("--placement: runner.%s %q: want %s", k, v, safeArg)
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
	ipath := "instances/" + rn.Instance
	var in gceInstance
	for {
		if err := gceCall(tok, rn, "GET", ipath, nil, &in); err != nil {
			return unavailable("%v", err) // e.g. the VM does not exist yet
		}
		if in.Status != "STOPPING" && in.Status != "PENDING_STOP" { // self-stop in progress: wait it out
			break
		}
		if time.Now().After(deadline) {
			return unavailable("%s still %s after %s", rn.Instance, in.Status, vmBudget)
		}
		sleep(vmPoll)
	}
	if in.Status != "TERMINATED" { // RUNNING / PROVISIONING / STAGING: another task has it
		return unavailable("%s is %s (busy)", rn.Instance, in.Status)
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
	// The task: "<id> <issue> <repo> <model> <image>". The id lets the VM skip a task it already ran (reboot).
	task := fmt.Sprintf("%d-%d %d %s %s %s", n, t0.Unix(), n, repo, rn.Model, rn.Image)
	items := []metaItem{{"worker-task", task}}
	for _, it := range in.Metadata.Items {
		if it.Key != "worker-task" {
			items = append(items, it)
		}
	}
	var op gceOperation
	// The fingerprint makes a concurrent dispatch to the same VM fail here (412) instead of overwriting the task.
	if err := gceCall(tok, rn, "POST", ipath+"/setMetadata", map[string]any{"fingerprint": in.Metadata.Fingerprint, "items": items}, &op); err != nil {
		return unavailable("setMetadata %s: %v", rn.Instance, err)
	}
	if err := wait(op); err != nil {
		return unavailable("setMetadata %s: %v", rn.Instance, err)
	}
	op = gceOperation{}
	if err := gceCall(tok, rn, "POST", ipath+"/start", nil, &op); err != nil {
		return unavailable("start %s: %v", rn.Instance, err)
	}
	if err := wait(op); err != nil {
		return unavailable("start %s: %v", rn.Instance, err) // e.g. no Spot capacity in the zone
	}
	return 0, printJSON(w, map[string]any{"started": true, "instance": rn.Instance, "container": "opencode-worker-" + strconv.Itoa(n),
		"task": task, "startSec": math.Round(time.Since(t0).Seconds()*10) / 10})
}
