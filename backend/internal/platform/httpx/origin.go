package httpx

import (
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/shared/apperr"
)

// RequireSameOrigin protects endpoints authenticated by cookie from requests
// started by other sites.
func RequireSameOrigin(r *http.Request) error {
	denied := apperr.New(apperr.KindForbidden, "ORIGIN_NOT_ALLOWED", "This request is not allowed from another site.")
	origin, err := url.Parse(r.Header.Get("Origin"))
	if err != nil || origin.Host == "" || !strings.EqualFold(origin.Hostname(), Hostname(r.Host)) {
		return denied
	}
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" {
		return denied
	}
	return nil
}

// Hostname removes the port from a host header value.
func Hostname(hostport string) string {
	if host, _, err := net.SplitHostPort(hostport); err == nil {
		return host
	}
	return hostport
}
