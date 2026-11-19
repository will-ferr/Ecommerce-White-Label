// Package identity manages accounts, sessions, two-step verification and roles.
package identity

import (
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	identityhttp "github.com/will-ferr/Ecommerce-White-Label/backend/internal/modules/identity/internal/adapters/http"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/modules/identity/internal/app"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/config"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/mail"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/ratelimit"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/security"
)

// Mailer schedules outgoing e-mail.
type Mailer interface {
	Enqueue(message mail.Message)
}

// Deps lists what the module needs. A nil Limiter disables rate limiting.
type Deps struct {
	Pool    *pgxpool.Pool
	Logger  *slog.Logger
	Limiter *ratelimit.Limiter
	Mailer  Mailer
	Auth    config.Auth
}

// Module is the public surface of the identity module.
type Module struct {
	customer *identityhttp.Handler
	staff    *identityhttp.Handler
	platform *identityhttp.Handler
}

// New wires the module.
func New(deps Deps) (*Module, error) {
	hasher, err := security.NewPasswordHasher()
	if err != nil {
		return nil, fmt.Errorf("create password hasher: %w", err)
	}
	sealer, err := security.NewSealer(deps.Auth.MFAKey)
	if err != nil {
		return nil, fmt.Errorf("create mfa sealer: %w", err)
	}
	issuer, err := app.NewTokenIssuer(deps.Auth.SigningSeed, time.Now)
	if err != nil {
		return nil, fmt.Errorf("create token issuer: %w", err)
	}
	service := app.NewService(app.Deps{
		Pool: deps.Pool, Hasher: hasher, Sealer: sealer, Issuer: issuer, Mailer: deps.Mailer,
		Links: deps.Auth.Links, Logger: deps.Logger,
	})
	handler := func(portal app.Portal) *identityhttp.Handler {
		return identityhttp.NewHandler(service, portal, deps.Limiter, deps.Logger, deps.Auth.CookieSecure)
	}
	return &Module{customer: handler(app.Customer), staff: handler(app.Staff), platform: handler(app.Platform)}, nil
}

// CustomerAuthentication reads the bearer token of customers on every store route.
func (m *Module) CustomerAuthentication() func(http.Handler) http.Handler {
	return m.customer.Authenticate
}

// StaffAuthentication reads the bearer token of store teams.
func (m *Module) StaffAuthentication() func(http.Handler) http.Handler { return m.staff.Authenticate }

// PlatformAuthentication reads the bearer token of platform operators.
func (m *Module) PlatformAuthentication() func(http.Handler) http.Handler {
	return m.platform.Authenticate
}

// RegisterCustomerRoutes adds the customer portal. The store must already be
// resolved from the domain of the request.
func (m *Module) RegisterCustomerRoutes(router chi.Router) { m.customer.Register(router) }

// RegisterStaffRoutes adds the portal of store teams.
func (m *Module) RegisterStaffRoutes(router chi.Router) { m.staff.Register(router) }

// RegisterPlatformRoutes adds the portal of platform operators.
func (m *Module) RegisterPlatformRoutes(router chi.Router) { m.platform.Register(router) }
