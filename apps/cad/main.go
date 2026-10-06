// cad serves host metadata (policy, capacity, workers, quota) over HTTP + SSE. It holds no task state.
package main

import (
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"time"
)

func main() {
	if isCLI(os.Args[1:]) {
		var stdin io.Reader
		if st, err := os.Stdin.Stat(); err == nil && st.Mode()&os.ModeCharDevice == 0 {
			stdin = os.Stdin // piped
		}
		os.Exit(runCLI(os.Args[1:], stdin, os.Stdout, os.Stderr))
	}
	addr := os.Getenv("CAD_ADDR")
	if addr == "" {
		addr = "127.0.0.1:7878"
	}
	token := os.Getenv("CAD_TOKEN")
	if token == "" && !isLoopback(addr) {
		log.Fatalf("cad: refusing to bind non-loopback %s without CAD_TOKEN", addr)
	}
	h := newHub()
	h.loadSnapshot()
	go h.run(collectors, 250*time.Millisecond, nil)
	go h.runQuotaResets(pollEvery)
	log.Printf("cad: listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, newServer(h, token)))
}

func isLoopback(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return host == "localhost" || (ip != nil && ip.IsLoopback())
}
