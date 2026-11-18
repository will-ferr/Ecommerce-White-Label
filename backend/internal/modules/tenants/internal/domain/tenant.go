// Package domain holds the tenant rules, free of infrastructure concerns.
package domain

import (
	"regexp"

	"github.com/google/uuid"

	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/shared/apperr"
)

// Tenant statuses.
const (
	StatusActive    = "active"
	StatusSuspended = "suspended"
)

// ResolvedTenant is what routing needs to know about the tenant of a host.
type ResolvedTenant struct {
	ID       uuid.UUID
	Status   string
	PlanCode string
}

// IsActive reports whether the store may serve requests.
func (t ResolvedTenant) IsActive() bool { return t.Status == StatusActive }

var slugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,61}[a-z0-9]$`)

// reservedSlugs protects subdomains used by the platform itself.
var reservedSlugs = map[string]struct{}{
	"admin": {}, "api": {}, "app": {}, "assets": {}, "auth": {}, "billing": {}, "blog": {},
	"cdn": {}, "console": {}, "dashboard": {}, "docs": {}, "ftp": {}, "grafana": {}, "help": {},
	"internal": {}, "login": {}, "mail": {}, "media": {}, "ns1": {}, "ns2": {}, "platform": {},
	"root": {}, "smtp": {}, "static": {}, "status": {}, "support": {}, "system": {}, "www": {},
}

// ValidateSlug checks that a slug can become a subdomain of the platform.
func ValidateSlug(slug string) error {
	if !slugPattern.MatchString(slug) {
		return fieldError("slug", "invalid", "Use 3 to 63 lowercase letters, numbers or hyphens.")
	}
	if _, reserved := reservedSlugs[slug]; reserved {
		return fieldError("slug", "reserved", "This name is reserved by the platform.")
	}
	return nil
}

func fieldError(field, code, message string) error {
	return apperr.Validation(apperr.FieldError{Field: field, Code: code, Message: message})
}
