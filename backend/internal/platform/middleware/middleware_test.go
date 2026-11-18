package middleware

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/shared/reqctx"
)

func discard() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestCorrelationKeepsValidClientID(t *testing.T) {
	const id = "0196f2c1-9b10-7a31-8d2c-4a6b9f0c1e22"
	var seen string
	handler := Correlation(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		seen = reqctx.CorrelationID(r.Context())
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(CorrelationHeader, id)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if seen != id || rec.Header().Get(CorrelationHeader) != id {
		t.Errorf("context=%q header=%q, want %q", seen, rec.Header().Get(CorrelationHeader), id)
	}
}

func TestCorrelationReplacesMalformedID(t *testing.T) {
	for _, bad := range []string{"", "abc", "injected\nlevel=ERROR", strings.Repeat("a", 500), "urn:uuid:0196f2c1-9b10-7a31-8d2c-4a6b9f0c1e22"} {
		handler := Correlation(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header["X-Correlation-Id"] = []string{bad}
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		got := rec.Header().Get(CorrelationHeader)
		if got == bad {
			t.Errorf("malformed id %q was accepted", bad)
		}
		if _, err := uuid.Parse(got); err != nil {
			t.Errorf("generated id %q is not a UUID", got)
		}
	}
}

func TestRecoverReturnsGenericProblem(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	handler := Recover(logger)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("database password is hunter2")
	}))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "hunter2") {
		t.Errorf("panic value exposed to the client: %s", rec.Body.String())
	}
	if !strings.Contains(logs.String(), "http.panic") {
		t.Errorf("panic was not logged: %s", logs.String())
	}
}

func TestRecoverPreservesAbortHandler(t *testing.T) {
	handler := Recover(discard())(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic(http.ErrAbortHandler)
	}))
	defer func() {
		if recover() != http.ErrAbortHandler { //nolint:errorlint,err113 // sentinel compared as documented by net/http
			t.Error("ErrAbortHandler must propagate")
		}
	}()
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
}

func TestSecurityHeaders(t *testing.T) {
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})

	rec := httptest.NewRecorder()
	SecurityHeaders(false)(next).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	for header, want := range map[string]string{
		"X-Content-Type-Options":  "nosniff",
		"X-Frame-Options":         "DENY",
		"Content-Security-Policy": "default-src 'none'; frame-ancestors 'none'",
		"Cache-Control":           "no-store",
	} {
		if got := rec.Header().Get(header); got != want {
			t.Errorf("%s = %q, want %q", header, got, want)
		}
	}
	if rec.Header().Get("Strict-Transport-Security") != "" {
		t.Error("HSTS must be disabled without TLS")
	}

	rec = httptest.NewRecorder()
	SecurityHeaders(true)(next).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Header().Get("Strict-Transport-Security") == "" {
		t.Error("HSTS must be enabled in hardened environments")
	}
}

func TestMaxBodyRejectsDeclaredOversize(t *testing.T) {
	called := false
	handler := MaxBody(discard(), 4)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/", strings.NewReader("too large")))

	if rec.Code != http.StatusRequestEntityTooLarge || called {
		t.Errorf("status=%d called=%v", rec.Code, called)
	}
}

func TestMaxBodyLimitsUndeclaredOversize(t *testing.T) {
	var readErr error
	handler := MaxBody(discard(), 4)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		_, readErr = io.ReadAll(r.Body)
	}))
	req := httptest.NewRequest(http.MethodPost, "/", io.NopCloser(strings.NewReader("too large")))
	req.ContentLength = -1

	handler.ServeHTTP(httptest.NewRecorder(), req)

	if readErr == nil {
		t.Error("reading past the limit must fail")
	}
}

func TestTimeoutSetsDeadline(t *testing.T) {
	var deadline time.Time
	var ok bool
	handler := Timeout(time.Minute)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		deadline, ok = r.Context().Deadline()
	}))

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	if !ok || time.Until(deadline) > time.Minute {
		t.Errorf("unexpected deadline: %v %v", deadline, ok)
	}
}

func TestAccessLogUsesRoutePatternAndHidesRawPath(t *testing.T) {
	var logs bytes.Buffer
	router := chi.NewRouter()
	router.Use(AccessLog(slog.New(slog.NewJSONHandler(&logs, nil))))
	router.Get("/customers/{id}", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusAccepted) })

	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/customers/maria-silva?email=a@b.com", nil))

	out := logs.String()
	if !strings.Contains(out, `"route":"/customers/{id}"`) || !strings.Contains(out, `"status":202`) {
		t.Errorf("unexpected access log: %s", out)
	}
	if strings.Contains(out, "maria-silva") || strings.Contains(out, "a@b.com") {
		t.Errorf("personal data in access log: %s", out)
	}
}

func TestAccessLogSkipsProbes(t *testing.T) {
	var logs bytes.Buffer
	handler := AccessLog(slog.New(slog.NewJSONHandler(&logs, nil)))(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/health/live", nil))

	if logs.Len() != 0 {
		t.Errorf("probe was logged: %s", logs.String())
	}
}

func TestRoutePatternFallsBackForUnmatchedRequests(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/random/path", nil)
	if got := routePattern(req.WithContext(context.Background())); got != unmatchedRoute {
		t.Errorf("routePattern() = %q", got)
	}
}
