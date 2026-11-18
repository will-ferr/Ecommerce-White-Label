package health

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
)

func probe(err error) func(context.Context) error {
	return func(context.Context) error { return err }
}

func newService(checks ...Check) *Service {
	return NewService(slog.New(slog.NewTextHandler(io.Discard, nil)), "1.2.3", time.Second, checks...)
}

func TestReadyStates(t *testing.T) {
	failure := errors.New("connection refused at 10.0.0.5")
	tests := map[string]struct {
		postgres, redis error
		wantCode        int
		wantStatus      string
	}{
		"all healthy":            {nil, nil, http.StatusOK, StatusOK},
		"optional dependency":    {nil, failure, http.StatusOK, StatusDegraded},
		"critical dependency":    {failure, nil, http.StatusServiceUnavailable, StatusUnavailable},
		"everything unavailable": {failure, failure, http.StatusServiceUnavailable, StatusUnavailable},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			service := newService(
				Check{Name: "postgres", Critical: true, Probe: probe(tt.postgres)},
				Check{Name: "redis", Probe: probe(tt.redis)},
			)
			rec := httptest.NewRecorder()

			service.Ready(rec, httptest.NewRequest(http.MethodGet, "/health/ready", nil))

			var report Report
			if err := json.Unmarshal(rec.Body.Bytes(), &report); err != nil {
				t.Fatalf("invalid body: %v", err)
			}
			if rec.Code != tt.wantCode || report.Status != tt.wantStatus {
				t.Errorf("got %d %s, want %d %s", rec.Code, report.Status, tt.wantCode, tt.wantStatus)
			}
			if strings.Contains(rec.Body.String(), "10.0.0.5") {
				t.Errorf("error detail exposed: %s", rec.Body.String())
			}
		})
	}
}

func TestLiveIgnoresDependencies(t *testing.T) {
	service := newService(Check{Name: "postgres", Critical: true, Probe: probe(errors.New("down"))})
	rec := httptest.NewRecorder()

	service.Live(rec, httptest.NewRequest(http.MethodGet, "/health/live", nil))

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d", rec.Code)
	}
}

func TestDrainFailsReadinessOnly(t *testing.T) {
	service := newService()
	service.Drain()

	ready := httptest.NewRecorder()
	service.Ready(ready, httptest.NewRequest(http.MethodGet, "/health/ready", nil))
	live := httptest.NewRecorder()
	service.Live(live, httptest.NewRequest(http.MethodGet, "/health/live", nil))

	if ready.Code != http.StatusServiceUnavailable || live.Code != http.StatusOK {
		t.Errorf("ready=%d live=%d", ready.Code, live.Code)
	}
}

func TestPublicDoesNotNameDependencies(t *testing.T) {
	service := newService(Check{Name: "postgres", Critical: true, Probe: probe(nil)})
	rec := httptest.NewRecorder()

	service.Public(rec, httptest.NewRequest(http.MethodGet, "/api/v1/status", nil))

	if strings.Contains(rec.Body.String(), "postgres") {
		t.Errorf("dependency name exposed: %s", rec.Body.String())
	}
	var status PublicStatus
	if err := json.Unmarshal(rec.Body.Bytes(), &status); err != nil {
		t.Fatalf("invalid body: %v", err)
	}
	if status.Status != StatusOK || status.Version != "1.2.3" {
		t.Errorf("unexpected status: %+v", status)
	}
}

func TestRunEnforcesTimeout(t *testing.T) {
	service := NewService(slog.New(slog.NewTextHandler(io.Discard, nil)), "1", 20*time.Millisecond, Check{
		Name: "slow", Critical: true,
		Probe: func(ctx context.Context) error {
			<-ctx.Done()
			return ctx.Err()
		},
	})

	start := time.Now()
	report := service.Run(context.Background())

	if report.Status != StatusUnavailable || time.Since(start) > time.Second {
		t.Errorf("status=%s elapsed=%s", report.Status, time.Since(start))
	}
}
