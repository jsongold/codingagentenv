// cad serves host metadata (policy, capacity, workers, quota) over HTTP + SSE. It holds no task state.
// `cad capacity` is a one-shot subcommand: it collects capacity once, prints the
// Capacity JSON to stdout, and exits 0 without starting the server.
package main

import (
	"encoding/json"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"time"
)

// runCapacityOnce collects capacity once and writes it as JSON to w.
func runCapacityOnce(w io.Writer) error {
	v, err := collectCapacity()
	if err != nil {
		return err
	}
	return json.NewEncoder(w).Encode(v)
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "capacity" {
		if err := runCapacityOnce(os.Stdout); err != nil {
			log.Fatalf("cad: capacity: %v", err)
		}
		return
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
	h.collectAll()
	go h.run(2 * time.Second)
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
