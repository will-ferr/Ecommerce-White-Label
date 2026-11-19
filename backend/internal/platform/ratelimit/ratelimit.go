// Package ratelimit limits how often a key may perform an action.
package ratelimit

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/httpx"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/shared/apperr"
)

const maxLocalKeys = 50_000

// Rule allows Max actions per Window.
type Rule struct {
	Max    int64
	Window time.Duration
}

// Decision is the outcome of a check.
type Decision struct {
	Allowed    bool
	RetryAfter time.Duration
}

var fixedWindow = redis.NewScript(`
local count = redis.call('INCR', KEYS[1])
if count == 1 then redis.call('PEXPIRE', KEYS[1], ARGV[1]) end
return {count, redis.call('PTTL', KEYS[1])}
`)

// Limiter counts actions in Redis. If Redis fails, each instance keeps counting
// in memory, so protection is reduced but never disappears.
type Limiter struct {
	client  *redis.Client
	logger  *slog.Logger
	counter metric.Int64Counter
	mu      sync.Mutex
	local   map[string]localWindow
}

type localWindow struct {
	count   int64
	expires time.Time
}

// New creates a Limiter. A nil client counts only in memory.
func New(client *redis.Client, logger *slog.Logger) *Limiter {
	counter, err := otel.Meter("ratelimit").Int64Counter("security.rate_limited")
	if err != nil {
		logger.Warn("ratelimit.metric_failed", "error", err)
	}
	return &Limiter{client: client, logger: logger, counter: counter, local: make(map[string]localWindow)}
}

// Allow records one action for key and reports whether it is within the rule.
func (l *Limiter) Allow(ctx context.Context, key string, rule Rule) Decision {
	if l.client != nil {
		values, err := fixedWindow.Run(ctx, l.client, []string{"rl:" + key}, rule.Window.Milliseconds()).Int64Slice()
		if err == nil && len(values) == 2 {
			return decide(values[0], time.Duration(values[1])*time.Millisecond, rule)
		}
		l.logger.WarnContext(ctx, "ratelimit.redis_unavailable", "error", err)
	}
	return l.allowLocal(key, rule)
}

func decide(count int64, remaining time.Duration, rule Rule) Decision {
	if count <= rule.Max {
		return Decision{Allowed: true}
	}
	if remaining <= 0 {
		remaining = rule.Window
	}
	return Decision{RetryAfter: remaining}
}

func (l *Limiter) allowLocal(key string, rule Rule) Decision {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()

	window, ok := l.local[key]
	if !ok || !now.Before(window.expires) {
		if len(l.local) >= maxLocalKeys {
			clear(l.local)
		}
		window = localWindow{expires: now.Add(rule.Window)}
	}
	window.count++
	l.local[key] = window
	return decide(window.count, window.expires.Sub(now), rule)
}

// Enforce returns the error for a denied action, or nil when it is allowed.
func (l *Limiter) Enforce(ctx context.Context, scope, key string, rule Rule) error {
	decision := l.Allow(ctx, scope+":"+key, rule)
	if decision.Allowed {
		return nil
	}
	if l.counter != nil {
		l.counter.Add(ctx, 1, metric.WithAttributes(attribute.String("scope", scope)))
	}
	l.logger.WarnContext(ctx, "security.rate_limited", "scope", scope)
	return &LimitedError{RetryAfter: decision.RetryAfter}
}

// LimitedError means an action exceeded its rule.
type LimitedError struct {
	RetryAfter time.Duration
}

// Error implements the error interface.
func (e *LimitedError) Error() string { return "rate limit exceeded" }

// Unwrap maps the failure to the API error format.
func (e *LimitedError) Unwrap() error {
	return apperr.New(apperr.KindRateLimited, "RATE_LIMITED", "Too many attempts. Try again later.")
}

// SetRetryAfter adds the Retry-After header for a limited request.
func SetRetryAfter(w http.ResponseWriter, retryAfter time.Duration) {
	seconds := int64(retryAfter/time.Second) + 1
	w.Header().Set("Retry-After", strconv.FormatInt(seconds, 10))
}

// Middleware limits requests by the key computed from each request.
func (l *Limiter) Middleware(scope string, rule Rule, key func(*http.Request) string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if err := l.Enforce(r.Context(), scope, key(r), rule); err != nil {
				if limited, ok := err.(*LimitedError); ok { //nolint:errorlint // Enforce returns this type directly
					SetRetryAfter(w, limited.RetryAfter)
				}
				httpx.WriteError(w, r, l.logger, err)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
