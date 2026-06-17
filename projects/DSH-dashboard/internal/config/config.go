package config

import (
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Port          string
	DBPath        string
	AdminPassword string // first-run only; empty after bootstrap
	Origin        string // WebAuthn RP origin, e.g. http://localhost:8080
	PMPath        string // path to project-management directory (read-only)
	TodoPath      string // path to todo.txt file (read-write)
	VAPIDContact  string // mailto: address for VAPID (Web Push)
	TLSCert       string // path to TLS certificate file
	TLSKey        string // path to TLS private key file
	LLPURL        string // LLM-proxy data API base URL (loopback), e.g. http://localhost:4000
	LLPSocket     string // path to LLP's control socket (bind-mounted) for the token handshake
}

func Load() *Config {
	port := getenv("DSH_PORT", "8080")
	origin := getenv("DSH_ORIGIN", "http://localhost:"+port)
	return &Config{
		Port:          port,
		DBPath:        getenv("DSH_DB_PATH", "/data/dsh.db"),
		AdminPassword: os.Getenv("DSH_ADMIN_PASSWORD"),
		Origin:        origin,
		PMPath:        os.Getenv("DSH_PM_PATH"),
		TodoPath:      os.Getenv("DSH_TODO_PATH"),
		VAPIDContact:  getenv("DSH_VAPID_CONTACT", "mailto:admin@localhost"),
		TLSCert:       os.Getenv("DSH_TLS_CERT"),
		TLSKey:        os.Getenv("DSH_TLS_KEY"),
		LLPURL:        getenv("DSH_LLP_URL", "http://localhost:4000"),
		LLPSocket:     getenv("DSH_LLP_SOCKET", "/llp/control.sock"),
	}
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func (c *Config) PortInt() int {
	n, _ := strconv.Atoi(c.Port)
	return n
}

func (c *Config) Origins() []string {
	parts := strings.Split(c.Origin, ",")
	var origins []string
	for _, p := range parts {
		if s := strings.TrimSpace(p); s != "" {
			origins = append(origins, s)
		}
	}
	return origins
}

// ExternalOrigin returns the first configured origin whose host is not a
// loopback address — i.e. the URL a phone or other device actually uses
// (e.g. https://dsh-1.your-tailnet.ts.net). Device-enrollment links must point
// (e.g. https://dsh-1.your-tailnet.ts.net). Device-enrollment links must point
// here, never at localhost. Falls back to the first origin, then to Origin.
func (c *Config) ExternalOrigin() string {
	origins := c.Origins()
	for _, o := range origins {
		if !isLoopbackOrigin(o) {
			return o
		}
	}
	if len(origins) > 0 {
		return origins[0]
	}
	return c.Origin
}

func isLoopbackOrigin(origin string) bool {
	h := origin
	if i := strings.Index(h, "://"); i >= 0 {
		h = h[i+3:]
	}
	// Bracketed IPv6 literal, e.g. [::1]:9090 — take the address inside [].
	if strings.HasPrefix(h, "[") {
		if i := strings.Index(h, "]"); i >= 0 {
			h = h[1:i]
		}
	} else if i := strings.IndexAny(h, ":/"); i >= 0 {
		h = h[:i]
	}
	switch h {
	case "localhost", "127.0.0.1", "::1":
		return true
	}
	return false
}
