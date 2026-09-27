package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCLIGet(t *testing.T) {
	h := newHub()
	h.publish("usage", UsageMap{"claude/a": {FiveHour: &UsageWindow{UsedPct: 7}}})
	srv := httptest.NewServer(newServer(h, "tok"))
	defer srv.Close()
	t.Setenv("CAD_ADDR", strings.TrimPrefix(srv.URL, "http://"))
	t.Setenv("CAD_TOKEN", "tok")

	out, code := run(t, "", "get", "usage", "-ns", "dev")
	if code != 0 || !strings.HasPrefix(out, `{"claude/a":{"fiveHour":{"usedPct":7,`) {
		t.Fatalf("get usage: %d %s", code, out)
	}
	if out, code = run(t, "", "get", "--ns", "dev", "meta"); code != 0 || !strings.Contains(out, `"usage":{"claude/a"`) || !strings.Contains(out, `"quota":[]`) {
		t.Fatalf("get meta: %d %s", code, out)
	}
	if out, code = run(t, "", "get", "nope", "-ns", "dev"); code != 1 || !strings.Contains(out, "404") {
		t.Fatalf("unknown topic: %d %s", code, out)
	}
	t.Setenv("CAD_TOKEN", "bad")
	if out, code = run(t, "", "get", "meta", "-ns", "dev"); code != 1 || !strings.Contains(out, "401") {
		t.Fatalf("bad token: %d %s", code, out)
	}

	// Missing/bad ns: usage, exit 2, and no request reaches the daemon.
	hit := false
	stub := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hit = true }))
	defer stub.Close()
	t.Setenv("CAD_ADDR", strings.TrimPrefix(stub.URL, "http://"))
	for _, args := range [][]string{{"get", "meta"}, {"get", "meta", "-ns", ""}, {"get", "meta", "-ns", "Bad/NS"}} {
		if out, code := run(t, "", args...); code != 2 || !strings.Contains(out, "usage:") {
			t.Errorf("%v: %d %s", args, code, out)
		}
	}
	if hit {
		t.Error("request sent without ns")
	}

	stub.Close()
	if out, code := run(t, "", "get", "meta", "-ns", "dev"); code != 1 || !strings.Contains(out, "unreachable") {
		t.Fatalf("down: %d %s", code, out)
	}
}
