package app

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/modules/tenants/internal/domain"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/shared/apperr"
)

type fakeLookup struct {
	calls   atomic.Int32
	tenants map[string]domain.ResolvedTenant
	err     error
	gate    chan struct{}
}

func (f *fakeLookup) LookupHost(_ context.Context, host string) (domain.ResolvedTenant, error) {
	f.calls.Add(1)
	if f.gate != nil {
		<-f.gate
	}
	if f.err != nil {
		return domain.ResolvedTenant{}, f.err
	}
	tenant, ok := f.tenants[host]
	if !ok {
		return domain.ResolvedTenant{}, ErrHostNotFound
	}
	return tenant, nil
}

type clock struct{ now time.Time }

func (c *clock) Now() time.Time { return c.now }

func active() domain.ResolvedTenant {
	return domain.ResolvedTenant{ID: uuid.New(), Status: domain.StatusActive, PlanCode: "starter"}
}

func wantCode(t *testing.T, err error, code string) {
	t.Helper()
	appErr, ok := apperr.As(err)
	if !ok || appErr.Code != code {
		t.Fatalf("error = %v, want code %s", err, code)
	}
}

func TestResolveReturnsTenantOfNormalizedHost(t *testing.T) {
	tenant := active()
	resolver := NewResolver(&fakeLookup{tenants: map[string]domain.ResolvedTenant{"loja.com.br": tenant}}, time.Now)

	got, err := resolver.Resolve(context.Background(), "LOJA.com.br:443")

	if err != nil || got.ID != tenant.ID {
		t.Fatalf("Resolve() = %+v, %v", got, err)
	}
}

func TestResolveRefusesInvalidHostsWithoutQuerying(t *testing.T) {
	lookup := &fakeLookup{}
	resolver := NewResolver(lookup, time.Now)

	for _, host := range []string{"", "127.0.0.1:8080", "localhost", "api:8080", "bad host.com"} {
		_, err := resolver.Resolve(context.Background(), host)
		wantCode(t, err, "STORE_NOT_FOUND")
	}
	if lookup.calls.Load() != 0 {
		t.Errorf("invalid hosts reached the database %d times", lookup.calls.Load())
	}
}

func TestResolveRefusesSuspendedStore(t *testing.T) {
	tenant := active()
	tenant.Status = domain.StatusSuspended
	resolver := NewResolver(&fakeLookup{tenants: map[string]domain.ResolvedTenant{"loja.com.br": tenant}}, time.Now)

	_, err := resolver.Resolve(context.Background(), "loja.com.br")

	wantCode(t, err, "STORE_UNAVAILABLE")
}

func TestResolveCachesHitsUntilExpiry(t *testing.T) {
	lookup := &fakeLookup{tenants: map[string]domain.ResolvedTenant{"loja.com.br": active()}}
	now := &clock{now: time.Unix(1000, 0)}
	resolver := NewResolver(lookup, now.Now)

	for range 5 {
		if _, err := resolver.Resolve(context.Background(), "loja.com.br"); err != nil {
			t.Fatal(err)
		}
	}
	if lookup.calls.Load() != 1 {
		t.Errorf("lookups = %d, want 1", lookup.calls.Load())
	}

	now.now = now.now.Add(HitTTL)
	if _, err := resolver.Resolve(context.Background(), "loja.com.br"); err != nil {
		t.Fatal(err)
	}
	if lookup.calls.Load() != 2 {
		t.Errorf("lookups after expiry = %d, want 2", lookup.calls.Load())
	}
}

func TestResolveCachesUnknownHostsBriefly(t *testing.T) {
	lookup := &fakeLookup{tenants: map[string]domain.ResolvedTenant{}}
	now := &clock{now: time.Unix(1000, 0)}
	resolver := NewResolver(lookup, now.Now)

	for range 5 {
		_, err := resolver.Resolve(context.Background(), "desconhecida.com.br")
		wantCode(t, err, "STORE_NOT_FOUND")
	}
	if lookup.calls.Load() != 1 {
		t.Errorf("lookups = %d, want 1", lookup.calls.Load())
	}

	lookup.tenants["desconhecida.com.br"] = active()
	now.now = now.now.Add(MissTTL)
	if _, err := resolver.Resolve(context.Background(), "desconhecida.com.br"); err != nil {
		t.Errorf("a new store must appear after the short TTL: %v", err)
	}
}

func TestResolveDoesNotCacheFailures(t *testing.T) {
	lookup := &fakeLookup{err: errors.New("connection refused")}
	resolver := NewResolver(lookup, time.Now)

	_, err := resolver.Resolve(context.Background(), "loja.com.br")
	wantCode(t, err, "STORE_LOOKUP_FAILED")

	lookup.err = nil
	lookup.tenants = map[string]domain.ResolvedTenant{"loja.com.br": active()}
	if _, err := resolver.Resolve(context.Background(), "loja.com.br"); err != nil {
		t.Errorf("recovery failed: %v", err)
	}
}

func TestResolveCollapsesConcurrentLookups(t *testing.T) {
	lookup := &fakeLookup{tenants: map[string]domain.ResolvedTenant{"loja.com.br": active()}, gate: make(chan struct{})}
	resolver := NewResolver(lookup, time.Now)

	var wg sync.WaitGroup
	for range 50 {
		wg.Go(func() {
			if _, err := resolver.Resolve(context.Background(), "loja.com.br"); err != nil {
				t.Error(err)
			}
		})
	}
	for lookup.calls.Load() == 0 {
		time.Sleep(time.Millisecond)
	}
	time.Sleep(20 * time.Millisecond)
	close(lookup.gate)
	wg.Wait()

	if lookup.calls.Load() != 1 {
		t.Errorf("lookups = %d, want 1", lookup.calls.Load())
	}
}

func TestResolverCacheIsBounded(t *testing.T) {
	resolver := NewResolver(&fakeLookup{tenants: map[string]domain.ResolvedTenant{}}, time.Now)

	for i := range MaxEntries + 500 {
		_, err := resolver.Resolve(context.Background(), fmt.Sprintf("host-%d.example.com", i))
		wantCode(t, err, "STORE_NOT_FOUND")
	}

	if size := len(resolver.entries); size > MaxEntries {
		t.Errorf("cache grew to %d entries", size)
	}
}
