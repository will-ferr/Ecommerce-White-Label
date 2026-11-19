package app

import (
	"crypto/hkdf"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/modules/catalog"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/modules/identity"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/modules/sales"
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
	Pool     *pgxpool.Pool
	Logger   *slog.Logger
	Limiter  *ratelimit.Limiter
	Mailer   identity.Mailer
	Auth     config.Auth
	Commerce config.Commerce
}

// Modules holds the wired business modules.
type Modules struct {
	Sales    *sales.Module
	Register func(chi.Router)
}

// BuildModules wires the business modules and their routes.
func BuildModules(deps ModuleDeps) (*Modules, error) {
	tenantsModule := tenants.New(deps.Pool, deps.Logger)
	identityModule, err := identity.New(identity.Deps{
		Pool: deps.Pool, Logger: deps.Logger, Limiter: deps.Limiter, Mailer: deps.Mailer, Auth: deps.Auth,
	})
	if err != nil {
		return nil, fmt.Errorf("create identity module: %w", err)
	}
	accessKey, err := hkdf.Key(sha256.New, deps.Auth.SigningSeed, nil, "order-access-v1", 32)
	if err != nil {
		return nil, fmt.Errorf("derive order access key: %w", err)
	}
	catalogModule := catalog.New(deps.Pool, deps.Logger)
	salesModule := sales.New(sales.Deps{
		Pool: deps.Pool, Logger: deps.Logger, AccessKey: accessKey, PaymentTTL: deps.Commerce.PaymentTTL,
		TestPayments: deps.Commerce.TestPayments, CookieSecure: deps.Auth.CookieSecure,
	}, deps.Limiter)

	register := func(api chi.Router) {
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
				store.Use(identityModule.CustomerAuthentication())
				tenantsModule.RegisterStorefrontRoutes(store)
				store.Route("/auth", identityModule.RegisterCustomerRoutes)
				catalogModule.RegisterStoreRoutes(store)
				salesModule.RegisterStoreRoutes(store)
			})

			limited.Route("/staff", func(staff chi.Router) {
				staff.Use(identityModule.StaffAuthentication())
				identityModule.RegisterStaffRoutes(staff)
				catalogModule.RegisterAdminRoutes(staff)
				salesModule.RegisterAdminRoutes(staff)
			})
			limited.Route("/platform", func(platform chi.Router) {
				platform.Use(identityModule.PlatformAuthentication())
				identityModule.RegisterPlatformRoutes(platform)
			})
		})
	}
	return &Modules{Sales: salesModule, Register: register}, nil
}
