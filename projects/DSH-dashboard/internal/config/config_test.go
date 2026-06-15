package config

import "testing"

func TestExternalOrigin(t *testing.T) {
	cases := []struct {
		name   string
		origin string
		want   string
	}{
		{"single localhost", "http://localhost:9090", "http://localhost:9090"},
		{"localhost then external", "http://localhost:9090,https://dsh-1.your-tailnet.ts.net", "https://dsh-1.your-tailnet.ts.net"},
		{"external then localhost", "https://dsh-1.your-tailnet.ts.net,http://localhost:9090", "https://dsh-1.your-tailnet.ts.net"},
		{"127.0.0.1 is loopback", "http://127.0.0.1:9090,https://x.ts.net", "https://x.ts.net"},
		{"ipv6 loopback", "http://[::1]:9090,https://x.ts.net", "https://x.ts.net"},
		{"spaces trimmed", " http://localhost:9090 , https://x.ts.net ", "https://x.ts.net"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg := &Config{Origin: c.origin}
			if got := cfg.ExternalOrigin(); got != c.want {
				t.Errorf("ExternalOrigin(%q) = %q, want %q", c.origin, got, c.want)
			}
		})
	}
}
