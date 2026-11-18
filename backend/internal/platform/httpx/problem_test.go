package httpx

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/shared/apperr"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/shared/reqctx"
)

func writeProblem(t *testing.T, err error) (*httptest.ResponseRecorder, Problem, string) {
	t.Helper()
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/resource", nil)
	req = req.WithContext(reqctx.WithCorrelationID(req.Context(), "corr-1"))
	rec := httptest.NewRecorder()

	WriteError(rec, req, logger, err)

	var problem Problem
	if decodeErr := json.Unmarshal(rec.Body.Bytes(), &problem); decodeErr != nil {
		t.Fatalf("invalid problem body %q: %v", rec.Body.String(), decodeErr)
	}
	return rec, problem, logs.String()
}

func TestWriteErrorMapsKindsToStatus(t *testing.T) {
	tests := []struct {
		kind apperr.Kind
		want int
	}{
		{apperr.KindInvalid, http.StatusBadRequest},
		{apperr.KindUnauthenticated, http.StatusUnauthorized},
		{apperr.KindForbidden, http.StatusForbidden},
		{apperr.KindNotFound, http.StatusNotFound},
		{apperr.KindConflict, http.StatusConflict},
		{apperr.KindUnprocessable, http.StatusUnprocessableEntity},
		{apperr.KindRateLimited, http.StatusTooManyRequests},
		{apperr.KindUnavailable, http.StatusServiceUnavailable},
	}
	for _, tt := range tests {
		t.Run(http.StatusText(tt.want), func(t *testing.T) {
			rec, problem, _ := writeProblem(t, apperr.New(tt.kind, "SOME_CODE", "Safe message."))
			if rec.Code != tt.want || problem.Status != tt.want {
				t.Errorf("status = %d/%d, want %d", rec.Code, problem.Status, tt.want)
			}
			if got := rec.Header().Get("Content-Type"); got != "application/problem+json" {
				t.Errorf("Content-Type = %q", got)
			}
			if problem.Code != "SOME_CODE" || problem.Detail != "Safe message." {
				t.Errorf("unexpected problem: %+v", problem)
			}
			if problem.Instance != "/api/v1/resource" || problem.CorrelationID != "corr-1" {
				t.Errorf("missing request details: %+v", problem)
			}
		})
	}
}

func TestWriteErrorHidesInternalDetails(t *testing.T) {
	secret := errors.New("pq: password authentication failed for user app_runtime")
	tests := map[string]error{
		"plain error":   secret,
		"wrapped error": fmt.Errorf("load: %w", secret),
		"internal kind": apperr.Wrap(apperr.KindInternal, "DB_DOWN", "database detail", secret),
	}
	for name, err := range tests {
		t.Run(name, func(t *testing.T) {
			rec, problem, logs := writeProblem(t, err)
			if rec.Code != http.StatusInternalServerError || problem.Code != "INTERNAL_ERROR" {
				t.Errorf("got %d %s", rec.Code, problem.Code)
			}
			if strings.Contains(rec.Body.String(), "password") || strings.Contains(rec.Body.String(), "database detail") {
				t.Errorf("response exposed internals: %s", rec.Body.String())
			}
			if !strings.Contains(logs, "password authentication failed") {
				t.Errorf("the cause must be logged for diagnosis, logs: %s", logs)
			}
		})
	}
}

func TestWriteErrorIncludesFieldErrors(t *testing.T) {
	rec, problem, _ := writeProblem(t, apperr.Validation(apperr.FieldError{Field: "sku", Code: "required", Message: "Required."}))
	if rec.Code != http.StatusBadRequest || len(problem.Errors) != 1 || problem.Errors[0].Field != "sku" {
		t.Errorf("unexpected problem: %d %+v", rec.Code, problem)
	}
}

func TestWriteErrorMapsDeadlineAndBodyLimit(t *testing.T) {
	rec, problem, _ := writeProblem(t, fmt.Errorf("query: %w", context.DeadlineExceeded))
	if rec.Code != http.StatusGatewayTimeout || problem.Code != "REQUEST_TIMEOUT" {
		t.Errorf("deadline: got %d %s", rec.Code, problem.Code)
	}

	rec, problem, _ = writeProblem(t, &http.MaxBytesError{Limit: 10})
	if rec.Code != http.StatusRequestEntityTooLarge || problem.Code != "PAYLOAD_TOO_LARGE" {
		t.Errorf("body limit: got %d %s", rec.Code, problem.Code)
	}
}

func TestWriteErrorWritesNothingWhenClientDisconnected(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx)
	rec := httptest.NewRecorder()

	WriteError(rec, req, slog.New(slog.NewTextHandler(io.Discard, nil)), context.Canceled)

	if rec.Body.Len() != 0 {
		t.Errorf("unexpected body: %s", rec.Body.String())
	}
}

func TestHandleRoutesReturnedErrors(t *testing.T) {
	handler := Handle(slog.New(slog.NewTextHandler(io.Discard, nil)), func(http.ResponseWriter, *http.Request) error {
		return apperr.New(apperr.KindNotFound, "PRODUCT_NOT_FOUND", "Product not found.")
	})
	rec := httptest.NewRecorder()
	handler(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d", rec.Code)
	}
}
