package ratelimit

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func discard() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestLocalLimiterEnforcesRule(t *testing.T) {
	limiter := New(nil, discard())
	rule := Rule{Max: 3, Window: time.Minute}

	for i := range 3 {
		if !limiter.Allow(context.Background(), "key", rule).Allowed {
			t.Fatalf("attempt %d was denied", i+1)
		}
	}
	decision := limiter.Allow(context.Background(), "key", rule)
	if decision.Allowed || decision.RetryAfter <= 0 {
		t.Errorf("decision = %+v", decision)
	}
	if !limiter.Allow(context.Background(), "other", rule).Allowed {
		t.Error("keys must be independent")
	}
}

func TestLocalWindowExpires(t *testing.T) {
	limiter := New(nil, discard())
	rule := Rule{Max: 1, Window: 10 * time.Millisecond}

	limiter.Allow(context.Background(), "key", rule)
	time.Sleep(20 * time.Millisecond)

	if !limiter.Allow(context.Background(), "key", rule).Allowed {
		t.Error("a new window must allow the action")
	}
}

func TestFallsBackToMemoryWhenRedisIsDown(t *testing.T) {
	client := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1", MaxRetries: -1, DialTimeout: 50 * time.Millisecond})
	t.Cleanup(func() { _ = client.Close() })
	limiter := New(client, discard())
	rule := Rule{Max: 1, Window: time.Minute}

	if !limiter.Allow(context.Background(), "key", rule).Allowed {
		t.Fatal("the first attempt must pass")
	}
	if limiter.Allow(context.Background(), "key", rule).Allowed {
		t.Error("the limit must still apply without Redis")
	}
}

func TestMiddlewareAnswers429WithRetryAfter(t *testing.T) {
	limiter := New(nil, discard())
	handler := limiter.Middleware("test", Rule{Max: 1, Window: time.Minute}, func(*http.Request) string { return "ip" })(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }),
	)

	first := httptest.NewRecorder()
	handler.ServeHTTP(first, httptest.NewRequest(http.MethodPost, "/", nil))
	second := httptest.NewRecorder()
	handler.ServeHTTP(second, httptest.NewRequest(http.MethodPost, "/", nil))

	if first.Code != http.StatusNoContent || second.Code != http.StatusTooManyRequests {
		t.Fatalf("codes = %d, %d", first.Code, second.Code)
	}
	if second.Header().Get("Retry-After") == "" {
		t.Error("Retry-After is missing")
	}
}
