// Package health reports whether the process and its dependencies can serve
// traffic.
package health

import (
	"context"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/httpx"
)

// Overall states.
const (
	StatusOK          = "ok"
	StatusDegraded    = "degraded"
	StatusUnavailable = "unavailable"
)

const (
	checkUp   = "up"
	checkDown = "down"
)

// Check probes one dependency. A failing critical check removes the instance
// from the load balancer; a non-critical failure only degrades it.
type Check struct {
	Name     string
	Critical bool
	Probe    func(context.Context) error
}

// Report is the result of running every check.
type Report struct {
	Status string            `json:"status"`
	Checks map[string]string `json:"checks"`
}

// PublicStatus is the response of the public status endpoint.
type PublicStatus struct {
	Status  string `json:"status"`
	Version string `json:"version"`
}

// Service runs health checks and exposes them over HTTP.
type Service struct {
	checks   []Check
	timeout  time.Duration
	version  string
	logger   *slog.Logger
	draining atomic.Bool
}

// NewService creates a Service.
func NewService(logger *slog.Logger, version string, timeout time.Duration, checks ...Check) *Service {
	return &Service{checks: checks, timeout: timeout, version: version, logger: logger}
}

// Drain makes readiness fail so the instance stops receiving new traffic
// before it shuts down.
func (s *Service) Drain() { s.draining.Store(true) }

// Run executes all checks concurrently.
func (s *Service) Run(ctx context.Context) Report {
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	results := make([]error, len(s.checks))
	var wg sync.WaitGroup
	for i, check := range s.checks {
		wg.Go(func() { results[i] = check.Probe(ctx) })
	}
	wg.Wait()

	report := Report{Status: StatusOK, Checks: make(map[string]string, len(s.checks))}
	for i, check := range s.checks {
		if results[i] == nil {
			report.Checks[check.Name] = checkUp
			continue
		}
		report.Checks[check.Name] = checkDown
		s.logger.WarnContext(ctx, "health.check.failed", "check", check.Name, "error", results[i])
		if check.Critical {
			report.Status = StatusUnavailable
		} else if report.Status == StatusOK {
			report.Status = StatusDegraded
		}
	}
	return report
}

// Live answers the liveness probe. It checks no dependency, because restarting
// the process would not fix an unavailable database.
func (s *Service) Live(w http.ResponseWriter, r *http.Request) {
	s.write(w, r, http.StatusOK, map[string]string{"status": StatusOK})
}

// Ready answers the readiness probe.
func (s *Service) Ready(w http.ResponseWriter, r *http.Request) {
	if s.draining.Load() {
		s.write(w, r, http.StatusServiceUnavailable, Report{Status: StatusUnavailable, Checks: map[string]string{}})
		return
	}
	report := s.Run(r.Context())
	s.write(w, r, httpStatus(report.Status), report)
}

// Public answers the public status endpoint, without naming dependencies.
func (s *Service) Public(w http.ResponseWriter, r *http.Request) {
	report := s.Run(r.Context())
	s.write(w, r, httpStatus(report.Status), PublicStatus{Status: report.Status, Version: s.version})
}

func (s *Service) write(w http.ResponseWriter, r *http.Request, status int, body any) {
	if err := httpx.WriteJSON(w, status, body); err != nil {
		s.logger.WarnContext(r.Context(), "health.response.write_failed", "error", err)
	}
}

func httpStatus(status string) int {
	if status == StatusUnavailable {
		return http.StatusServiceUnavailable
	}
	return http.StatusOK
}
