package middleware

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
)

func TestResolveClientIP(t *testing.T) {
	trusted := []netip.Prefix{netip.MustParsePrefix("172.16.0.0/12")}
	tests := map[string]struct {
		remote, forwarded, want string
	}{
		"direct connection":              {"203.0.113.9:4000", "", "203.0.113.9"},
		"untrusted peer cannot spoof":    {"203.0.113.9:4000", "198.51.100.1", "203.0.113.9"},
		"trusted proxy":                  {"172.18.0.5:4000", "198.51.100.1", "198.51.100.1"},
		"forged left value is ignored":   {"172.18.0.5:4000", "1.2.3.4, 198.51.100.1", "198.51.100.1"},
		"chain of trusted proxies":       {"172.18.0.5:4000", "198.51.100.1, 172.18.0.9", "198.51.100.1"},
		"malformed value falls back":     {"172.18.0.5:4000", "not-an-ip", "172.18.0.5"},
		"proxy without forwarded header": {"172.18.0.5:4000", "", "172.18.0.5"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = tt.remote
			if tt.forwarded != "" {
				req.Header.Set("X-Forwarded-For", tt.forwarded)
			}
			if got := resolveClientIP(req, trusted); got != tt.want {
				t.Errorf("resolveClientIP() = %q, want %q", got, tt.want)
			}
		})
	}
}
