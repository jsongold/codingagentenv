package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func seedPolicy(t *testing.T) Policy {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", ".agent", "policy.json"))
	if err != nil {
		t.Fatal(err)
	}
	var p Policy
	if err := json.Unmarshal(b, &p); err != nil {
		t.Fatal(err)
	}
	return p
}

func spec(class, strategy string, mem, min int, maxCost float64, allow ...string) PlaceSpec {
	var s PlaceSpec
	s.Class, s.Placement.Strategy, s.Placement.Allow, s.Placement.MaxCostUSD = class, strategy, allow, maxCost
	s.Resources.MemoryMB, s.Resources.CPUs, s.Resources.TimeoutMin = mem, 1, min
	return s
}

func TestPlace(t *testing.T) {
	pol := seedPolicy(t)
	explicit := spec("light-edit", "cheap", 1024, 30, 0)
	explicit.Placement.Provider = "e2b"
	explicit.Resources.TimeoutMin = 120 // e2b maxMin 60
	ranked := spec("light-edit", "ranked", 1024, 30, 0, "e2b", "cloud-run-jobs", "gce-spot")
	ranked.Placement.Order = []string{"cloud-run-jobs", "e2b"}
	cases := []struct {
		name  string
		s     PlaceSpec
		slots int
		want  string // computer, "" = 422
	}{
		{"local-first", spec("light-edit", "cheap", 1024, 30, 0), 5, "local"},
		{"local no slots -> cheapest free non-preemptible", spec("light-edit", "cheap", 1024, 30, 0), 0, "claude-cloud"},
		{"needs-db excludes cloud-run", spec("needs-db", "cheap", 1024, 30, 0, "cloud-run-jobs", "e2b"), 0, "e2b"},
		{"needs-db only cloud-run -> 422", spec("needs-db", "cheap", 1024, 30, 0, "cloud-run-jobs"), 0, ""},
		{"maxCostUSD filters e2b", spec("light-edit", "cheap", 1024, 30, 0.02, "e2b", "gce-spot"), 0, "gce-spot"},
		{"maxCostUSD filters all -> 422", spec("light-edit", "cheap", 1024, 30, 0.0001, "e2b", "gce-spot"), 0, ""},
		{"cheap picks preemptible when cheaper", spec("light-edit", "cheap", 1024, 30, 0, "e2b", "gce-spot"), 0, "gce-spot"},
		{"safe excludes preemptible", spec("light-edit", "safe", 1024, 30, 0, "e2b", "gce-spot"), 0, "e2b"},
		{"fast by coldStart", spec("light-edit", "fast", 1024, 30, 0, "gce-spot", "cloud-run-jobs", "e2b"), 0, "e2b"},
		{"ranked by order", ranked, 0, "cloud-run-jobs"},
		{"explicit provider infeasible -> 422", explicit, 5, ""},
		{"unknown class -> 422", spec("nope", "cheap", 1024, 30, 0), 5, ""},
	}
	for _, c := range cases {
		got, ok := place(pol, c.s, c.slots)
		if ok != (c.want != "") || got.Computer != c.want {
			t.Errorf("%s: got %q ok=%v reasons=%v, want %q", c.name, got.Computer, ok, got.Reason, c.want)
		}
		if ok && got.Agent != "claude/default" {
			t.Errorf("%s: agent %q", c.name, got.Agent)
		}
	}
}

func TestPlaceTieBreakAndDeterminism(t *testing.T) {
	pol := seedPolicy(t)
	pol.Agents = []string{"claude/b", "codex/x", "claude/a"}
	pol.Computers["twin"] = pol.Computers["e2b"] // same key as e2b; "e2b" < "twin"
	s := spec("light-edit", "safe", 1024, 30, 0, "twin", "e2b")
	first, ok := place(pol, s, 0)
	if !ok || first.Agent != "claude/a" || first.Computer != "e2b" {
		t.Fatalf("got %+v", first)
	}
	for i := 0; i < 20; i++ {
		if again, _ := place(pol, s, 0); !reflect.DeepEqual(first, again) {
			t.Fatalf("run %d: %+v != %+v", i, again, first)
		}
	}
}

func TestPostPlace(t *testing.T) {
	t.Setenv("CAD_POLICY", filepath.Join("..", ".agent", "policy.json"))
	h := newHub()
	h.publish("capacity", Capacity{Slots: 2})
	srv := httptest.NewServer(newServer(h, "tok"))
	defer srv.Close()
	post := func(q, body, token string) *http.Response {
		req, _ := http.NewRequest("POST", srv.URL+"/v1/place"+q, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	ok := `{"class":"light-edit","resources":{"memoryMB":1024,"cpus":1,"timeoutMin":30},"placement":{"strategy":"cheap"}}`
	for _, c := range []struct {
		q, body, token string
		want           int
	}{
		{"?ns=a", ok, "tok", 200},
		{"", ok, "tok", 400},
		{"?ns=a", ok, "bad", 401},
		{"?ns=a", `{"class":"light-edit","placement":{"strategy":"weird"}}`, "tok", 400},
		{"?ns=a", `{"class":"needs-db","resources":{"memoryMB":1024,"cpus":1},"placement":{"allow":["cloud-run-jobs"]}}`, "tok", 422},
	} {
		res := post(c.q, c.body, c.token)
		if res.StatusCode != c.want {
			t.Errorf("%s %s: %d want %d", c.q, c.body, res.StatusCode, c.want)
		}
		if c.want == 200 {
			var p Placement
			json.NewDecoder(res.Body).Decode(&p)
			if p.Computer != "local" || p.Agent != "claude/default" {
				t.Errorf("200 body: %+v", p)
			}
		}
		res.Body.Close()
	}
}
