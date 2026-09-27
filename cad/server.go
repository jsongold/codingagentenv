package main

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// newServer routes /healthz and /v1/*. Any published topic is served at GET /v1/<topic>.
func newServer(h *hub, token string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			fmt.Fprintln(w, "ok")
			return
		}
		if token != "" && subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+token)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		p := strings.TrimPrefix(r.URL.Path, "/v1/")
		switch {
		case p == r.URL.Path:
			http.NotFound(w, r)
		case strings.HasPrefix(p, "quota/") && r.Method == http.MethodPost:
			h.postQuota(w, r, strings.TrimPrefix(p, "quota/"))
		case r.Method != http.MethodGet:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		case p == "events":
			h.serveEvents(w, r)
		case p == "meta":
			m := map[string]json.RawMessage{}
			for _, e := range h.current() {
				m[e.Topic] = e.Data
			}
			writeJSON(w, m)
		default:
			for _, e := range h.current() {
				if e.Topic == p {
					writeJSON(w, e.Data)
					return
				}
			}
			http.NotFound(w, r)
		}
	})
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func (h *hub) postQuota(w http.ResponseWriter, r *http.Request, reviewer string) {
	var q Quota
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&q); err != nil || reviewer == "" || strings.Contains(reviewer, "/") ||
		(q.State != "ok" && q.State != "exhausted") {
		http.Error(w, `want {"state":"ok|exhausted","resetAt":"RFC3339"?}`, http.StatusBadRequest)
		return
	}
	q.Reviewer, q.Source, q.LastHitAt = reviewer, "api", nil
	h.setQuota(q)
	w.WriteHeader(http.StatusNoContent)
}

func (h *hub) serveEvents(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	want := map[string]bool{}
	for _, t := range strings.Split(r.URL.Query().Get("topics"), ",") {
		if t != "" {
			want[t] = true
		}
	}
	ch, first := h.subscribe(r.Header.Get("Last-Event-ID"))
	defer h.unsubscribe(ch)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	send := func(e Event) {
		if len(want) == 0 || want[e.Topic] {
			b, _ := json.Marshal(e)
			fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", e.Rev, e.Topic, b)
		}
	}
	for _, e := range first {
		send(e)
	}
	fl.Flush()
	hb := time.NewTicker(15 * time.Second)
	defer hb.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case e, ok := <-ch:
			if !ok {
				return
			}
			send(e)
		case <-hb.C:
			fmt.Fprint(w, ": ping\n\n")
		}
		fl.Flush()
	}
}
