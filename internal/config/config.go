// Package config reads Dibs's settings from the environment. Dibs has no
// secrets; every setting is a plain value written in the compose file.
package config

import (
	"fmt"
	"net/url"
	"runtime/debug"
	"strings"
)

// Config is everything the server needs at startup.
type Config struct {
	Env            string   // "prod" or "dev"
	Version        string   // DIBS_VERSION, or "dev-<commit>" / "dev" for a local build
	Addr           string   // listen address, e.g. ":3180"
	PublicURL      *url.URL // the address players open; share links, cookies and origin checks use it
	ClientIPHeader string   // header carrying the real client IP behind a proxy; empty uses the connection's address
}

// DefaultAddr and DevAddr are the listen addresses when DIBS_ADDR isn't set.
const (
	DefaultAddr = ":3180"
	DevAddr     = ":3181"
)

// Load reads the settings through getenv (os.Getenv outside tests).
func Load(getenv func(string) string) (Config, error) {
	env := strings.ToLower(strings.TrimSpace(getenv("APP_ENV")))
	switch env {
	case "":
		env = "prod"
	case "prod", "dev":
	default:
		return Config{}, fmt.Errorf("config: APP_ENV=%q isn't valid: set it to prod or dev, or leave it out for prod", env)
	}

	version := strings.TrimSpace(getenv("DIBS_VERSION"))
	if version == "" {
		version = buildVersion()
	}

	addr := strings.TrimSpace(getenv("DIBS_ADDR"))
	if addr == "" {
		addr = DefaultAddr
		if env == "dev" {
			addr = DevAddr
		}
	}

	raw := strings.TrimSpace(getenv("DIBS_PUBLIC_URL"))
	if raw == "" {
		raw = "https://dibs.mobasity.com"
		if env == "dev" {
			raw = "http://localhost" + portOf(addr)
		}
	}
	public, err := url.Parse(raw)
	if err != nil || (public.Scheme != "http" && public.Scheme != "https") || public.Host == "" || public.User != nil {
		return Config{}, fmt.Errorf("config: DIBS_PUBLIC_URL=%q isn't valid: set it to the address players open, such as https://dibs.mobasity.com", raw)
	}
	public.Path, public.RawQuery, public.Fragment = "", "", ""

	return Config{
		Env:            env,
		Version:        version,
		Addr:           addr,
		PublicURL:      public,
		ClientIPHeader: strings.TrimSpace(getenv("DIBS_CLIENT_IP_HEADER")),
	}, nil
}

// SecureCookies reports whether cookies need the Secure flag (an https public URL).
func (c Config) SecureCookies() bool { return c.PublicURL.Scheme == "https" }

// HealthURL is where `dibs health` asks the running server, inside its container.
func (c Config) HealthURL() string { return "http://127.0.0.1" + portOf(c.Addr) + "/health" }

// portOf returns ":<port>" from a listen address such as ":3180" or "0.0.0.0:3180".
func portOf(addr string) string {
	if i := strings.LastIndex(addr, ":"); i >= 0 {
		return addr[i:]
	}
	return DefaultAddr
}

// buildVersion is "dev-<commit>" when the binary was built from a git
// checkout, otherwise "dev".
func buildVersion() string {
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, s := range info.Settings {
			if s.Key == "vcs.revision" && len(s.Value) >= 7 {
				return "dev-" + s.Value[:7]
			}
		}
	}
	return "dev"
}
