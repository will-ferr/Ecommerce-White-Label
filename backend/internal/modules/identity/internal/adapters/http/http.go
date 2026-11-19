// Package http exposes the identity use cases over HTTP.
package http

import (
	"context"
	"encoding/hex"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/modules/identity/internal/app"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/modules/identity/internal/domain"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/authz"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/httpx"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/ratelimit"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/security"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/tenancy"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/shared/apperr"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/shared/reqctx"
)

const sessionCookieMaxAge = 30 * 24 * time.Hour

// Limits of the sensitive operations.
var (
	loginByIP       = ratelimit.Rule{Max: 30, Window: 15 * time.Minute}
	loginByAccount  = ratelimit.Rule{Max: 10, Window: 15 * time.Minute}
	registerByIP    = ratelimit.Rule{Max: 10, Window: time.Hour}
	forgotByIP      = ratelimit.Rule{Max: 10, Window: time.Hour}
	forgotByAccount = ratelimit.Rule{Max: 3, Window: time.Hour}
	tokenByIP       = ratelimit.Rule{Max: 20, Window: 15 * time.Minute}
	mfaByIP         = ratelimit.Rule{Max: 20, Window: 15 * time.Minute}
	refreshByIP     = ratelimit.Rule{Max: 120, Window: time.Minute}
	passwordByUser  = ratelimit.Rule{Max: 10, Window: 15 * time.Minute}
)

// Handler serves one portal.
type Handler struct {
	service      *app.Service
	portal       app.Portal
	limiter      *ratelimit.Limiter
	logger       *slog.Logger
	cookieSecure bool
}

// NewHandler creates a Handler. A nil limiter disables rate limiting.
func NewHandler(service *app.Service, portal app.Portal, limiter *ratelimit.Limiter, logger *slog.Logger, cookieSecure bool) *Handler {
	return &Handler{service: service, portal: portal, limiter: limiter, logger: logger, cookieSecure: cookieSecure}
}

func (h *Handler) wrap(fn httpx.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		err := fn(w, r)
		if err == nil {
			return
		}
		var limited *ratelimit.LimitedError
		if errors.As(err, &limited) {
			ratelimit.SetRetryAfter(w, limited.RetryAfter)
		}
		httpx.WriteError(w, r, h.logger, err)
	}
}

func (h *Handler) route(router chi.Router, method, pattern string, policy authz.Policy, fn httpx.HandlerFunc) {
	router.Method(method, pattern, authz.Protect(h.logger, policy, h.wrap(fn)))
}

func (h *Handler) limit(ctx context.Context, scope, key string, rule ratelimit.Rule) error {
	if h.limiter == nil {
		return nil
	}
	return h.limiter.Enforce(ctx, h.portal.Name+":"+scope, key, rule)
}

func (h *Handler) limitIP(r *http.Request, scope string, rule ratelimit.Rule) error {
	return h.limit(r.Context(), scope+":ip", reqctx.ClientIP(r.Context()), rule)
}

func accountKey(tenantID uuid.UUID, email string) string {
	normalized := strings.ToLower(strings.TrimSpace(email))
	return tenantID.String() + ":" + hex.EncodeToString(security.HashToken(normalized))
}

func client(r *http.Request) app.Client {
	return app.Client{IP: reqctx.ClientIP(r.Context()), Device: domain.DeviceLabel(r.UserAgent())}
}

func hostname(hostport string) string {
	if host, _, err := net.SplitHostPort(hostport); err == nil {
		return host
	}
	return hostport
}

// requireSameOrigin protects the endpoints authenticated by cookie from
// cross-site requests.
func requireSameOrigin(r *http.Request) error {
	denied := apperr.New(apperr.KindForbidden, "ORIGIN_NOT_ALLOWED", "This request is not allowed from another site.")
	origin, err := url.Parse(r.Header.Get("Origin"))
	if err != nil || origin.Host == "" || !strings.EqualFold(origin.Hostname(), hostname(r.Host)) {
		return denied
	}
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" {
		return denied
	}
	return nil
}

func (h *Handler) cookieName() string {
	if h.cookieSecure {
		return "__Host-wl_refresh"
	}
	return "wl_refresh"
}

func (h *Handler) setRefreshCookie(w http.ResponseWriter, token string, maxAge time.Duration) {
	http.SetCookie(w, &http.Cookie{ //nolint:gosec // Secure is mandatory in staging and production, enforced by config.
		Name: h.cookieName(), Value: token, Path: "/", HttpOnly: true, Secure: h.cookieSecure,
		SameSite: http.SameSiteLaxMode, MaxAge: int(maxAge.Seconds()),
	})
}

func (h *Handler) clearRefreshCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{ //nolint:gosec // Secure is mandatory in staging and production, enforced by config.
		Name: h.cookieName(), Path: "/", HttpOnly: true, Secure: h.cookieSecure,
		SameSite: http.SameSiteLaxMode, MaxAge: -1,
	})
}

func (h *Handler) respondAuth(w http.ResponseWriter, result app.AuthResult) error {
	if result.RefreshToken != "" {
		h.setRefreshCookie(w, result.RefreshToken, sessionCookieMaxAge)
	}
	return httpx.WriteJSON(w, http.StatusOK, result)
}

// scopeTenant selects the tenant of an anonymous request. Stores are identified
// by their domain, staff by the store they type, and operators by the reserved tenant.
func (h *Handler) scopeTenant(ctx context.Context, store string) (context.Context, error) {
	switch h.portal.Name {
	case app.Platform.Name:
		return tenancy.WithTenant(ctx, app.PlatformTenantID), nil
	case app.Staff.Name:
		tenantID, err := h.service.ResolveStore(ctx, store)
		if err != nil {
			return nil, err
		}
		return tenancy.WithTenant(ctx, tenantID), nil
	}
	return ctx, nil
}

// Authenticate reads the bearer token, when present. Endpoints decide through
// their policy whether a caller is required.
func (h *Handler) Authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := r.Header.Get("Authorization")
		if header == "" {
			next.ServeHTTP(w, r)
			return
		}
		raw, ok := strings.CutPrefix(header, "Bearer ")
		if !ok {
			httpx.WriteError(w, r, h.logger, apperr.New(apperr.KindUnauthenticated, "SESSION_EXPIRED", "Your session has expired. Sign in again."))
			return
		}
		principal, err := h.service.Authenticate(r.Context(), h.portal, raw)
		if err != nil {
			httpx.WriteError(w, r, h.logger, err)
			return
		}
		ctx := tenancy.WithTenant(r.Context(), principal.TenantID)
		reqctx.SetTenantID(ctx, principal.TenantID.String())
		next.ServeHTTP(w, r.WithContext(authz.WithPrincipal(ctx, principal)))
	})
}

// Register adds the routes of the portal.
func (h *Handler) Register(router chi.Router) {
	router.Use(h.Authenticate)

	public := authz.Public
	if h.portal.SelfRegistration {
		h.route(router, http.MethodPost, "/register", public, h.register)
		h.route(router, http.MethodPost, "/email/verify", public, h.verifyEmail)
	}
	h.route(router, http.MethodPost, "/login", public, h.login)
	h.route(router, http.MethodPost, "/mfa/challenge", public, h.mfaChallenge)
	h.route(router, http.MethodPost, "/refresh", public, h.refresh)
	h.route(router, http.MethodPost, "/logout", public, h.logout)
	h.route(router, http.MethodPost, "/password/forgot", public, h.forgotPassword)
	h.route(router, http.MethodPost, "/password/reset", public, h.resetPassword)
	if h.portal.MFARequired {
		h.route(router, http.MethodPost, "/mfa/setup/start", public, h.mfaSetupStart)
		h.route(router, http.MethodPost, "/mfa/setup/confirm", public, h.mfaSetupConfirm)
	}

	signedIn := authz.Authenticated
	h.route(router, http.MethodGet, "/me", signedIn, h.me)
	h.route(router, http.MethodPost, "/password/change", signedIn, h.changePassword)
	h.route(router, http.MethodGet, "/sessions", signedIn, h.listSessions)
	h.route(router, http.MethodDelete, "/sessions", signedIn, h.revokeOtherSessions)
	h.route(router, http.MethodDelete, "/sessions/{id}", signedIn, h.revokeSession)
	h.route(router, http.MethodGet, "/access-history", signedIn, h.accessHistory)
	h.route(router, http.MethodPost, "/mfa/enroll", signedIn, h.mfaEnroll)
	h.route(router, http.MethodPost, "/mfa/enroll/confirm", signedIn, h.mfaEnrollConfirm)
	h.route(router, http.MethodPost, "/mfa/disable", signedIn, h.mfaDisable)
	h.route(router, http.MethodPost, "/mfa/recovery-codes", signedIn, h.mfaRecoveryCodes)

	if h.portal.TopRole != "" {
		h.route(router, http.MethodPost, "/invitation/accept", public, h.acceptInvitation)
		h.route(router, http.MethodGet, "/roles", authz.Permission("team:read"), h.listRoles)
		h.route(router, http.MethodGet, "/team", authz.Permission("team:read"), h.listTeam)
		h.route(router, http.MethodPost, "/team/invitations", authz.Permission("team:manage"), h.inviteMember)
		h.route(router, http.MethodPut, "/team/{id}/role", authz.Permission("team:manage"), h.changeRole)
		h.route(router, http.MethodPost, "/team/{id}/deactivate", authz.Permission("team:manage"), h.deactivateMember)
		h.route(router, http.MethodGet, "/audit", authz.Permission("audit:read"), h.listAudit)
	}
}

func principalOf(r *http.Request) authz.Principal {
	principal, _ := authz.FromContext(r.Context())
	return principal
}

func pathID(r *http.Request) (uuid.UUID, error) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		return uuid.Nil, apperr.New(apperr.KindNotFound, "RESOURCE_NOT_FOUND", "The resource does not exist.")
	}
	return id, nil
}
