package config

import "testing"

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestLoadDefaults(t *testing.T) {
	c, err := Load(env(nil))
	if err != nil {
		t.Fatal(err)
	}
	if c.Env != "prod" || c.Addr != ":2600" || c.PublicURL.String() != "https://dibs.mobasity.com" || !c.SecureCookies() {
		t.Fatalf("unexpected defaults: %+v", c)
	}
	if c.HealthURL() != "http://127.0.0.1:2600/health" {
		t.Fatalf("health URL %q", c.HealthURL())
	}
}

func TestLoadDev(t *testing.T) {
	c, err := Load(env(map[string]string{"APP_ENV": "dev", "DIBS_ADDR": "127.0.0.1:2601"}))
	if err != nil {
		t.Fatal(err)
	}
	if c.PublicURL.String() != "http://localhost:2601" || c.SecureCookies() {
		t.Fatalf("dev public URL %q", c.PublicURL)
	}
}

func TestLoadRejects(t *testing.T) {
	for _, m := range []map[string]string{
		{"APP_ENV": "staging"},
		{"DIBS_PUBLIC_URL": "dibs.mobasity.com"},
		{"DIBS_PUBLIC_URL": "ftp://dibs.mobasity.com"},
		{"DIBS_PUBLIC_URL": "https://user:pw@dibs.mobasity.com"},
	} {
		if _, err := Load(env(m)); err == nil {
			t.Errorf("Load(%v) succeeded, want an error", m)
		}
	}
}

func TestLoadTrimsPublicPath(t *testing.T) {
	c, err := Load(env(map[string]string{"DIBS_PUBLIC_URL": "https://example.com/x?y=1"}))
	if err != nil {
		t.Fatal(err)
	}
	if c.PublicURL.String() != "https://example.com" {
		t.Fatalf("got %q", c.PublicURL)
	}
}
