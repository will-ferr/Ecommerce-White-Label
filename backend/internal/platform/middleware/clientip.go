package middleware

import (
	"net"
	"net/http"
	"net/netip"
	"slices"
	"strings"

	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/shared/reqctx"
)

// ClientIP records the address of the caller. X-Forwarded-For is only read when
// the connection comes from a trusted proxy, and it is walked from the right so
// a value forged by the client is never selected.
func ClientIP(trusted []netip.Prefix) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			reqctx.SetClientIP(r.Context(), resolveClientIP(r, trusted))
			next.ServeHTTP(w, r)
		})
	}
}

func resolveClientIP(r *http.Request, trusted []netip.Prefix) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	remote, err := netip.ParseAddr(host)
	if err != nil {
		return ""
	}
	remote = remote.Unmap()
	if !isTrusted(remote, trusted) {
		return remote.String()
	}

	hops := strings.Split(strings.Join(r.Header.Values("X-Forwarded-For"), ","), ",")
	for _, hop := range slices.Backward(hops) {
		addr, err := netip.ParseAddr(strings.TrimSpace(hop))
		if err != nil {
			return remote.String()
		}
		addr = addr.Unmap()
		if !isTrusted(addr, trusted) {
			return addr.String()
		}
	}
	return remote.String()
}

func isTrusted(addr netip.Addr, trusted []netip.Prefix) bool {
	return slices.ContainsFunc(trusted, func(prefix netip.Prefix) bool { return prefix.Contains(addr) })
}
