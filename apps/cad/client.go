package main

import (
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"
)

// getCmd fetches /v1/<topic> (meta = every topic) from the running cad and copies the JSON to w.
// It is on the agents' hot path, so it does one request with a short timeout and nothing else.
func getCmd(args []string, w io.Writer) error {
	fs := flag.NewFlagSet("get", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	ns := fs.String("ns", "", "")
	if err := fs.Parse(args); err != nil { // flags before the topic
		return fmt.Errorf("%v: %w", err, errUsage)
	}
	topic := fs.Arg(0)
	if topic != "" {
		if err := fs.Parse(fs.Args()[1:]); err != nil || fs.NArg() > 0 { // and after it
			return fmt.Errorf("get %s: bad args: %w", topic, errUsage)
		}
	}
	switch {
	case topic == "":
		return errUsage
	case !nsRe.MatchString(*ns):
		return errNS
	}
	b, err := cadGetRaw(topic, *ns)
	if err != nil {
		return err
	}
	_, err = w.Write(b)
	return err
}

// cadGetRaw issues one GET /v1/<topic>?ns=<ns> against the running cad (CAD_ADDR/CAD_TOKEN) and
// returns the raw response body. Shared by `cad get` and `cad show --usage`.
func cadGetRaw(topic, ns string) ([]byte, error) {
	addr := os.Getenv("CAD_ADDR")
	if addr == "" {
		addr = "127.0.0.1:7878"
	}
	req, err := http.NewRequest("GET", "http://"+addr+"/v1/"+url.PathEscape(topic)+"?ns="+url.QueryEscape(ns), nil)
	if err != nil {
		return nil, err
	}
	if t := os.Getenv("CAD_TOKEN"); t != "" {
		req.Header.Set("Authorization", "Bearer "+t)
	}
	res, err := (&http.Client{Timeout: 2 * time.Second}).Do(req)
	if err != nil {
		return nil, fmt.Errorf("cad daemon unreachable at %s: %v", addr, err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(res.Body, 512))
		return nil, fmt.Errorf("GET /v1/%s: %s: %s", topic, res.Status, b)
	}
	return io.ReadAll(res.Body)
}
