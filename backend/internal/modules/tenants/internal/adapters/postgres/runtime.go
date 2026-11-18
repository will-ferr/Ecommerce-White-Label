// Package postgres implements the persistence of the tenants module.
package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/modules/tenants/internal/adapters/postgres/sqlcgen"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/modules/tenants/internal/app"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/modules/tenants/internal/domain"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/database"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/tenancy"
)

// HostLookup resolves hosts through the restricted database function.
type HostLookup struct {
	pool *pgxpool.Pool
}

// NewHostLookup creates a HostLookup.
func NewHostLookup(pool *pgxpool.Pool) *HostLookup {
	return &HostLookup{pool: pool}
}

// LookupHost implements app.HostLookup.
func (l *HostLookup) LookupHost(ctx context.Context, host string) (domain.ResolvedTenant, error) {
	var tenant domain.ResolvedTenant
	err := l.pool.QueryRow(ctx,
		`SELECT tenant_id, status, plan_code FROM core.resolve_tenant_by_host($1)`, host,
	).Scan(&tenant.ID, &tenant.Status, &tenant.PlanCode)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ResolvedTenant{}, app.ErrHostNotFound
	}
	if err != nil {
		return domain.ResolvedTenant{}, fmt.Errorf("resolve host: %w", err)
	}
	return tenant, nil
}

// StorefrontReader reads store configuration under Row Level Security.
type StorefrontReader struct {
	db *database.TenantDB
}

// NewStorefrontReader creates a StorefrontReader.
func NewStorefrontReader(db *database.TenantDB) *StorefrontReader {
	return &StorefrontReader{db: db}
}

// LoadStorefront implements app.StorefrontReader.
func (r *StorefrontReader) LoadStorefront(ctx context.Context) (app.StoredStorefront, error) {
	tenantID, ok := tenancy.FromContext(ctx)
	if !ok {
		return app.StoredStorefront{}, tenancy.ErrMissingTenant
	}

	var stored app.StoredStorefront
	err := r.db.Run(ctx, func(tx pgx.Tx) error {
		queries := sqlcgen.New(tx)

		slug, err := queries.GetTenantSlug(ctx, tenantID)
		if err != nil {
			return fmt.Errorf("get tenant: %w", err)
		}
		settings, err := queries.GetSettings(ctx, tenantID)
		if err != nil {
			return fmt.Errorf("get settings: %w", err)
		}
		theme, err := queries.GetPublishedTheme(ctx, tenantID)
		if err != nil {
			return fmt.Errorf("get theme: %w", err)
		}
		features, err := queries.ListFeatureStates(ctx, tenantID)
		if err != nil {
			return fmt.Errorf("list features: %w", err)
		}

		stored = app.StoredStorefront{
			Slug: slug,
			Settings: domain.Settings{
				StoreName: settings.StoreName, Currency: settings.Currency, Locale: settings.Locale,
				Timezone: settings.Timezone, SEOTitle: settings.SeoTitle,
				SEODescription: settings.SeoDescription, SEOIndexable: settings.SeoIndexable,
			},
			ThemeVersion: theme.Version,
			ThemeTokens:  theme.Tokens,
			Features:     make([]domain.FeatureState, 0, len(features)),
		}
		for _, feature := range features {
			stored.Features = append(stored.Features, domain.FeatureState{
				Key: feature.Key, GloballyEnabled: feature.GloballyEnabled,
				InPlan: feature.InPlan, TenantEnabled: feature.TenantEnabled,
			})
		}
		return nil
	})
	if err != nil {
		return app.StoredStorefront{}, err
	}
	return stored, nil
}
