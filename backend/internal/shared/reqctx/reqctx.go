// Package reqctx stores request-scoped values shared across platform packages.
package reqctx

import "context"

type correlationKey struct{}

// WithCorrelationID returns a context carrying the correlation ID.
func WithCorrelationID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, correlationKey{}, id)
}

// CorrelationID returns the correlation ID, or an empty string if none is set.
func CorrelationID(ctx context.Context) string {
	id, _ := ctx.Value(correlationKey{}).(string)
	return id
}
