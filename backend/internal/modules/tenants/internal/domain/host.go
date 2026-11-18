package domain

import (
	"net"
	"regexp"
	"strings"
)

// Domain kinds.
const (
	DomainSubdomain = "subdomain"
	DomainCustom    = "custom"
)

// VerificationPrefix is the DNS label that proves ownership of a custom domain.
const VerificationPrefix = "_ecommerce-verify."

const maxHostLength = 253

var hostLabelPattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

// NormalizeHost converts a Host header value to its canonical form. Addresses
// and malformed names are rejected, so they never reach the database.
func NormalizeHost(raw string) (string, bool) {
	host := strings.TrimSpace(raw)
	if withoutPort, _, err := net.SplitHostPort(host); err == nil {
		host = withoutPort
	}
	host = strings.ToLower(strings.TrimSuffix(host, "."))

	if host == "" || len(host) > maxHostLength || net.ParseIP(host) != nil {
		return "", false
	}
	labels := strings.Split(host, ".")
	if len(labels) < 2 {
		return "", false
	}
	for _, label := range labels {
		if !hostLabelPattern.MatchString(label) {
			return "", false
		}
	}
	return host, true
}

// SubdomainHost returns the platform subdomain of a store.
func SubdomainHost(slug, baseDomain string) string {
	return slug + "." + baseDomain
}

// ValidateCustomHost checks a domain supplied by a store. Names under the
// platform domain are refused, because they belong to other stores.
func ValidateCustomHost(raw, baseDomain string) (string, error) {
	host, ok := NormalizeHost(raw)
	if !ok {
		return "", fieldError("host", "invalid", "Enter a valid domain name.")
	}
	if host == baseDomain || strings.HasSuffix(host, "."+baseDomain) {
		return "", fieldError("host", "reserved", "This domain belongs to the platform.")
	}
	return host, nil
}
