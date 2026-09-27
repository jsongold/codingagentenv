// cad serves host metadata (policy, capacity, workers, quota) over HTTP + SSE. It holds no task state.
package main

import (
	"log"
	"net"
	"net/http"
	"os"
	"time"
)

func main() {
	addr := os.Getenv("CAD_ADDR")
	if addr == "" {
		addr = "127.0.0.1:7878"
	}
	token := os.Getenv("CAD_TOKEN")
	if token == "" && !isLoopback(addr) {
		log.Fatalf("cad: refusing to bind non-loopback %s without CAD_TOKEN", addr)
	}
	h := newHub()
	h.collectAll()
	go h.run(2 * time.Second)
	go h.runQuotaResets(2 * time.Second)
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
