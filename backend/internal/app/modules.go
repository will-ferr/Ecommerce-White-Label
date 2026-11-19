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
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/mail"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/messaging"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/ratelimit"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/tenancy"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/shared/reqctx"
)

// General limits that protect the API from abuse.
var (
	requestsByIP     = ratelimit.Rule{Max: 600, Window: time.Minute}
	requestsByTenant = ratelimit.Rule{Max: 6000, Window: time.Minute}
)

// ModuleDeps lists what the business modules need. A nil Limiter disables
// rate limiting. A nil Mailer stores account e-mails in the durable outbox.
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
	// Subscriptions are the event consumers run by the worker.
	Subscriptions []messaging.Subscription
}

func deriveKey(seed []byte, label string) ([]byte, error) {
	key, err := hkdf.Key(sha256.New, seed, nil, label, 32)
	if err != nil {
		return nil, fmt.Errorf("derive %s key: %w", label, err)
	}
	return key, nil
}

// BuildModules wires the business modules and their routes.
func BuildModules(deps ModuleDeps) (*Modules, error) {
	sealer, err := mail.NewSealer(deps.Auth.MFAKey)
	if err != nil {
		return nil, err
	}
	outbox := mail.NewOutbox(deps.Pool, sealer, deps.Logger)
	mailer := deps.Mailer
	if mailer == nil {
		mailer = outbox
	}

	tenantsModule := tenants.New(deps.Pool, deps.Logger)
	identityModule, err := identity.New(identity.Deps{
		Pool: deps.Pool, Logger: deps.Logger, Limiter: deps.Limiter, Mailer: mailer, Auth: deps.Auth,
	})
	if err != nil {
		return nil, fmt.Errorf("create identity module: %w", err)
	}
	accessKey, err := deriveKey(deps.Auth.SigningSeed, "order-access-v1")
	if err != nil {
		return nil, err
	}
	var testPaymentKey []byte
	if deps.Commerce.TestPayments {
		if testPaymentKey, err = deriveKey(deps.Auth.SigningSeed, "test-payments-v1"); err != nil {
			return nil, err
		}
	}
	catalogModule := catalog.New(deps.Pool, deps.Logger)
	salesModule := sales.New(sales.Deps{
		Pool: deps.Pool, Logger: deps.Logger, AccessKey: accessKey, TestPaymentKey: testPaymentKey,
		PaymentTTL: deps.Commerce.PaymentTTL, CookieSecure: deps.Auth.CookieSecure, Mail: outbox, Links: deps.Auth.Links,
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
	return &Modules{
		Sales: salesModule, Register: register,
		Subscriptions: []messaging.Subscription{
			salesModule.Service().NotificationSubscription(),
			salesModule.Service().RefundSubscription(),
		},
	}, nil
}
