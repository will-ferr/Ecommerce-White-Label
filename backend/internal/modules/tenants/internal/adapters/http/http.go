// Package http exposes the tenants module over HTTP.
package http

import (
	"context"
	"log/slog"
	"net/http"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/modules/tenants/internal/app"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/modules/tenants/internal/domain"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/httpx"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/tenancy"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/shared/reqctx"
)

// HostResolver finds the tenant of a Host header.
type HostResolver interface {
	Resolve(ctx context.Context, rawHost string) (domain.ResolvedTenant, error)
}

// RequireTenant binds the request to the store of its Host header. The tenant
// never comes from a value the client can choose per request, such as a query
// parameter, a body field or an X-Tenant style header.
func RequireTenant(resolver HostResolver, logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tenant, err := resolver.Resolve(r.Context(), r.Host)
			if err != nil {
				httpx.WriteError(w, r, logger, err)
				return
			}

			ctx := tenancy.WithTenant(r.Context(), tenant.ID)
			reqctx.SetTenantID(ctx, tenant.ID.String())
			trace.SpanFromContext(ctx).SetAttributes(
				attribute.String("tenant.id", tenant.ID.String()),
				attribute.String("tenant.tier", tenant.PlanCode),
			)
			// The plan has few values, unlike the tenant ID, so it is safe as a metric label.
			if labeler, ok := otelhttp.LabelerFromContext(ctx); ok {
				labeler.Add(attribute.String("tenant_tier", tenant.PlanCode))
			}
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// StorefrontHandler serves the public configuration of a store.
type StorefrontHandler struct {
	storefront *app.Storefront
}

// NewStorefrontHandler creates a StorefrontHandler.
func NewStorefrontHandler(storefront *app.Storefront) *StorefrontHandler {
	return &StorefrontHandler{storefront: storefront}
}

type storefrontResponse struct {
	Slug     string          `json:"slug"`
	Store    storeResponse   `json:"store"`
	SEO      seoResponse     `json:"seo"`
	Theme    themeResponse   `json:"theme"`
	Features map[string]bool `json:"features"`
}

type storeResponse struct {
	Name     string `json:"name"`
	Currency string `json:"currency"`
	Locale   string `json:"locale"`
	Timezone string `json:"timezone"`
}

type seoResponse struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Indexable   bool   `json:"indexable"`
}

type themeResponse struct {
	Version int32        `json:"version"`
	Tokens  domain.Theme `json:"tokens"`
}

// Config handles GET /api/v1/storefront/config.
func (h *StorefrontHandler) Config(w http.ResponseWriter, r *http.Request) error {
	config, err := h.storefront.Config(r.Context())
	if err != nil {
		return err
	}
	return httpx.WriteJSON(w, http.StatusOK, storefrontResponse{
		Slug: config.Slug,
		Store: storeResponse{
			Name: config.Settings.StoreName, Currency: config.Settings.Currency,
			Locale: config.Settings.Locale, Timezone: config.Settings.Timezone,
		},
		SEO: seoResponse{
			Title: config.Settings.SEOTitle, Description: config.Settings.SEODescription,
			Indexable: config.Settings.SEOIndexable,
		},
		Theme:    themeResponse{Version: config.ThemeVersion, Tokens: config.Theme},
		Features: config.Features,
	})
}
