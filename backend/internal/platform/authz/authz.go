// Package authz declares and enforces the access policy of each API endpoint.
// Routes under /api/v1 must be built with Protect, and a test fails if one is not.
package authz

import (
	"context"
	"log/slog"
	"net/http"
	"slices"

	"github.com/google/uuid"

	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/httpx"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/shared/apperr"
)

// Principal is the authenticated caller of a request.
type Principal struct {
	UserID      uuid.UUID
	TenantID    uuid.UUID
	SessionID   uuid.UUID
	Realm       string
	Permissions []string
}

// Can reports whether the principal holds a permission.
func (p Principal) Can(permission string) bool {
	return slices.Contains(p.Permissions, permission)
}

type principalKey struct{}

// WithPrincipal binds the authenticated caller to the context.
func WithPrincipal(ctx context.Context, principal Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, principal)
}

// FromContext returns the authenticated caller.
func FromContext(ctx context.Context) (Principal, bool) {
	principal, ok := ctx.Value(principalKey{}).(Principal)
	return principal, ok
}

// Policy states who may call an endpoint.
type Policy struct {
	authenticated bool
	permission    string
}

// Public allows anonymous callers.
var Public = Policy{}

// Authenticated allows any signed-in caller.
var Authenticated = Policy{authenticated: true}

// Permission allows callers holding the given permission.
func Permission(code string) Policy {
	return Policy{authenticated: true, permission: code}
}

// IsPublic reports whether the policy allows anonymous callers.
func (p Policy) IsPublic() bool { return !p.authenticated }

// Endpoint is a handler bound to its access policy.
type Endpoint struct {
	Policy  Policy
	handler http.Handler
	logger  *slog.Logger
}

// Protect binds a handler to a policy, which is checked on every request.
func Protect(logger *slog.Logger, policy Policy, handler http.HandlerFunc) *Endpoint {
	return &Endpoint{Policy: policy, handler: handler, logger: logger}
}

// ServeHTTP implements http.Handler.
func (e *Endpoint) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if e.Policy.authenticated {
		principal, ok := FromContext(r.Context())
		if !ok {
			w.Header().Set("WWW-Authenticate", "Bearer")
			httpx.WriteError(w, r, e.logger, apperr.New(apperr.KindUnauthenticated, "AUTHENTICATION_REQUIRED", "Sign in to continue."))
			return
		}
		if e.Policy.permission != "" && !principal.Can(e.Policy.permission) {
			httpx.WriteError(w, r, e.logger, apperr.New(apperr.KindForbidden, "PERMISSION_DENIED", "You do not have permission to do this."))
			return
		}
	}
	e.handler.ServeHTTP(w, r)
}
