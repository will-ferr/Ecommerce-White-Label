// Package app is the composition root: it connects the platform and modules.
package app

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/health"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/httpx"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/middleware"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/shared/apperr"
)

// RouterDeps lists what the HTTP layer needs.
type RouterDeps struct {
	Logger         *slog.Logger
	Health         *health.Service
	RequestTimeout time.Duration
	MaxBodyBytes   int64
	HSTS           bool
}

// NewRouter builds the HTTP handler with its middleware chain.
func NewRouter(deps RouterDeps) http.Handler {
	router := chi.NewRouter()
	router.Use(
		middleware.Correlation,
		middleware.RouteTelemetry,
		middleware.AccessLog(deps.Logger),
		middleware.Recover(deps.Logger),
		middleware.SecurityHeaders(deps.HSTS),
		middleware.MaxBody(deps.Logger, deps.MaxBodyBytes),
		middleware.Timeout(deps.RequestTimeout),
	)

	router.NotFound(httpx.Handle(deps.Logger, func(http.ResponseWriter, *http.Request) error {
		return apperr.New(apperr.KindNotFound, "ROUTE_NOT_FOUND", "The requested resource does not exist.")
	}))
	router.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		httpx.WriteProblem(w, r, deps.Logger, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED",
			"This method is not supported for the requested resource.")
	})

	router.Get("/health/live", deps.Health.Live)
	router.Get("/health/ready", deps.Health.Ready)

	router.Route("/api/v1", func(api chi.Router) {
		api.Get("/status", deps.Health.Public)
	})

	// Every route is public, so trace context sent by clients only becomes a
	// span link. server.address is fixed so tenant hosts never become metric labels.
	return otelhttp.NewHandler(router, "http.server",
		otelhttp.WithPublicEndpointFn(func(*http.Request) bool { return true }),
		otelhttp.WithServerName("ecommerce-api"),
		otelhttp.WithFilter(middleware.IsTraced),
	)
}
