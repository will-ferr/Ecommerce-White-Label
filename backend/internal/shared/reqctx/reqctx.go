// Package reqctx stores request-scoped values shared across platform packages.
package reqctx

import (
	"context"
	"sync/atomic"
)

type requestKey struct{}

type requestInfo struct {
	correlationID string
	tenantID      atomic.Pointer[string]
	clientIP      atomic.Pointer[string]
}

// WithCorrelationID returns a context carrying the correlation ID.
func WithCorrelationID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestKey{}, &requestInfo{correlationID: id})
}

// CorrelationID returns the correlation ID, or an empty string if none is set.
func CorrelationID(ctx context.Context) string {
	if info, ok := ctx.Value(requestKey{}).(*requestInfo); ok {
		return info.correlationID
	}
	return ""
}

// SetTenantID records the tenant for logging. The value is shared with outer
// middlewares, so the access log also reports the tenant resolved further in.
func SetTenantID(ctx context.Context, id string) {
	if info, ok := ctx.Value(requestKey{}).(*requestInfo); ok {
		info.tenantID.Store(&id)
	}
}

// TenantID returns the tenant recorded for logging, or an empty string.
func TenantID(ctx context.Context) string {
	if info, ok := ctx.Value(requestKey{}).(*requestInfo); ok {
		if id := info.tenantID.Load(); id != nil {
			return *id
		}
	}
	return ""
}

// SetClientIP records the address of the client.
func SetClientIP(ctx context.Context, ip string) {
	if info, ok := ctx.Value(requestKey{}).(*requestInfo); ok {
		info.clientIP.Store(&ip)
	}
}

// ClientIP returns the address of the client, or an empty string.
func ClientIP(ctx context.Context) string {
	if info, ok := ctx.Value(requestKey{}).(*requestInfo); ok {
		if ip := info.clientIP.Load(); ip != nil {
			return *ip
		}
	}
	return ""
}
