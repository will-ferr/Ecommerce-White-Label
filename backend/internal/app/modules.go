package app

import (
	"log/slog"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/modules/tenants"
)

// RegisterModules connects the business modules to the API router.
func RegisterModules(pool *pgxpool.Pool, logger *slog.Logger) func(chi.Router) {
	tenantsModule := tenants.New(pool, logger)

	return func(api chi.Router) {
		api.Group(func(store chi.Router) {
			store.Use(tenantsModule.RequireTenant())
			tenantsModule.RegisterStorefrontRoutes(store)
		})
	}
}
