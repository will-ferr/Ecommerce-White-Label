// Package middleware contains the HTTP middlewares applied to every request.
package middleware

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/httpx"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/shared/reqctx"
)

// CorrelationHeader carries the identifier used to correlate a request.
const CorrelationHeader = "X-Correlation-ID"

const unmatchedRoute = "unmatched"

// Correlation reuses a well-formed client identifier or creates a new one.
// Anything other than a UUID is discarded to prevent log injection.
func Correlation(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(CorrelationHeader)
		if _, err := uuid.Parse(id); err != nil || len(id) != 36 {
			id = newID()
		}
		w.Header().Set(CorrelationHeader, id)
		next.ServeHTTP(w, r.WithContext(reqctx.WithCorrelationID(r.Context(), id)))
	})
}

func newID() string {
	id, err := uuid.NewV7()
	if err != nil {
		return uuid.NewString()
	}
	return id.String()
}

// AccessLog records one line per request. It logs the route pattern and never
// the raw path or query string, which may contain personal data.
func AccessLog(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if isProbe(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}
			start := time.Now()
			ww := chimw.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r)

			logger.LogAttrs(r.Context(), levelFor(ww.Status()), "http.request",
				slog.String("method", r.Method),
				slog.String("route", routePattern(r)),
				slog.Int("status", ww.Status()),
				slog.Int("bytes", ww.BytesWritten()),
				slog.Int64("duration_ms", time.Since(start).Milliseconds()),
			)
		})
	}
}

func levelFor(status int) slog.Level {
	switch {
	case status >= http.StatusInternalServerError:
		return slog.LevelError
	case status >= http.StatusBadRequest:
		return slog.LevelWarn
	default:
		return slog.LevelInfo
	}
}

func isProbe(path string) bool { return strings.HasPrefix(path, "/health/") }

// IsTraced reports whether a request should produce telemetry.
func IsTraced(r *http.Request) bool { return !isProbe(r.URL.Path) }

// RouteTelemetry names the span and labels metrics with the route pattern, so
// raw URLs and tenant hosts never become metric labels.
func RouteTelemetry(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r)

		route := routePattern(r)
		attr := attribute.String("http.route", route)
		span := trace.SpanFromContext(r.Context())
		span.SetName(r.Method + " " + route)
		span.SetAttributes(attr)
		if labeler, ok := otelhttp.LabelerFromContext(r.Context()); ok {
			labeler.Add(attr)
		}
	})
}

func routePattern(r *http.Request) string {
	if rc := chi.RouteContext(r.Context()); rc != nil {
		if pattern := rc.RoutePattern(); pattern != "" && pattern != "/*" {
			return pattern
		}
	}
	return unmatchedRoute
}

// Recover turns a panic into a generic 500 response.
func Recover(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				recovered := recover()
				if recovered == nil {
					return
				}
				if recovered == http.ErrAbortHandler { //nolint:errorlint,err113 // sentinel compared as documented by net/http
					panic(recovered)
				}
				logger.ErrorContext(r.Context(), "http.panic", "stack", string(debug.Stack()))
				httpx.WriteError(w, r, logger, fmt.Errorf("panic: %v", recovered))
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// SecurityHeaders applies restrictive defaults suited to a JSON API.
func SecurityHeaders(hsts bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("X-Frame-Options", "DENY")
			h.Set("Referrer-Policy", "no-referrer")
			h.Set("Content-Security-Policy", "default-src 'none'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
			h.Set("Cross-Origin-Resource-Policy", "same-origin")
			h.Set("Cache-Control", "no-store")
			if hsts {
				h.Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
			}
			next.ServeHTTP(w, r)
		})
	}
}

// MaxBody rejects bodies larger than limit.
func MaxBody(logger *slog.Logger, limit int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.ContentLength > limit {
				httpx.WriteError(w, r, logger, &http.MaxBytesError{Limit: limit})
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, limit)
			next.ServeHTTP(w, r)
		})
	}
}

// Timeout sets a deadline that handlers and their dependencies must honor.
func Timeout(limit time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), limit)
			defer cancel()
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
