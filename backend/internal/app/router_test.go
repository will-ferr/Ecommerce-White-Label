package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/health"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/httpx"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/middleware"
)

func newRouter(probe func(context.Context) error) http.Handler {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewRouter(RouterDeps{
		Logger:         logger,
		Health:         health.NewService(logger, "1.2.3", time.Second, health.Check{Name: "postgres", Critical: true, Probe: probe}),
		RequestTimeout: time.Second,
		MaxBodyBytes:   1024,
	})
}

func healthy(context.Context) error { return nil }

func TestStatusEndpoint(t *testing.T) {
	rec := httptest.NewRecorder()
	newRouter(healthy).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/status", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var body health.PublicStatus
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid body: %v", err)
	}
	if body.Status != health.StatusOK || body.Version != "1.2.3" {
		t.Errorf("unexpected body: %+v", body)
	}
	if rec.Header().Get(middleware.CorrelationHeader) == "" {
		t.Error("correlation header is missing")
	}
	if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Error("security headers are missing")
	}
}

func TestStatusEndpointReportsOutage(t *testing.T) {
	rec := httptest.NewRecorder()
	router := newRouter(func(context.Context) error { return errors.New("postgres down") })

	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/status", nil))

	if rec.Code != http.StatusServiceUnavailable || strings.Contains(rec.Body.String(), "postgres") {
		t.Errorf("got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestProbesAreRouted(t *testing.T) {
	router := newRouter(healthy)
	for _, path := range []string{"/health/live", "/health/ready"} {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Errorf("%s = %d", path, rec.Code)
		}
	}
}

func TestProtocolErrorsUseProblemFormat(t *testing.T) {
	tests := map[string]struct {
		method, path string
		wantStatus   int
		wantCode     string
	}{
		"unknown route": {http.MethodGet, "/api/v1/missing", http.StatusNotFound, "ROUTE_NOT_FOUND"},
		"wrong method":  {http.MethodDelete, "/api/v1/status", http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			newRouter(healthy).ServeHTTP(rec, httptest.NewRequest(tt.method, tt.path, nil))

			var problem httpx.Problem
			if err := json.Unmarshal(rec.Body.Bytes(), &problem); err != nil {
				t.Fatalf("invalid body: %v", err)
			}
			if rec.Code != tt.wantStatus || problem.Code != tt.wantCode || problem.CorrelationID == "" {
				t.Errorf("got %d %+v", rec.Code, problem)
			}
			if got := rec.Header().Get("Content-Type"); got != "application/problem+json" {
				t.Errorf("Content-Type = %q", got)
			}
		})
	}
}
