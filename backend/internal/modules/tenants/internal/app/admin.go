package app

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/modules/tenants/internal/domain"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/shared/apperr"
)

// NewTenant holds what is needed to provision a store.
type NewTenant struct {
	Slug     string
	PlanCode string
	Host     string
	Settings domain.Settings
	Theme    domain.Theme
}

// TenantSummary is one line of the tenant list.
type TenantSummary struct {
	Slug      string
	Status    string
	PlanCode  string
	StoreName string
	CreatedAt time.Time
}

// DomainRecord is a stored domain.
type DomainRecord struct {
	Host              string
	Kind              string
	VerificationToken string
	Verified          bool
}

// SettingsChange applies a partial update to the current settings.
type SettingsChange func(*domain.Settings)

// AdminStore persists platform-level changes.
type AdminStore interface {
	CreateTenant(ctx context.Context, tenant NewTenant) error
	TenantExists(ctx context.Context, slug string) (bool, error)
	ListTenants(ctx context.Context) ([]TenantSummary, error)
	SetTenantStatus(ctx context.Context, slug, status string) error
	AddCustomDomain(ctx context.Context, slug, host, token string) error
	Domain(ctx context.Context, host string) (DomainRecord, error)
	MarkDomainVerified(ctx context.Context, host string) error
	RemoveCustomDomain(ctx context.Context, host string) error
	UpdateSettings(ctx context.Context, slug string, change func(domain.Settings) (domain.Settings, error)) error
	SetFeature(ctx context.Context, slug, key string, enabled bool) error
	PublishTheme(ctx context.Context, slug string, theme domain.Theme) (int32, error)
	RollbackTheme(ctx context.Context, slug string, version int32) error
}

// TXTResolver reads DNS TXT records. net.Resolver implements it.
type TXTResolver interface {
	LookupTXT(ctx context.Context, name string) ([]string, error)
}

// Admin implements the operations reserved to platform operators.
type Admin struct {
	store      AdminStore
	dns        TXTResolver
	baseDomain string
}

// NewAdmin creates an Admin.
func NewAdmin(store AdminStore, dns TXTResolver, baseDomain string) *Admin {
	return &Admin{store: store, dns: dns, baseDomain: baseDomain}
}

// CreateTenant provisions a store with its subdomain, settings and theme.
func (a *Admin) CreateTenant(ctx context.Context, slug, storeName, planCode string) (string, error) {
	if err := domain.ValidateSlug(slug); err != nil {
		return "", err
	}
	settings := domain.DefaultSettings(storeName)
	if err := settings.Validate(); err != nil {
		return "", err
	}
	host := domain.SubdomainHost(slug, a.baseDomain)
	if _, ok := domain.NormalizeHost(host); !ok {
		return "", apperr.New(apperr.KindInvalid, "INVALID_BASE_DOMAIN", "The platform base domain is not valid.")
	}
	err := a.store.CreateTenant(ctx, NewTenant{
		Slug: slug, PlanCode: planCode, Host: host, Settings: settings, Theme: domain.DefaultTheme(),
	})
	if err != nil {
		return "", fmt.Errorf("create tenant: %w", err)
	}
	return host, nil
}

// TenantExists reports whether a slug is in use.
func (a *Admin) TenantExists(ctx context.Context, slug string) (bool, error) {
	return a.store.TenantExists(ctx, slug)
}

// ListTenants returns every store.
func (a *Admin) ListTenants(ctx context.Context) ([]TenantSummary, error) {
	return a.store.ListTenants(ctx)
}

// SetStatus suspends or reactivates a store.
func (a *Admin) SetStatus(ctx context.Context, slug, status string) error {
	if !slices.Contains([]string{domain.StatusActive, domain.StatusSuspended}, status) {
		return apperr.New(apperr.KindInvalid, "INVALID_STATUS", "The status is not supported.")
	}
	return a.store.SetTenantStatus(ctx, slug, status)
}

// AddDomain registers a custom domain and returns the DNS record that proves
// its ownership. The domain serves no traffic until it is verified.
func (a *Admin) AddDomain(ctx context.Context, slug, rawHost string) (recordName, token string, err error) {
	host, err := domain.ValidateCustomHost(rawHost, a.baseDomain)
	if err != nil {
		return "", "", err
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return "", "", fmt.Errorf("generate verification token: %w", err)
	}
	token = hex.EncodeToString(secret)
	if err := a.store.AddCustomDomain(ctx, slug, host, token); err != nil {
		return "", "", fmt.Errorf("add domain: %w", err)
	}
	return domain.VerificationPrefix + host, token, nil
}

// VerifyDomain activates a domain whose DNS record contains the expected token.
func (a *Admin) VerifyDomain(ctx context.Context, rawHost string) error {
	host, ok := domain.NormalizeHost(rawHost)
	if !ok {
		return apperr.New(apperr.KindInvalid, "INVALID_HOST", "Enter a valid domain name.")
	}
	record, err := a.store.Domain(ctx, host)
	if err != nil {
		return err
	}
	if record.Verified {
		return nil
	}

	values, err := a.dns.LookupTXT(ctx, domain.VerificationPrefix+host)
	if err != nil {
		return apperr.Wrap(apperr.KindConflict, "DOMAIN_NOT_VERIFIED", "The verification record was not found.", err)
	}
	for _, value := range values {
		if subtle.ConstantTimeCompare([]byte(strings.TrimSpace(value)), []byte(record.VerificationToken)) == 1 {
			return a.store.MarkDomainVerified(ctx, host)
		}
	}
	return apperr.New(apperr.KindConflict, "DOMAIN_NOT_VERIFIED", "The verification record does not match.")
}

// RemoveDomain detaches a custom domain immediately.
func (a *Admin) RemoveDomain(ctx context.Context, rawHost string) error {
	host, ok := domain.NormalizeHost(rawHost)
	if !ok {
		return apperr.New(apperr.KindInvalid, "INVALID_HOST", "Enter a valid domain name.")
	}
	return a.store.RemoveCustomDomain(ctx, host)
}

// UpdateSettings applies the changes and validates the result.
func (a *Admin) UpdateSettings(ctx context.Context, slug string, changes ...SettingsChange) error {
	return a.store.UpdateSettings(ctx, slug, func(current domain.Settings) (domain.Settings, error) {
		for _, change := range changes {
			change(&current)
		}
		if err := current.Validate(); err != nil {
			return domain.Settings{}, err
		}
		return current, nil
	})
}

// SetFeature turns a feature on or off for a store.
func (a *Admin) SetFeature(ctx context.Context, slug, key string, enabled bool) error {
	return a.store.SetFeature(ctx, slug, key, enabled)
}

// PublishTheme validates a theme document and makes it the live version.
func (a *Admin) PublishTheme(ctx context.Context, slug string, document []byte) (int32, error) {
	theme, err := domain.ParseTheme(document)
	if err != nil {
		return 0, err
	}
	return a.store.PublishTheme(ctx, slug, theme)
}

// RollbackTheme makes a previous version live again.
func (a *Admin) RollbackTheme(ctx context.Context, slug string, version int32) error {
	if version <= 0 {
		return apperr.New(apperr.KindInvalid, "INVALID_VERSION", "The version must be positive.")
	}
	return a.store.RollbackTheme(ctx, slug, version)
}
