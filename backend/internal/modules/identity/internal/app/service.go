// Package app contains the identity use cases. It works directly with the
// queries generated from SQL, which avoids mirroring them in a repository
// interface that would have a single implementation.
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/attribute"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/modules/audit"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/modules/identity/internal/domain"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/modules/identity/internal/sqlcgen"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/authz"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/config"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/database"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/mail"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/observability"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/security"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/tenancy"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/shared/apperr"
)

// PlatformTenantID is the reserved tenant of platform operators.
var PlatformTenantID = tenancy.PlatformTenantID

const (
	sessionTTL        = 30 * 24 * time.Hour
	challengeTTL      = 5 * time.Minute
	maxMFAAttempts    = 5
	refreshReuseGrace = 10 * time.Second
	resetTokenTTL     = time.Hour
	verifyTokenTTL    = 48 * time.Hour
	invitationTTL     = 7 * 24 * time.Hour
	historyLimit      = 50
	auditLimit        = 100
	uniqueViolation   = "23505"
)

// Portal is one of the three separate entry points of the platform.
type Portal struct {
	Name             string
	Realm            string
	Audience         string
	TopRole          string
	MFARequired      bool
	SelfRegistration bool
}

// The portals never share accounts or tokens.
var (
	Customer = Portal{Name: "customer", Realm: domain.RealmCustomer, Audience: "storefront", SelfRegistration: true}
	Staff    = Portal{Name: "staff", Realm: domain.RealmStaff, Audience: "staff", TopRole: "owner"}
	Platform = Portal{Name: "platform", Realm: domain.RealmPlatform, Audience: "platform", TopRole: "platform_admin", MFARequired: true}
)

// Mailer schedules outgoing e-mail.
type Mailer interface {
	Enqueue(ctx context.Context, message mail.Message)
}

// Client describes where a request came from.
type Client struct {
	IP     string
	Device string
}

// Login outcomes.
const (
	StatusAuthenticated         = "authenticated"
	StatusMFARequired           = "mfa_required"
	StatusMFAEnrollmentRequired = "mfa_enrollment_required"
)

// UserView is the account data returned to its owner.
type UserView struct {
	ID            uuid.UUID `json:"id"`
	Email         string    `json:"email"`
	Name          string    `json:"name"`
	Realm         string    `json:"realm"`
	EmailVerified bool      `json:"emailVerified"`
	MFAEnabled    bool      `json:"mfaEnabled"`
	Role          string    `json:"role,omitempty"`
	Permissions   []string  `json:"permissions"`
}

// AuthResult is the outcome of a login step. RefreshToken travels only in a cookie.
type AuthResult struct {
	Status        string    `json:"status"`
	AccessToken   string    `json:"accessToken,omitempty"`
	ExpiresIn     int       `json:"expiresIn,omitempty"`
	Challenge     string    `json:"challenge,omitempty"`
	User          *UserView `json:"user,omitempty"`
	RecoveryCodes []string  `json:"recoveryCodes,omitempty"`
	RefreshToken  string    `json:"-"`
}

// Service implements the identity use cases.
type Service struct {
	db     *database.TenantDB
	pool   *pgxpool.Pool
	hasher *security.PasswordHasher
	sealer *security.Sealer
	issuer *TokenIssuer
	mailer Mailer
	links  config.PublicLinks
	logger *slog.Logger
	now    func() time.Time
}

// Deps lists what the service needs.
type Deps struct {
	Pool   *pgxpool.Pool
	Hasher *security.PasswordHasher
	Sealer *security.Sealer
	Issuer *TokenIssuer
	Mailer Mailer
	Links  config.PublicLinks
	Logger *slog.Logger
}

// NewService creates a Service.
func NewService(deps Deps) *Service {
	return &Service{
		db: database.NewTenantDB(deps.Pool), pool: deps.Pool, hasher: deps.Hasher, sealer: deps.Sealer,
		issuer: deps.Issuer, mailer: deps.Mailer, links: deps.Links, logger: deps.Logger, now: time.Now,
	}
}

func (s *Service) in(ctx context.Context, fn func(pgx.Tx, *sqlcgen.Queries) error) error {
	return s.db.Run(ctx, func(tx pgx.Tx) error { return fn(tx, sqlcgen.New(tx)) })
}

func tenantOf(ctx context.Context) (uuid.UUID, error) {
	tenantID, ok := tenancy.FromContext(ctx)
	if !ok {
		return uuid.Nil, tenancy.ErrMissingTenant
	}
	return tenantID, nil
}

func newID() uuid.UUID {
	id, err := uuid.NewV7()
	if err != nil {
		return uuid.New()
	}
	return id
}

func invalidCredentials() error {
	return apperr.New(apperr.KindUnauthenticated, "INVALID_CREDENTIALS", "The e-mail or password is incorrect.")
}

func invalidToken() error {
	return apperr.New(apperr.KindInvalid, "INVALID_TOKEN", "This link is invalid or has expired.")
}

func sessionExpired() error {
	return apperr.New(apperr.KindUnauthenticated, "SESSION_EXPIRED", "Your session has expired. Sign in again.")
}

func invalidCode() error {
	return apperr.New(apperr.KindUnauthenticated, "INVALID_MFA_CODE", "The verification code is incorrect.")
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == uniqueViolation
}

// LinkBase returns the address used in e-mails of a portal.
func (s *Service) LinkBase(portal Portal, host string) string {
	switch portal.Name {
	case Staff.Name:
		return s.links.AdminURL
	case Platform.Name:
		return s.links.ConsoleURL
	}
	base := s.links.StoreScheme + "://" + host
	if s.links.StorePort != "" {
		base += ":" + s.links.StorePort
	}
	return base
}

func (s *Service) recordEvent(ctx context.Context, portal Portal, userID *uuid.UUID, event string, client Client) {
	observability.Count(ctx, "identity.access_events", attribute.String("portal", portal.Name), attribute.String("event", event))
	if event == "login_failed" {
		observability.SecurityEvent(ctx, "login_failed")
	}
	tenantID, err := tenantOf(ctx)
	if err != nil {
		return
	}
	err = s.in(context.WithoutCancel(ctx), func(_ pgx.Tx, q *sqlcgen.Queries) error {
		return q.InsertAccessEvent(ctx, sqlcgen.InsertAccessEventParams{
			TenantID: tenantID, ID: newID(), UserID: userID, Realm: portal.Realm, Event: event,
			Device: client.Device, Ip: client.IP,
		})
	})
	if err != nil {
		s.logger.WarnContext(ctx, "identity.access_event_failed", "event", event, "error", err)
	}
}

func userAudit(ctx context.Context, tx pgx.Tx, principal authz.Principal, action, targetType, targetID string, metadata map[string]string) error {
	return audit.Record(ctx, tx, audit.Entry{
		TenantID: principal.TenantID, ActorType: audit.ActorUser, ActorID: principal.UserID.String(),
		Action: action, TargetType: targetType, TargetID: targetID, Metadata: metadata,
	})
}

func selfAudit(ctx context.Context, tx pgx.Tx, tenantID, userID uuid.UUID, action string) error {
	return audit.Record(ctx, tx, audit.Entry{
		TenantID: tenantID, ActorType: audit.ActorUser, ActorID: userID.String(),
		Action: action, TargetType: "user", TargetID: userID.String(),
	})
}

func (s *Service) userView(ctx context.Context, q *sqlcgen.Queries, user sqlcgen.IdentityUser) (*UserView, error) {
	permissions, err := q.ListUserPermissions(ctx, sqlcgen.ListUserPermissionsParams{TenantID: user.TenantID, UserID: user.ID})
	if err != nil {
		return nil, fmt.Errorf("list permissions: %w", err)
	}
	role, err := q.GetUserRole(ctx, sqlcgen.GetUserRoleParams{TenantID: user.TenantID, UserID: user.ID})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("get role: %w", err)
	}
	if permissions == nil {
		permissions = []string{}
	}
	return &UserView{
		ID: user.ID, Email: user.Email, Name: user.Name, Realm: user.Realm,
		EmailVerified: user.EmailVerifiedAt != nil, MFAEnabled: user.MfaEnabledAt != nil,
		Role: role, Permissions: permissions,
	}, nil
}

// createUserToken replaces earlier tokens of the same kind and returns the new one.
func createUserToken(ctx context.Context, q *sqlcgen.Queries, tenantID, userID uuid.UUID, kind string, expires time.Time) (string, error) {
	if err := q.InvalidateUserTokens(ctx, sqlcgen.InvalidateUserTokensParams{TenantID: tenantID, UserID: userID, Kind: kind}); err != nil {
		return "", fmt.Errorf("invalidate tokens: %w", err)
	}
	secret, err := security.NewToken()
	if err != nil {
		return "", err
	}
	err = q.InsertUserToken(ctx, sqlcgen.InsertUserTokenParams{
		TenantID: tenantID, ID: newID(), UserID: userID, Kind: kind,
		TokenHash: security.HashToken(secret), ExpiresAt: expires,
	})
	if err != nil {
		return "", fmt.Errorf("insert token: %w", err)
	}
	return composeToken(tenantID, secret), nil
}

// bindToken scopes the context to the tenant named by a token.
func bindToken(ctx context.Context, portal Portal, token string) (context.Context, string, error) {
	tenantID, secret, ok := parseToken(token)
	if !ok {
		return nil, "", invalidToken()
	}
	if current, bound := tenancy.FromContext(ctx); bound && current != tenantID {
		return nil, "", invalidToken()
	}
	if portal.Name == Platform.Name && tenantID != PlatformTenantID {
		return nil, "", invalidToken()
	}
	return tenancy.WithTenant(ctx, tenantID), secret, nil
}
