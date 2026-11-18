// Package httpx centralizes how the API writes responses and errors.
package httpx

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/shared/apperr"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/shared/reqctx"
)

// Problem is an RFC 9457 error response.
type Problem struct {
	Type          string              `json:"type"`
	Title         string              `json:"title"`
	Status        int                 `json:"status"`
	Detail        string              `json:"detail,omitempty"`
	Instance      string              `json:"instance,omitempty"`
	Code          string              `json:"code"`
	CorrelationID string              `json:"correlation_id,omitempty"`
	Errors        []apperr.FieldError `json:"errors,omitempty"`
}

// HandlerFunc is an HTTP handler that returns its failure to a single place.
type HandlerFunc func(w http.ResponseWriter, r *http.Request) error

// Handle adapts a HandlerFunc and writes any returned error as a Problem.
func Handle(logger *slog.Logger, fn HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := fn(w, r); err != nil {
			WriteError(w, r, logger, err)
		}
	}
}

// WriteJSON writes a JSON response. An encoding failure can only be logged by
// the caller, because the status line has already been sent.
func WriteJSON(w http.ResponseWriter, status int, body any) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	return json.NewEncoder(w).Encode(body)
}

// WriteError converts err into a Problem. Unexpected errors are logged in full
// and answered with a generic message.
func WriteError(w http.ResponseWriter, r *http.Request, logger *slog.Logger, err error) {
	ctx := r.Context()

	if errors.Is(err, context.Canceled) && ctx.Err() != nil {
		logger.DebugContext(ctx, "http.request.canceled")
		return
	}

	problem := toProblem(err)
	problem.Instance = r.URL.Path
	problem.CorrelationID = reqctx.CorrelationID(ctx)

	if problem.Status >= http.StatusInternalServerError {
		span := trace.SpanFromContext(ctx)
		span.RecordError(err)
		span.SetStatus(codes.Error, problem.Code)
		logger.ErrorContext(ctx, "http.request.failed", "code", problem.Code, "status", problem.Status, "error", err)
	}

	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(problem.Status)
	if encodeErr := json.NewEncoder(w).Encode(problem); encodeErr != nil {
		logger.WarnContext(ctx, "http.response.write_failed", "error", encodeErr)
	}
}

// WriteProblem writes a protocol-level error that has no matching error kind.
func WriteProblem(w http.ResponseWriter, r *http.Request, logger *slog.Logger, status int, code, detail string) {
	ctx := r.Context()
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	err := json.NewEncoder(w).Encode(Problem{
		Type:          typeURI(code),
		Title:         http.StatusText(status),
		Status:        status,
		Detail:        detail,
		Instance:      r.URL.Path,
		Code:          code,
		CorrelationID: reqctx.CorrelationID(ctx),
	})
	if err != nil {
		logger.WarnContext(ctx, "http.response.write_failed", "error", err)
	}
}

func toProblem(err error) Problem {
	if appErr, ok := apperr.As(err); ok {
		status := statusFor(appErr.Kind)
		if status == http.StatusInternalServerError {
			return internalProblem()
		}
		return Problem{
			Type:   typeURI(appErr.Code),
			Title:  http.StatusText(status),
			Status: status,
			Detail: appErr.Message,
			Code:   appErr.Code,
			Errors: appErr.Fields,
		}
	}

	if errors.Is(err, context.DeadlineExceeded) {
		return Problem{
			Type:   typeURI("REQUEST_TIMEOUT"),
			Title:  http.StatusText(http.StatusGatewayTimeout),
			Status: http.StatusGatewayTimeout,
			Detail: "The request took too long to complete.",
			Code:   "REQUEST_TIMEOUT",
		}
	}

	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		return Problem{
			Type:   typeURI("PAYLOAD_TOO_LARGE"),
			Title:  http.StatusText(http.StatusRequestEntityTooLarge),
			Status: http.StatusRequestEntityTooLarge,
			Detail: "The request body is too large.",
			Code:   "PAYLOAD_TOO_LARGE",
		}
	}

	return internalProblem()
}

func internalProblem() Problem {
	return Problem{
		Type:   typeURI("INTERNAL_ERROR"),
		Title:  http.StatusText(http.StatusInternalServerError),
		Status: http.StatusInternalServerError,
		Detail: "An unexpected error occurred.",
		Code:   "INTERNAL_ERROR",
	}
}

func typeURI(code string) string { return "urn:ecommerce:problem:" + code }

func statusFor(kind apperr.Kind) int {
	switch kind {
	case apperr.KindInvalid:
		return http.StatusBadRequest
	case apperr.KindUnauthenticated:
		return http.StatusUnauthorized
	case apperr.KindForbidden:
		return http.StatusForbidden
	case apperr.KindNotFound:
		return http.StatusNotFound
	case apperr.KindConflict:
		return http.StatusConflict
	case apperr.KindUnprocessable:
		return http.StatusUnprocessableEntity
	case apperr.KindRateLimited:
		return http.StatusTooManyRequests
	case apperr.KindUnavailable:
		return http.StatusServiceUnavailable
	case apperr.KindInternal:
		return http.StatusInternalServerError
	default:
		return http.StatusInternalServerError
	}
}
