// Package sales covers cart, checkout, orders, payments, shipping methods and
// customer addresses. They share one Go module while their tables stay in
// separate schemas, so each part can still be extracted later (ADR-017).
package sales

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/modules/audit"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/modules/sales/internal/payments"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/modules/sales/internal/sqlcgen"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/authz"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/config"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/database"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/mail"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/tenancy"
)

// Deps lists what the module needs. TestPaymentKey enables the test payment
// provider; it must be empty in production.
type Deps struct {
	Pool           *pgxpool.Pool
	Logger         *slog.Logger
	AccessKey      []byte
	TestPaymentKey []byte
	PaymentTTL     time.Duration
	CookieSecure   bool
	Mail           *mail.Outbox
	Links          config.PublicLinks
}

// Service implements the sales use cases.
type Service struct {
	db         *database.TenantDB
	pool       *pgxpool.Pool
	logger     *slog.Logger
	accessKey  []byte
	paymentTTL time.Duration
	mail       *mail.Outbox
	links      config.PublicLinks
	now        func() time.Time

	// providers holds every payment provider the platform can talk to, by
	// name. checkoutProvider charges new orders; nil means none is configured.
	providers        map[string]payments.Provider
	checkoutProvider payments.Provider
	testProvider     *payments.TestProvider
}

// NewService creates a Service.
func NewService(deps Deps) *Service {
	s := &Service{
		db: database.NewTenantDB(deps.Pool), pool: deps.Pool, logger: deps.Logger, accessKey: deps.AccessKey,
		paymentTTL: deps.PaymentTTL, mail: deps.Mail, links: deps.Links, now: time.Now,
		providers: map[string]payments.Provider{},
	}
	if len(deps.TestPaymentKey) > 0 {
		s.testProvider = payments.NewTestProvider(deps.TestPaymentKey)
		s.providers[s.testProvider.Name()] = s.testProvider
		s.checkoutProvider = s.testProvider
	}
	return s
}

// providerLabel names a provider for buyers and the store team.
func (s *Service) providerLabel(name string) string {
	if provider, ok := s.providers[name]; ok {
		return provider.Label()
	}
	return name
}

// storeLink returns the address of a page of the store where an order was placed.
func (s *Service) storeLink(host, path string) string {
	base := s.links.StoreScheme + "://" + host
	if s.links.StorePort != "" {
		base += ":" + s.links.StorePort
	}
	return base + path
}

func (s *Service) in(ctx context.Context, fn func(pgx.Tx, *sqlcgen.Queries, uuid.UUID) error) error {
	tenantID, ok := tenancy.FromContext(ctx)
	if !ok {
		return tenancy.ErrMissingTenant
	}
	return s.db.Run(ctx, func(tx pgx.Tx) error { return fn(tx, sqlcgen.New(tx), tenantID) })
}

func newID() uuid.UUID {
	id, err := uuid.NewV7()
	if err != nil {
		return uuid.New()
	}
	return id
}

// accessToken lets a guest open their own order. It is derived from the order,
// so it never needs to be stored.
func (s *Service) accessToken(tenantID, orderID uuid.UUID) string {
	mac := hmac.New(sha256.New, s.accessKey)
	mac.Write([]byte(tenantID.String() + "|" + orderID.String()))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (s *Service) validAccessToken(tenantID, orderID uuid.UUID, token string) bool {
	return token != "" && hmac.Equal([]byte(token), []byte(s.accessToken(tenantID, orderID)))
}

func userAudit(ctx context.Context, tx pgx.Tx, principal authz.Principal, action, targetType, targetID string, metadata map[string]string) error {
	return audit.Record(ctx, tx, audit.Entry{
		TenantID: principal.TenantID, ActorType: audit.ActorUser, ActorID: principal.UserID.String(),
		Action: action, TargetType: targetType, TargetID: targetID, Metadata: metadata,
	})
}
