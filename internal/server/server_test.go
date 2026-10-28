package server

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lsariol/dibs/internal/config"
)

func testConfig(t *testing.T) config.Config {
	t.Helper()
	c, err := config.Load(func(k string) string {
		return map[string]string{"APP_ENV": "dev", "DIBS_VERSION": "test"}[k]
	})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	s, err := New(testConfig(t), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return ts
}

func get(t *testing.T, url string) (*http.Response, string) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, string(b)
}

func TestLanding(t *testing.T) {
	ts := newTestServer(t)
	resp, body := get(t, ts.URL+"/")
	if resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	for _, want := range []string{">dibs<", "Create Lobby", "Join Lobby", `href="/new"`, `href="/join"`} {
		if !strings.Contains(body, want) {
			t.Errorf("landing page lacks %q", want)
		}
	}
	if resp.Header.Get("Content-Security-Policy") == "" || resp.Header.Get("X-Frame-Options") != "DENY" {
		t.Error("security headers missing")
	}
}

func TestHealth(t *testing.T) {
	ts := newTestServer(t)
	resp, body := get(t, ts.URL+"/health")
	if resp.StatusCode != 200 || !strings.Contains(body, `"status":"ok"`) || !strings.Contains(body, `"version":"test"`) {
		t.Fatalf("health: %d %s", resp.StatusCode, body)
	}
}

func TestStatic(t *testing.T) {
	ts := newTestServer(t)
	resp, body := get(t, ts.URL+"/static/test/css/dibs.css")
	if resp.StatusCode != 200 || !strings.Contains(body, "--accent") {
		t.Fatalf("css: %d", resp.StatusCode)
	}
	if resp, _ := get(t, ts.URL+"/static/test/css/"); resp.StatusCode != 404 {
		t.Fatalf("directory listing: %d, want 404", resp.StatusCode)
	}
}

func TestUnknownPath(t *testing.T) {
	ts := newTestServer(t)
	if resp, _ := get(t, ts.URL+"/nope"); resp.StatusCode != 404 {
		t.Fatalf("status %d, want 404", resp.StatusCode)
	}
}
