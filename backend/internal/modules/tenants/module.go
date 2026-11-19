// Package tenants manages stores, domains, themes and feature flags.
package tenants

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	tenanthttp "github.com/will-ferr/Ecommerce-White-Label/backend/internal/modules/tenants/internal/adapters/http"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/modules/tenants/internal/adapters/postgres"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/modules/tenants/internal/app"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/authz"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/database"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/httpx"
)

// Module is the public surface of the tenants module for the API process.
type Module struct {
	db         *database.TenantDB
	logger     *slog.Logger
	resolver   *app.Resolver
	storefront *tenanthttp.StorefrontHandler
}

// New wires the module. The pool must use the unprivileged runtime role.
func New(pool *pgxpool.Pool, logger *slog.Logger) *Module {
	db := database.NewTenantDB(pool)
	reader := postgres.NewStorefrontReader(db)
	return &Module{
		db:         db,
		logger:     logger,
		resolver:   app.NewResolver(postgres.NewHostLookup(pool), time.Now),
		storefront: tenanthttp.NewStorefrontHandler(app.NewStorefront(reader, logger)),
	}
}

// RequireTenant returns the middleware that binds requests to their store.
func (m *Module) RequireTenant() func(http.Handler) http.Handler {
	return tenanthttp.RequireTenant(m.resolver, m.logger)
}

// RegisterStorefrontRoutes adds the routes that run inside a store context.
func (m *Module) RegisterStorefrontRoutes(router chi.Router) {
	router.Method(http.MethodGet, "/storefront/config",
		authz.Protect(m.logger, authz.Public, httpx.Handle(m.logger, m.storefront.Config)))
}
