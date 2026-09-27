package main

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSlots(t *testing.T) {
	two := 2
	g := Gate{MemoryMB: 1500, CPUs: 1, ReserveMB: 2048}
	cases := []struct {
		free, cpus int
		g          Gate
		env        string
		want       int
	}{
		{2048 + 4500, 8, g, "", 3},                          // memory bound
		{2048 + 15000, 4, g, "", 4},                         // cpu bound
		{1000, 8, g, "", 0},                                 // below reserve clamps to 0
		{2048 + 15000, 8, Gate{1500, 1, 2048, &two}, "", 2}, // maxSlots
		{1000, 1, g, "7", 7},                                // CAD_SLOTS wins
		{2048 + 15000, 8, Gate{}, "", 0},                    // no policy
	}
	for i, c := range cases {
		t.Setenv("CAD_SLOTS", c.env)
		if got := slots(c.free, c.cpus, c.g); got != c.want {
			t.Errorf("case %d: slots=%d want %d", i, got, c.want)
		}
	}
}

func get(t *testing.T, url, token string) *http.Response {
	req, _ := http.NewRequest("GET", url, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestAuth(t *testing.T) {
	srv := httptest.NewServer(newServer(newHub(), "s3cret"))
	defer srv.Close()
	for _, c := range []struct {
		path, token string
		want        int
	}{{"/healthz", "", 200}, {"/v1/meta", "", 401}, {"/v1/meta", "wrong", 401}, {"/v1/meta", "s3cret", 200}} {
		if res := get(t, srv.URL+c.path, c.token); res.StatusCode != c.want {
			t.Errorf("%s token=%q: %d want %d", c.path, c.token, res.StatusCode, c.want)
		}
	}
}

func TestCapacityShape(t *testing.T) {
	h := newHub()
	v, err := collectCapacity()
	if err != nil {
		t.Fatal(err)
	}
	h.publish("capacity", v)
	srv := httptest.NewServer(newServer(h, ""))
	defer srv.Close()
	var m map[string]interface{}
	if err := json.NewDecoder(get(t, srv.URL+"/v1/capacity", "").Body).Decode(&m); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"host", "collectedAt", "memTotalMB", "memFreeMB", "cpus", "load1", "slots"} {
		if _, ok := m[k]; !ok {
			t.Errorf("missing %s in %v", k, m)
		}
	}
	if m["memTotalMB"].(float64) <= 0 || m["memFreeMB"].(float64) <= 0 {
		t.Errorf("memory not read: %v", m)
	}
}

func TestQuotaAndEvents(t *testing.T) {
	h := newHub()
	srv := httptest.NewServer(newServer(h, ""))
	defer srv.Close()

	res := get(t, srv.URL+"/v1/events?topics=quota", "")
	defer res.Body.Close()
	rd := bufio.NewReader(res.Body)
	next := func() Event {
		for {
			line, err := rd.ReadString('\n')
			if err != nil {
				t.Fatal(err)
			}
			if strings.HasPrefix(line, "data: ") {
				var e Event
				json.Unmarshal([]byte(line[6:]), &e)
				return e
			}
		}
	}
	if e := next(); e.Topic != "quota" || string(e.Data) != "[]" {
		t.Fatalf("snapshot: %+v", e)
	}

	h.publish("workers", []struct{}{}) // unchanged: must not emit
	body := strings.NewReader(`{"state":"exhausted","resetAt":"2026-01-01T00:00:00Z"}`)
	pr, err := http.Post(srv.URL+"/v1/quota/codex-bot", "application/json", body)
	if err != nil || pr.StatusCode != 204 {
		t.Fatalf("post: %v %v", err, pr)
	}
	done := make(chan Event)
	go func() { done <- next() }()
	select {
	case e := <-done:
		if e.Topic != "quota" || !strings.Contains(string(e.Data), `"codex-bot"`) {
			t.Fatalf("change event: %+v", e)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no change event")
	}

	var qs []Quota
	json.NewDecoder(get(t, srv.URL+"/v1/quota", "").Body).Decode(&qs)
	if len(qs) != 1 || qs[0].State != "exhausted" || qs[0].LastHitAt == nil || qs[0].ResetAt == nil || qs[0].Source != "api" {
		t.Fatalf("quota: %+v", qs)
	}
	if pr, _ := http.Post(srv.URL+"/v1/quota/x", "application/json", strings.NewReader(`{"state":"bogus"}`)); pr.StatusCode != 400 {
		t.Errorf("bad state: %d", pr.StatusCode)
	}
}
