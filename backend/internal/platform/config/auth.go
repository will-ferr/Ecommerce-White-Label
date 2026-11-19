package config

import (
	"bytes"
	"encoding/base64"
	"net/netip"
	"net/url"
	"strings"

	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/security"
)

// developmentKeyPrefix marks the sample keys of .env.example.
var developmentKeyPrefix = []byte("development-only-")

const keyBytes = 32

// Auth configures authentication and its supporting services.
type Auth struct {
	SigningSeed      []byte
	MFAKey           []byte
	CookieSecure     bool
	TrustedProxies   []netip.Prefix
	RateLimitEnabled bool
	Links            PublicLinks
	SMTP             SMTP
}

// PublicLinks builds the addresses sent in e-mails.
type PublicLinks struct {
	StoreScheme string
	StorePort   string
	AdminURL    string
	ConsoleURL  string
}

// SMTP configures the mail server.
type SMTP struct {
	Addr       string
	From       string
	Username   string
	Password   security.Secret
	RequireTLS bool
}

func (r *reader) key(name string) []byte {
	raw := r.required(name)
	if raw == "" {
		return nil
	}
	decoded, err := base64.StdEncoding.DecodeString(raw)
	if err != nil || len(decoded) != keyBytes {
		r.fail("%s must be 32 bytes encoded in base64", name)
		return nil
	}
	return decoded
}

func (r *reader) absoluteURL(name string) string {
	raw := strings.TrimRight(r.required(name), "/")
	if raw == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		r.fail("%s must be an absolute URL", name)
	}
	return raw
}

func (r *reader) prefixes(name string) []netip.Prefix {
	raw, ok := r.value(name)
	if !ok {
		return nil
	}
	var prefixes []netip.Prefix
	for item := range strings.SplitSeq(raw, ",") {
		prefix, err := netip.ParsePrefix(strings.TrimSpace(item))
		if err != nil {
			r.fail("%s must be a comma separated list of CIDR ranges", name)
			return nil
		}
		prefixes = append(prefixes, prefix)
	}
	return prefixes
}

func (r *reader) auth() Auth {
	return Auth{
		SigningSeed:      r.key("AUTH_SIGNING_KEY"),
		MFAKey:           r.key("MFA_ENCRYPTION_KEY"),
		CookieSecure:     r.boolean("AUTH_COOKIE_SECURE", true),
		TrustedProxies:   r.prefixes("TRUSTED_PROXIES"),
		RateLimitEnabled: r.boolean("RATE_LIMIT_ENABLED", true),
		Links: PublicLinks{
			StoreScheme: r.str("STORE_PUBLIC_SCHEME", "https"),
			StorePort:   r.str("STORE_PUBLIC_PORT", ""),
			AdminURL:    r.absoluteURL("ADMIN_PUBLIC_URL"),
			ConsoleURL:  r.absoluteURL("CONSOLE_PUBLIC_URL"),
		},
		SMTP: SMTP{
			Addr:       r.required("SMTP_ADDR"),
			From:       r.required("SMTP_FROM"),
			Username:   r.str("SMTP_USERNAME", ""),
			Password:   security.NewSecret(r.str("SMTP_PASSWORD", "")),
			RequireTLS: r.boolean("SMTP_REQUIRE_TLS", true),
		},
	}
}

func (r *reader) checkHardenedAuth(auth Auth) {
	r.check(auth.CookieSecure, "AUTH_COOKIE_SECURE must be true in staging and production")
	r.check(auth.RateLimitEnabled, "RATE_LIMIT_ENABLED must be true in staging and production")
	r.check(auth.SMTP.RequireTLS, "SMTP_REQUIRE_TLS must be true in staging and production")
	r.check(auth.Links.StoreScheme == "https", "STORE_PUBLIC_SCHEME must be https in staging and production")
	r.check(!bytes.HasPrefix(auth.SigningSeed, developmentKeyPrefix), "AUTH_SIGNING_KEY must not use the development sample")
	r.check(!bytes.HasPrefix(auth.MFAKey, developmentKeyPrefix), "MFA_ENCRYPTION_KEY must not use the development sample")
}
