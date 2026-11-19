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
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/database"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/tenancy"
)

// Deps lists what the module needs.
type Deps struct {
	Pool         *pgxpool.Pool
	Logger       *slog.Logger
	AccessKey    []byte
	PaymentTTL   time.Duration
	TestPayments bool
	CookieSecure bool
}

// Service implements the sales use cases.
type Service struct {
	db           *database.TenantDB
	pool         *pgxpool.Pool
	logger       *slog.Logger
	accessKey    []byte
	paymentTTL   time.Duration
	provider     payments.Provider
	testPayments bool
	now          func() time.Time
}

// NewService creates a Service.
func NewService(deps Deps) *Service {
	return &Service{
		db: database.NewTenantDB(deps.Pool), pool: deps.Pool, logger: deps.Logger, accessKey: deps.AccessKey,
		paymentTTL: deps.PaymentTTL, provider: payments.TestProvider{}, testPayments: deps.TestPayments, now: time.Now,
	}
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
