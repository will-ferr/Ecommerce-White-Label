// Package app contains the use cases of the tenants module.
package app

import (
	"context"
	"errors"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/modules/tenants/internal/domain"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/shared/apperr"
)

// Cache settings. A suspended store stops serving within HitTTL.
const (
	HitTTL     = 30 * time.Second
	MissTTL    = 10 * time.Second
	MaxEntries = 10_000
)

// ErrHostNotFound means no verified domain matches the host.
var ErrHostNotFound = errors.New("host not found")

// HostLookup finds the tenant of a verified host.
type HostLookup interface {
	LookupHost(ctx context.Context, host string) (domain.ResolvedTenant, error)
}

type cacheEntry struct {
	tenant  domain.ResolvedTenant
	found   bool
	expires time.Time
}

// Resolver maps hosts to tenants. Unknown hosts are cached too, so random Host
// headers cannot be used to flood the database.
type Resolver struct {
	lookup  HostLookup
	now     func() time.Time
	group   singleflight.Group
	mu      sync.RWMutex
	entries map[string]cacheEntry
}

// NewResolver creates a Resolver.
func NewResolver(lookup HostLookup, now func() time.Time) *Resolver {
	return &Resolver{lookup: lookup, now: now, entries: make(map[string]cacheEntry)}
}

// Resolve returns the tenant allowed to serve a raw Host header value.
func (r *Resolver) Resolve(ctx context.Context, rawHost string) (domain.ResolvedTenant, error) {
	host, ok := domain.NormalizeHost(rawHost)
	if !ok {
		return domain.ResolvedTenant{}, storeNotFound()
	}

	entry, ok := r.cached(host)
	if !ok {
		var err error
		entry, err = r.load(ctx, host)
		if err != nil {
			return domain.ResolvedTenant{}, err
		}
	}

	if !entry.found {
		return domain.ResolvedTenant{}, storeNotFound()
	}
	if !entry.tenant.IsActive() {
		return domain.ResolvedTenant{}, apperr.New(apperr.KindForbidden, "STORE_UNAVAILABLE", "This store is currently unavailable.")
	}
	return entry.tenant, nil
}

func storeNotFound() error {
	return apperr.New(apperr.KindNotFound, "STORE_NOT_FOUND", "No store is configured for this address.")
}

func (r *Resolver) cached(host string) (cacheEntry, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	entry, ok := r.entries[host]
	return entry, ok && r.now().Before(entry.expires)
}

func (r *Resolver) load(ctx context.Context, host string) (cacheEntry, error) {
	value, err, _ := r.group.Do(host, func() (any, error) {
		tenant, err := r.lookup.LookupHost(context.WithoutCancel(ctx), host)
		switch {
		case err == nil:
			return r.store(host, cacheEntry{tenant: tenant, found: true, expires: r.now().Add(HitTTL)}), nil
		case errors.Is(err, ErrHostNotFound):
			return r.store(host, cacheEntry{expires: r.now().Add(MissTTL)}), nil
		default:
			return cacheEntry{}, apperr.Wrap(apperr.KindUnavailable, "STORE_LOOKUP_FAILED", "The store could not be identified right now.", err)
		}
	})
	if err != nil {
		return cacheEntry{}, err
	}
	entry, ok := value.(cacheEntry)
	if !ok {
		return cacheEntry{}, errors.New("unexpected resolver result")
	}
	return entry, nil
}

func (r *Resolver) store(host string, entry cacheEntry) cacheEntry {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.entries) >= MaxEntries {
		now := r.now()
		for key, existing := range r.entries {
			if !now.Before(existing.expires) {
				delete(r.entries, key)
			}
		}
		if len(r.entries) >= MaxEntries {
			clear(r.entries)
		}
	}
	r.entries[host] = entry
	return entry
}
