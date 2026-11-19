package app

import (
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/modules/identity"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/modules/tenants"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/config"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/ratelimit"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/tenancy"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/shared/reqctx"
)

// General limits that protect the API from abuse.
var (
	requestsByIP     = ratelimit.Rule{Max: 600, Window: time.Minute}
	requestsByTenant = ratelimit.Rule{Max: 6000, Window: time.Minute}
)

// ModuleDeps lists what the business modules need. A nil Limiter disables rate limiting.
type ModuleDeps struct {
	Pool    *pgxpool.Pool
	Logger  *slog.Logger
	Limiter *ratelimit.Limiter
	Mailer  identity.Mailer
	Auth    config.Auth
}

// RegisterModules connects the business modules to the API router.
func RegisterModules(deps ModuleDeps) (func(chi.Router), error) {
	tenantsModule := tenants.New(deps.Pool, deps.Logger)
	identityModule, err := identity.New(identity.Deps{
		Pool: deps.Pool, Logger: deps.Logger, Limiter: deps.Limiter, Mailer: deps.Mailer, Auth: deps.Auth,
	})
	if err != nil {
		return nil, fmt.Errorf("create identity module: %w", err)
	}

	return func(api chi.Router) {
		api.Group(func(limited chi.Router) {
			if deps.Limiter != nil {
				limited.Use(deps.Limiter.Middleware("api:ip", requestsByIP, func(r *http.Request) string {
					return reqctx.ClientIP(r.Context())
				}))
			}

			limited.Group(func(store chi.Router) {
				store.Use(tenantsModule.RequireTenant())
				if deps.Limiter != nil {
					store.Use(deps.Limiter.Middleware("api:tenant", requestsByTenant, func(r *http.Request) string {
						tenantID, _ := tenancy.FromContext(r.Context())
						return tenantID.String()
					}))
				}
				tenantsModule.RegisterStorefrontRoutes(store)
				store.Route("/auth", identityModule.RegisterCustomerRoutes)
			})

			limited.Route("/staff", identityModule.RegisterStaffRoutes)
			limited.Route("/platform", identityModule.RegisterPlatformRoutes)
		})
	}, nil
}
