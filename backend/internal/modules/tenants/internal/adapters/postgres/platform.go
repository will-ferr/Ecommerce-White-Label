package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/modules/tenants/internal/adapters/postgres/sqlcgen"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/modules/tenants/internal/app"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/modules/tenants/internal/domain"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/shared/apperr"
)

const uniqueViolation = "23505"

// PlatformStore persists operator changes using the platform database role.
type PlatformStore struct {
	pool *pgxpool.Pool
}

// NewPlatformStore creates a PlatformStore.
func NewPlatformStore(pool *pgxpool.Pool) *PlatformStore {
	return &PlatformStore{pool: pool}
}

func (s *PlatformStore) inTx(ctx context.Context, fn func(*sqlcgen.Queries) error) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error { return fn(sqlcgen.New(tx)) })
}

func tenantNotFound() error {
	return apperr.New(apperr.KindNotFound, "TENANT_NOT_FOUND", "The store does not exist.")
}

func domainNotFound() error {
	return apperr.New(apperr.KindNotFound, "DOMAIN_NOT_FOUND", "The domain does not exist.")
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == uniqueViolation
}

func lockTenant(ctx context.Context, q *sqlcgen.Queries, slug string) (sqlcgen.LockTenantBySlugRow, error) {
	tenant, err := q.LockTenantBySlug(ctx, slug)
	if errors.Is(err, pgx.ErrNoRows) {
		return sqlcgen.LockTenantBySlugRow{}, tenantNotFound()
	}
	if err != nil {
		return sqlcgen.LockTenantBySlugRow{}, fmt.Errorf("lock tenant: %w", err)
	}
	return tenant, nil
}

// CreateTenant implements app.AdminStore. Every part is created atomically.
func (s *PlatformStore) CreateTenant(ctx context.Context, tenant app.NewTenant) error {
	tenantID, err := uuid.NewV7()
	if err != nil {
		return fmt.Errorf("generate tenant id: %w", err)
	}
	themeID, err := uuid.NewV7()
	if err != nil {
		return fmt.Errorf("generate theme id: %w", err)
	}
	tokens, err := json.Marshal(tenant.Theme)
	if err != nil {
		return fmt.Errorf("encode theme: %w", err)
	}

	err = s.inTx(ctx, func(q *sqlcgen.Queries) error {
		planID, err := q.GetPlanIDByCode(ctx, tenant.PlanCode)
		if errors.Is(err, pgx.ErrNoRows) {
			return apperr.New(apperr.KindInvalid, "PLAN_NOT_FOUND", "The plan does not exist.")
		}
		if err != nil {
			return fmt.Errorf("get plan: %w", err)
		}
		if err := q.InsertTenant(ctx, sqlcgen.InsertTenantParams{ID: tenantID, Slug: tenant.Slug, PlanID: planID}); err != nil {
			return fmt.Errorf("insert tenant: %w", err)
		}
		now := time.Now()
		if err := q.InsertDomain(ctx, sqlcgen.InsertDomainParams{
			Host: tenant.Host, TenantID: tenantID, Kind: domain.DomainSubdomain, VerifiedAt: &now,
		}); err != nil {
			return fmt.Errorf("insert domain: %w", err)
		}
		if err := q.InsertSettings(ctx, sqlcgen.InsertSettingsParams{
			TenantID: tenantID, StoreName: tenant.Settings.StoreName, Currency: tenant.Settings.Currency,
			Locale: tenant.Settings.Locale, Timezone: tenant.Settings.Timezone, SeoTitle: tenant.Settings.SEOTitle,
			SeoDescription: tenant.Settings.SEODescription, SeoIndexable: tenant.Settings.SEOIndexable,
		}); err != nil {
			return fmt.Errorf("insert settings: %w", err)
		}
		if err := q.EnablePlanFeatures(ctx, sqlcgen.EnablePlanFeaturesParams{TenantID: tenantID, PlanID: planID}); err != nil {
			return fmt.Errorf("enable features: %w", err)
		}
		if err := q.InsertThemeVersion(ctx, sqlcgen.InsertThemeVersionParams{
			TenantID: tenantID, ID: themeID, Version: 1, Tokens: tokens,
		}); err != nil {
			return fmt.Errorf("insert theme: %w", err)
		}
		return nil
	})
	if isUniqueViolation(err) {
		return apperr.New(apperr.KindConflict, "TENANT_ALREADY_EXISTS", "A store with this name already exists.")
	}
	return err
}

// TenantExists implements app.AdminStore.
func (s *PlatformStore) TenantExists(ctx context.Context, slug string) (bool, error) {
	return sqlcgen.New(s.pool).TenantExists(ctx, slug)
}

// ListTenants implements app.AdminStore.
func (s *PlatformStore) ListTenants(ctx context.Context) ([]app.TenantSummary, error) {
	rows, err := sqlcgen.New(s.pool).ListTenants(ctx)
	if err != nil {
		return nil, fmt.Errorf("list tenants: %w", err)
	}
	tenants := make([]app.TenantSummary, 0, len(rows))
	for _, row := range rows {
		tenants = append(tenants, app.TenantSummary{
			Slug: row.Slug, Status: row.Status, PlanCode: row.PlanCode, StoreName: row.StoreName,
		})
	}
	return tenants, nil
}

// SetTenantStatus implements app.AdminStore.
func (s *PlatformStore) SetTenantStatus(ctx context.Context, slug, status string) error {
	affected, err := sqlcgen.New(s.pool).SetTenantStatus(ctx, sqlcgen.SetTenantStatusParams{Slug: slug, Status: status})
	if err != nil {
		return fmt.Errorf("set tenant status: %w", err)
	}
	if affected == 0 {
		return tenantNotFound()
	}
	return nil
}

// AddCustomDomain implements app.AdminStore.
func (s *PlatformStore) AddCustomDomain(ctx context.Context, slug, host, token string) error {
	err := s.inTx(ctx, func(q *sqlcgen.Queries) error {
		tenant, err := lockTenant(ctx, q, slug)
		if err != nil {
			return err
		}
		return q.InsertDomain(ctx, sqlcgen.InsertDomainParams{
			Host: host, TenantID: tenant.ID, Kind: domain.DomainCustom, VerificationToken: &token,
		})
	})
	if isUniqueViolation(err) {
		return apperr.New(apperr.KindConflict, "DOMAIN_ALREADY_EXISTS", "This domain is already registered.")
	}
	return err
}

// Domain implements app.AdminStore.
func (s *PlatformStore) Domain(ctx context.Context, host string) (app.DomainRecord, error) {
	row, err := sqlcgen.New(s.pool).GetDomain(ctx, host)
	if errors.Is(err, pgx.ErrNoRows) {
		return app.DomainRecord{}, domainNotFound()
	}
	if err != nil {
		return app.DomainRecord{}, fmt.Errorf("get domain: %w", err)
	}
	record := app.DomainRecord{Host: row.Host, Kind: row.Kind, Verified: row.VerifiedAt != nil}
	if row.VerificationToken != nil {
		record.VerificationToken = *row.VerificationToken
	}
	return record, nil
}

// MarkDomainVerified implements app.AdminStore.
func (s *PlatformStore) MarkDomainVerified(ctx context.Context, host string) error {
	return sqlcgen.New(s.pool).MarkDomainVerified(ctx, host)
}

// RemoveCustomDomain implements app.AdminStore.
func (s *PlatformStore) RemoveCustomDomain(ctx context.Context, host string) error {
	affected, err := sqlcgen.New(s.pool).DeleteCustomDomain(ctx, host)
	if err != nil {
		return fmt.Errorf("delete domain: %w", err)
	}
	if affected == 0 {
		return domainNotFound()
	}
	return nil
}

// UpdateSettings implements app.AdminStore.
func (s *PlatformStore) UpdateSettings(ctx context.Context, slug string, change func(domain.Settings) (domain.Settings, error)) error {
	return s.inTx(ctx, func(q *sqlcgen.Queries) error {
		tenant, err := lockTenant(ctx, q, slug)
		if err != nil {
			return err
		}
		row, err := q.GetSettingsForUpdate(ctx, tenant.ID)
		if err != nil {
			return fmt.Errorf("get settings: %w", err)
		}
		updated, err := change(domain.Settings{
			StoreName: row.StoreName, Currency: row.Currency, Locale: row.Locale, Timezone: row.Timezone,
			SEOTitle: row.SeoTitle, SEODescription: row.SeoDescription, SEOIndexable: row.SeoIndexable,
		})
		if err != nil {
			return err
		}
		return q.UpdateSettings(ctx, sqlcgen.UpdateSettingsParams{
			TenantID: tenant.ID, StoreName: updated.StoreName, Currency: updated.Currency, Locale: updated.Locale,
			Timezone: updated.Timezone, SeoTitle: updated.SEOTitle, SeoDescription: updated.SEODescription,
			SeoIndexable: updated.SEOIndexable,
		})
	})
}

// SetFeature implements app.AdminStore.
func (s *PlatformStore) SetFeature(ctx context.Context, slug, key string, enabled bool) error {
	return s.inTx(ctx, func(q *sqlcgen.Queries) error {
		tenant, err := lockTenant(ctx, q, slug)
		if err != nil {
			return err
		}
		included, err := q.PlanIncludesFeature(ctx, sqlcgen.PlanIncludesFeatureParams{PlanID: tenant.PlanID, FeatureKey: key})
		if err != nil {
			return fmt.Errorf("check plan: %w", err)
		}
		if !included {
			return apperr.New(apperr.KindConflict, "FEATURE_NOT_IN_PLAN", "The plan of this store does not include this feature.")
		}
		return q.UpsertTenantFeature(ctx, sqlcgen.UpsertTenantFeatureParams{TenantID: tenant.ID, FeatureKey: key, Enabled: enabled})
	})
}

// PublishTheme implements app.AdminStore.
func (s *PlatformStore) PublishTheme(ctx context.Context, slug string, theme domain.Theme) (int32, error) {
	tokens, err := json.Marshal(theme)
	if err != nil {
		return 0, fmt.Errorf("encode theme: %w", err)
	}
	themeID, err := uuid.NewV7()
	if err != nil {
		return 0, fmt.Errorf("generate theme id: %w", err)
	}

	var version int32
	err = s.inTx(ctx, func(q *sqlcgen.Queries) error {
		tenant, err := lockTenant(ctx, q, slug)
		if err != nil {
			return err
		}
		version, err = q.NextThemeVersion(ctx, tenant.ID)
		if err != nil {
			return fmt.Errorf("next theme version: %w", err)
		}
		if err := q.ArchivePublishedTheme(ctx, tenant.ID); err != nil {
			return fmt.Errorf("archive theme: %w", err)
		}
		return q.InsertThemeVersion(ctx, sqlcgen.InsertThemeVersionParams{
			TenantID: tenant.ID, ID: themeID, Version: version, Tokens: tokens,
		})
	})
	if err != nil {
		return 0, err
	}
	return version, nil
}

// RollbackTheme implements app.AdminStore.
func (s *PlatformStore) RollbackTheme(ctx context.Context, slug string, version int32) error {
	return s.inTx(ctx, func(q *sqlcgen.Queries) error {
		tenant, err := lockTenant(ctx, q, slug)
		if err != nil {
			return err
		}
		if err := q.ArchivePublishedTheme(ctx, tenant.ID); err != nil {
			return fmt.Errorf("archive theme: %w", err)
		}
		affected, err := q.RepublishThemeVersion(ctx, sqlcgen.RepublishThemeVersionParams{TenantID: tenant.ID, Version: version})
		if err != nil {
			return fmt.Errorf("republish theme: %w", err)
		}
		if affected == 0 {
			return apperr.New(apperr.KindNotFound, "THEME_VERSION_NOT_FOUND", "This theme version does not exist.")
		}
		return nil
	})
}
