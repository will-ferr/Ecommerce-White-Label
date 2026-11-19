package tenants

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/modules/audit"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/modules/tenants/internal/adapters/postgres/sqlcgen"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/modules/tenants/internal/domain"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/authz"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/httpx"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/tenancy"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/shared/apperr"
)

// StoreProfile is what a store team edits about its own store. Currency,
// locale, timezone, plan, status and domains belong to the platform.
type StoreProfile struct {
	StoreName      string `json:"storeName"`
	SEOTitle       string `json:"seoTitle"`
	SEODescription string `json:"seoDescription"`
	SEOIndexable   bool   `json:"seoIndexable"`
	Currency       string `json:"currency"`
	Locale         string `json:"locale"`
	Timezone       string `json:"timezone"`
}

// ThemeVersion is one published or archived theme.
type ThemeVersion struct {
	Version   int32     `json:"version"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"createdAt"`
}

// StoreTheme is the live theme of a store, its history and the allowed values.
type StoreTheme struct {
	Version  int32               `json:"version"`
	Tokens   json.RawMessage     `json:"tokens"`
	Versions []ThemeVersion      `json:"versions"`
	Choices  domain.ThemeChoices `json:"choices"`
}

func (m *Module) inStore(ctx context.Context, fn func(pgx.Tx, *sqlcgen.Queries, uuid.UUID) error) error {
	tenantID, ok := tenancy.FromContext(ctx)
	if !ok {
		return tenancy.ErrMissingTenant
	}
	return m.db.Run(ctx, func(tx pgx.Tx) error { return fn(tx, sqlcgen.New(tx), tenantID) })
}

func storeAudit(ctx context.Context, tx pgx.Tx, principal authz.Principal, action string, metadata map[string]string) error {
	return audit.Record(ctx, tx, audit.Entry{
		TenantID: principal.TenantID, ActorType: audit.ActorUser, ActorID: principal.UserID.String(),
		Action: action, TargetType: "store", TargetID: principal.TenantID.String(), Metadata: metadata,
	})
}

// Profile returns the editable settings of the store in ctx.
func (m *Module) Profile(ctx context.Context) (StoreProfile, error) {
	var profile StoreProfile
	err := m.inStore(ctx, func(_ pgx.Tx, q *sqlcgen.Queries, tenantID uuid.UUID) error {
		row, err := q.GetSettings(ctx, tenantID)
		if err != nil {
			return fmt.Errorf("load settings: %w", err)
		}
		profile = StoreProfile{
			StoreName: row.StoreName, SEOTitle: row.SeoTitle, SEODescription: row.SeoDescription, SEOIndexable: row.SeoIndexable,
			Currency: row.Currency, Locale: row.Locale, Timezone: row.Timezone,
		}
		return nil
	})
	return profile, err
}

// SaveProfile changes the name and search settings of the store.
func (m *Module) SaveProfile(ctx context.Context, principal authz.Principal, input StoreProfile) error {
	return m.inStore(ctx, func(tx pgx.Tx, q *sqlcgen.Queries, tenantID uuid.UUID) error {
		row, err := q.GetSettings(ctx, tenantID)
		if err != nil {
			return fmt.Errorf("load settings: %w", err)
		}
		settings := domain.Settings{
			StoreName: strings.TrimSpace(input.StoreName), SEOTitle: strings.TrimSpace(input.SEOTitle),
			SEODescription: strings.TrimSpace(input.SEODescription), SEOIndexable: input.SEOIndexable,
			Currency: row.Currency, Locale: row.Locale, Timezone: row.Timezone,
		}
		if err := settings.Validate(); err != nil {
			return err
		}
		if _, err := q.UpdateStoreProfile(ctx, sqlcgen.UpdateStoreProfileParams{
			TenantID: tenantID, StoreName: settings.StoreName, SeoTitle: settings.SEOTitle,
			SeoDescription: settings.SEODescription, SeoIndexable: settings.SEOIndexable,
		}); err != nil {
			return fmt.Errorf("update settings: %w", err)
		}
		return storeAudit(ctx, tx, principal, "tenants.settings.updated", map[string]string{"indexable": strconv.FormatBool(settings.SEOIndexable)})
	})
}

// Theme returns the live theme and its history.
func (m *Module) Theme(ctx context.Context) (StoreTheme, error) {
	result := StoreTheme{Versions: []ThemeVersion{}, Choices: domain.AllowedThemeChoices()}
	err := m.inStore(ctx, func(_ pgx.Tx, q *sqlcgen.Queries, tenantID uuid.UUID) error {
		live, err := q.GetPublishedTheme(ctx, tenantID)
		if err != nil {
			return fmt.Errorf("load theme: %w", err)
		}
		result.Version, result.Tokens = live.Version, live.Tokens
		versions, err := q.ListThemeVersions(ctx, tenantID)
		if err != nil {
			return fmt.Errorf("list theme versions: %w", err)
		}
		for _, version := range versions {
			result.Versions = append(result.Versions, ThemeVersion{Version: version.Version, Status: version.Status, CreatedAt: version.CreatedAt})
		}
		return nil
	})
	return result, err
}

// PublishTheme validates a theme and makes it live as a new version. Colors
// with too little contrast are refused, so a store cannot become unreadable.
func (m *Module) PublishTheme(ctx context.Context, principal authz.Principal, document []byte) (int32, error) {
	theme, err := domain.ParseTheme(document)
	if err != nil {
		return 0, err
	}
	tokens, err := json.Marshal(theme)
	if err != nil {
		return 0, err
	}
	var version int32
	err = m.inStore(ctx, func(tx pgx.Tx, q *sqlcgen.Queries, tenantID uuid.UUID) error {
		if err := q.LockStoreThemes(ctx, tenantID); err != nil {
			return fmt.Errorf("lock themes: %w", err)
		}
		if version, err = q.NextThemeVersion(ctx, tenantID); err != nil {
			return fmt.Errorf("next theme version: %w", err)
		}
		if err := q.ArchivePublishedTheme(ctx, tenantID); err != nil {
			return fmt.Errorf("archive theme: %w", err)
		}
		themeID, err := uuid.NewV7()
		if err != nil {
			return err
		}
		if err := q.InsertThemeVersion(ctx, sqlcgen.InsertThemeVersionParams{TenantID: tenantID, ID: themeID, Version: version, Tokens: tokens}); err != nil {
			return fmt.Errorf("insert theme: %w", err)
		}
		return storeAudit(ctx, tx, principal, "tenants.theme.published", map[string]string{"version": strconv.Itoa(int(version))})
	})
	return version, err
}

// RollbackTheme makes a previous version live again.
func (m *Module) RollbackTheme(ctx context.Context, principal authz.Principal, version int32) error {
	if version <= 0 {
		return apperr.New(apperr.KindInvalid, "INVALID_VERSION", "The version must be positive.")
	}
	return m.inStore(ctx, func(tx pgx.Tx, q *sqlcgen.Queries, tenantID uuid.UUID) error {
		if err := q.LockStoreThemes(ctx, tenantID); err != nil {
			return fmt.Errorf("lock themes: %w", err)
		}
		if err := q.ArchivePublishedTheme(ctx, tenantID); err != nil {
			return fmt.Errorf("archive theme: %w", err)
		}
		affected, err := q.RepublishThemeVersion(ctx, sqlcgen.RepublishThemeVersionParams{TenantID: tenantID, Version: version})
		if err != nil {
			return fmt.Errorf("republish theme: %w", err)
		}
		if affected == 0 {
			return apperr.New(apperr.KindNotFound, "THEME_VERSION_NOT_FOUND", "This theme version does not exist.")
		}
		return storeAudit(ctx, tx, principal, "tenants.theme.rolled_back", map[string]string{"version": strconv.Itoa(int(version))})
	})
}

// RegisterStaffRoutes adds the store settings for the store team.
func (m *Module) RegisterStaffRoutes(router chi.Router) {
	manage := authz.Permission("store:manage")
	route := func(method, pattern string, fn httpx.HandlerFunc) {
		router.Method(method, pattern, authz.Protect(m.logger, manage, httpx.Handle(m.logger, fn)))
	}
	principal := func(r *http.Request) authz.Principal {
		p, _ := authz.FromContext(r.Context())
		return p
	}

	route(http.MethodGet, "/store/settings", func(w http.ResponseWriter, r *http.Request) error {
		profile, err := m.Profile(r.Context())
		if err != nil {
			return err
		}
		return httpx.WriteJSON(w, http.StatusOK, profile)
	})
	route(http.MethodPut, "/store/settings", func(w http.ResponseWriter, r *http.Request) error {
		var body StoreProfile
		if err := httpx.DecodeJSON(r, &body); err != nil {
			return err
		}
		if err := m.SaveProfile(r.Context(), principal(r), body); err != nil {
			return err
		}
		w.WriteHeader(http.StatusNoContent)
		return nil
	})
	route(http.MethodGet, "/store/theme", func(w http.ResponseWriter, r *http.Request) error {
		theme, err := m.Theme(r.Context())
		if err != nil {
			return err
		}
		return httpx.WriteJSON(w, http.StatusOK, theme)
	})
	route(http.MethodPost, "/store/theme", func(w http.ResponseWriter, r *http.Request) error {
		var body struct {
			Tokens json.RawMessage `json:"tokens"`
		}
		if err := httpx.DecodeJSON(r, &body); err != nil {
			return err
		}
		version, err := m.PublishTheme(r.Context(), principal(r), body.Tokens)
		if err != nil {
			return err
		}
		return httpx.WriteJSON(w, http.StatusCreated, map[string]int32{"version": version})
	})
	route(http.MethodPost, "/store/theme/rollback", func(w http.ResponseWriter, r *http.Request) error {
		var body struct {
			Version int32 `json:"version"`
		}
		if err := httpx.DecodeJSON(r, &body); err != nil {
			return err
		}
		if err := m.RollbackTheme(r.Context(), principal(r), body.Version); err != nil {
			return err
		}
		w.WriteHeader(http.StatusNoContent)
		return nil
	})
}
