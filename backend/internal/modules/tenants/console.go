package tenants

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/modules/audit"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/modules/tenants/internal/adapters/postgres"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/modules/tenants/internal/adapters/postgres/sqlcgen"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/modules/tenants/internal/app"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/modules/tenants/internal/domain"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/authz"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/httpx"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/shared/apperr"
)

// Console exposes store management to platform operators. It uses the
// platform database role and is served only by the platform process (ADR-019).
type Console struct {
	pool       *pgxpool.Pool
	admin      *app.Admin
	logger     *slog.Logger
	baseDomain string
}

// NewConsole creates a Console. The pool must use the platform role.
func NewConsole(pool *pgxpool.Pool, logger *slog.Logger, baseDomain string, dns app.TXTResolver) *Console {
	if dns == nil {
		dns = net.DefaultResolver
	}
	return &Console{pool: pool, admin: app.NewAdmin(postgres.NewPlatformStore(pool), dns, baseDomain), logger: logger, baseDomain: baseDomain}
}

// StoreSummary is one line of the store list.
type StoreSummary struct {
	Slug      string    `json:"slug"`
	Name      string    `json:"name"`
	Status    string    `json:"status"`
	Plan      string    `json:"plan"`
	CreatedAt time.Time `json:"createdAt"`
}

// StoreDomain is a domain of a store. The verification record is shown only
// while the domain is pending.
type StoreDomain struct {
	Host              string     `json:"host"`
	Kind              string     `json:"kind"`
	VerifiedAt        *time.Time `json:"verifiedAt"`
	VerificationName  string     `json:"verificationName,omitempty"`
	VerificationValue string     `json:"verificationValue,omitempty"`
}

// StoreFeature is a feature and whether the store may and does use it.
type StoreFeature struct {
	Key     string `json:"key"`
	InPlan  bool   `json:"inPlan"`
	Enabled bool   `json:"enabled"`
}

// StoreDetail is everything an operator manages about one store.
type StoreDetail struct {
	StoreSummary
	Currency  string         `json:"currency"`
	Indexable bool           `json:"indexable"`
	Domains   []StoreDomain  `json:"domains"`
	Features  []StoreFeature `json:"features"`
}

// Plan is a commercial plan.
type Plan struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

func (c *Console) stores(ctx context.Context) ([]StoreSummary, error) {
	tenants, err := c.admin.ListTenants(ctx)
	if err != nil {
		return nil, err
	}
	stores := make([]StoreSummary, 0, len(tenants))
	for _, tenant := range tenants {
		if tenant.Slug == "platform" {
			continue
		}
		stores = append(stores, StoreSummary{Slug: tenant.Slug, Name: tenant.StoreName, Status: tenant.Status, Plan: tenant.PlanCode, CreatedAt: tenant.CreatedAt})
	}
	return stores, nil
}

func (c *Console) detail(ctx context.Context, slug string) (StoreDetail, error) {
	q := sqlcgen.New(c.pool)
	row, err := q.GetTenantDetail(ctx, slug)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && row.Slug == "platform") {
		return StoreDetail{}, apperr.New(apperr.KindNotFound, "TENANT_NOT_FOUND", "The store does not exist.")
	}
	if err != nil {
		return StoreDetail{}, fmt.Errorf("load store: %w", err)
	}
	detail := StoreDetail{
		StoreSummary: StoreSummary{Slug: row.Slug, Name: row.StoreName, Status: row.Status, Plan: row.PlanCode, CreatedAt: row.CreatedAt},
		Currency:     row.Currency, Indexable: row.SeoIndexable, Domains: []StoreDomain{}, Features: []StoreFeature{},
	}
	domains, err := q.ListTenantDomains(ctx, row.ID)
	if err != nil {
		return StoreDetail{}, fmt.Errorf("list domains: %w", err)
	}
	for _, d := range domains {
		item := StoreDomain{Host: d.Host, Kind: d.Kind, VerifiedAt: d.VerifiedAt}
		if d.VerifiedAt == nil && d.VerificationToken != nil {
			item.VerificationName, item.VerificationValue = domain.VerificationPrefix+d.Host, *d.VerificationToken
		}
		detail.Domains = append(detail.Domains, item)
	}
	features, err := q.ListFeatureStates(ctx, row.ID)
	if err != nil {
		return StoreDetail{}, fmt.Errorf("list features: %w", err)
	}
	for _, f := range features {
		detail.Features = append(detail.Features, StoreFeature{Key: f.Key, InPlan: f.InPlan, Enabled: f.TenantEnabled})
	}
	return detail, nil
}

// ownsDomain refuses to act on a domain of another store through this store's address.
func (c *Console) ownsDomain(ctx context.Context, slug, host string) error {
	detail, err := c.detail(ctx, slug)
	if err != nil {
		return err
	}
	for _, d := range detail.Domains {
		if d.Host == strings.ToLower(host) {
			return nil
		}
	}
	return apperr.New(apperr.KindNotFound, "DOMAIN_NOT_FOUND", "The domain does not exist.")
}

// operatorContext attributes changes to the signed-in operator.
func operatorContext(r *http.Request) context.Context {
	principal, _ := authz.FromContext(r.Context())
	return audit.WithOperator(r.Context(), principal.UserID.String())
}

// RegisterPlatformRoutes adds store management to the platform portal.
func (c *Console) RegisterPlatformRoutes(router chi.Router) {
	read, manage := authz.Permission("stores:read"), authz.Permission("stores:manage")
	route := func(method, pattern string, policy authz.Policy, fn httpx.HandlerFunc) {
		router.Method(method, pattern, authz.Protect(c.logger, policy, httpx.Handle(c.logger, fn)))
	}
	noContent := func(w http.ResponseWriter, err error) error {
		if err != nil {
			return err
		}
		w.WriteHeader(http.StatusNoContent)
		return nil
	}

	route(http.MethodGet, "/plans", read, func(w http.ResponseWriter, r *http.Request) error {
		rows, err := sqlcgen.New(c.pool).ListPlans(r.Context())
		if err != nil {
			return err
		}
		plans := make([]Plan, 0, len(rows))
		for _, row := range rows {
			plans = append(plans, Plan{Code: row.Code, Name: row.Name})
		}
		return httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": plans})
	})
	route(http.MethodGet, "/stores", read, func(w http.ResponseWriter, r *http.Request) error {
		stores, err := c.stores(r.Context())
		if err != nil {
			return err
		}
		return httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": stores, "baseDomain": c.baseDomain})
	})
	route(http.MethodPost, "/stores", manage, func(w http.ResponseWriter, r *http.Request) error {
		var body struct {
			Slug string `json:"slug"`
			Name string `json:"name"`
			Plan string `json:"plan"`
		}
		if err := httpx.DecodeJSON(r, &body); err != nil {
			return err
		}
		host, err := c.admin.CreateTenant(operatorContext(r), strings.TrimSpace(body.Slug), body.Name, body.Plan)
		if err != nil {
			return err
		}
		return httpx.WriteJSON(w, http.StatusCreated, map[string]string{"slug": body.Slug, "host": host})
	})
	route(http.MethodGet, "/stores/{slug}", read, func(w http.ResponseWriter, r *http.Request) error {
		detail, err := c.detail(r.Context(), chi.URLParam(r, "slug"))
		if err != nil {
			return err
		}
		return httpx.WriteJSON(w, http.StatusOK, detail)
	})
	route(http.MethodPost, "/stores/{slug}/status", manage, func(w http.ResponseWriter, r *http.Request) error {
		var body struct {
			Status string `json:"status"`
		}
		if err := httpx.DecodeJSON(r, &body); err != nil {
			return err
		}
		if _, err := c.detail(r.Context(), chi.URLParam(r, "slug")); err != nil {
			return err
		}
		return noContent(w, c.admin.SetStatus(operatorContext(r), chi.URLParam(r, "slug"), body.Status))
	})
	route(http.MethodPost, "/stores/{slug}/domains", manage, func(w http.ResponseWriter, r *http.Request) error {
		var body struct {
			Host string `json:"host"`
		}
		if err := httpx.DecodeJSON(r, &body); err != nil {
			return err
		}
		if _, err := c.detail(r.Context(), chi.URLParam(r, "slug")); err != nil {
			return err
		}
		name, token, err := c.admin.AddDomain(operatorContext(r), chi.URLParam(r, "slug"), body.Host)
		if err != nil {
			return err
		}
		return httpx.WriteJSON(w, http.StatusCreated, map[string]string{"verificationName": name, "verificationValue": token})
	})
	route(http.MethodPost, "/stores/{slug}/domains/{host}/verify", manage, func(w http.ResponseWriter, r *http.Request) error {
		host := chi.URLParam(r, "host")
		if err := c.ownsDomain(r.Context(), chi.URLParam(r, "slug"), host); err != nil {
			return err
		}
		return noContent(w, c.admin.VerifyDomain(operatorContext(r), host))
	})
	route(http.MethodDelete, "/stores/{slug}/domains/{host}", manage, func(w http.ResponseWriter, r *http.Request) error {
		host := chi.URLParam(r, "host")
		if err := c.ownsDomain(r.Context(), chi.URLParam(r, "slug"), host); err != nil {
			return err
		}
		return noContent(w, c.admin.RemoveDomain(operatorContext(r), host))
	})
	route(http.MethodPut, "/stores/{slug}/features/{key}", manage, func(w http.ResponseWriter, r *http.Request) error {
		var body struct {
			Enabled bool `json:"enabled"`
		}
		if err := httpx.DecodeJSON(r, &body); err != nil {
			return err
		}
		if _, err := c.detail(r.Context(), chi.URLParam(r, "slug")); err != nil {
			return err
		}
		return noContent(w, c.admin.SetFeature(operatorContext(r), chi.URLParam(r, "slug"), chi.URLParam(r, "key"), body.Enabled))
	})
}
