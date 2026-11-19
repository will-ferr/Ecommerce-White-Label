// Package tenancy carries the validated tenant of the current operation.
package tenancy

import (
	"context"
	"errors"

	"github.com/google/uuid"
)

// PlatformTenantID is the reserved tenant that holds platform operators.
var PlatformTenantID = uuid.MustParse("01960000-0000-7000-8000-000000000001")

// ErrMissingTenant means tenant data was requested without a validated tenant.
var ErrMissingTenant = errors.New("tenant context is missing")

type contextKey struct{}

// WithTenant returns a context bound to a tenant the backend has validated.
func WithTenant(ctx context.Context, tenantID uuid.UUID) context.Context {
	return context.WithValue(ctx, contextKey{}, tenantID)
}

// FromContext returns the tenant bound to ctx.
func FromContext(ctx context.Context) (uuid.UUID, bool) {
	tenantID, ok := ctx.Value(contextKey{}).(uuid.UUID)
	return tenantID, ok && tenantID != uuid.Nil
}
