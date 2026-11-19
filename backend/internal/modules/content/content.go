// Package content manages the pages each store writes, such as "About us"
// and the privacy policy. Bodies use a small text format that the storefront
// renders without HTML, so a page can never carry scripts.
package content

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/modules/audit"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/modules/content/internal/sqlcgen"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/authz"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/database"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/httpx"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/tenancy"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/shared/apperr"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/shared/slug"
)

// Page statuses.
const (
	StatusDraft     = "draft"
	StatusPublished = "published"
)

const maxBodyLength = 20000

// Page is a content page.
type Page struct {
	ID             uuid.UUID `json:"id"`
	Slug           string    `json:"slug"`
	Title          string    `json:"title"`
	Body           string    `json:"body"`
	SEODescription string    `json:"seoDescription"`
	Status         string    `json:"status"`
	ShowInFooter   bool      `json:"showInFooter"`
	Version        int32     `json:"version"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

// PageInput is a page as the store team writes it.
type PageInput struct {
	Slug           string `json:"slug"`
	Title          string `json:"title"`
	Body           string `json:"body"`
	SEODescription string `json:"seoDescription"`
	Status         string `json:"status"`
	ShowInFooter   bool   `json:"showInFooter"`
	Version        int32  `json:"version"`
}

// Link points to a published page.
type Link struct {
	Slug      string     `json:"slug"`
	Title     string     `json:"title,omitempty"`
	UpdatedAt *time.Time `json:"updatedAt,omitempty"`
}

func printable(value string) bool {
	return !strings.ContainsFunc(value, func(r rune) bool { return unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' })
}

func (in PageInput) normalize() (PageInput, error) {
	in.Title = strings.Join(strings.Fields(in.Title), " ")
	in.SEODescription = strings.Join(strings.Fields(in.SEODescription), " ")
	in.Body = strings.TrimSpace(strings.ReplaceAll(in.Body, "\r\n", "\n"))
	in.Slug = strings.TrimSpace(in.Slug)
	if in.Slug == "" {
		in.Slug = slug.Make(in.Title, 80)
	}
	var fields []apperr.FieldError
	add := func(field, message string) {
		fields = append(fields, apperr.FieldError{Field: field, Code: "invalid", Message: message})
	}
	if length := utf8.RuneCountInString(in.Title); length == 0 || length > 120 {
		add("title", "Enter a title with up to 120 characters.")
	}
	if !slug.Valid(in.Slug, 80) {
		add("slug", "Use lowercase letters, numbers and hyphens.")
	}
	if utf8.RuneCountInString(in.Body) > maxBodyLength || !printable(in.Body) {
		add("body", "Use up to 20000 printable characters.")
	}
	if utf8.RuneCountInString(in.SEODescription) > 160 {
		add("seoDescription", "Use up to 160 characters.")
	}
	if in.Status != StatusDraft && in.Status != StatusPublished {
		add("status", "Choose draft or published.")
	}
	if len(fields) > 0 {
		return PageInput{}, apperr.Validation(fields...)
	}
	return in, nil
}

// Module serves content pages.
type Module struct {
	db     *database.TenantDB
	logger *slog.Logger
}

// New creates the module. The pool must use the runtime role.
func New(pool *pgxpool.Pool, logger *slog.Logger) *Module {
	return &Module{db: database.NewTenantDB(pool), logger: logger}
}

func (m *Module) in(ctx context.Context, fn func(pgx.Tx, *sqlcgen.Queries, uuid.UUID) error) error {
	tenantID, ok := tenancy.FromContext(ctx)
	if !ok {
		return tenancy.ErrMissingTenant
	}
	return m.db.Run(ctx, func(tx pgx.Tx) error { return fn(tx, sqlcgen.New(tx), tenantID) })
}

func toPage(row sqlcgen.ContentPage) Page {
	return Page{
		ID: row.ID, Slug: row.Slug, Title: row.Title, Body: row.Body, SEODescription: row.SeoDescription,
		Status: row.Status, ShowInFooter: row.ShowInFooter, Version: row.Version, UpdatedAt: row.UpdatedAt,
	}
}

func notFound() error {
	return apperr.New(apperr.KindNotFound, "PAGE_NOT_FOUND", "The page does not exist.")
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// Pages lists every page of the store, drafts included.
func (m *Module) Pages(ctx context.Context) ([]Page, error) {
	pages := []Page{}
	err := m.in(ctx, func(_ pgx.Tx, q *sqlcgen.Queries, tenantID uuid.UUID) error {
		rows, err := q.ListPages(ctx, tenantID)
		for _, row := range rows {
			pages = append(pages, toPage(row))
		}
		return err
	})
	return pages, err
}

// SavePage creates a page, or changes it when id is given. The version
// detects a page changed by someone else since it was opened.
func (m *Module) SavePage(ctx context.Context, principal authz.Principal, id *uuid.UUID, input PageInput) (uuid.UUID, error) {
	input, err := input.normalize()
	if err != nil {
		return uuid.Nil, err
	}
	pageID := uuid.Nil
	err = m.in(ctx, func(tx pgx.Tx, q *sqlcgen.Queries, tenantID uuid.UUID) error {
		var err error
		if id == nil {
			if pageID, err = uuid.NewV7(); err != nil {
				return err
			}
			err = q.InsertPage(ctx, sqlcgen.InsertPageParams{
				TenantID: tenantID, ID: pageID, Slug: input.Slug, Title: input.Title, Body: input.Body,
				SeoDescription: input.SEODescription, Status: input.Status, ShowInFooter: input.ShowInFooter,
			})
		} else {
			pageID = *id
			var affected int64
			affected, err = q.UpdatePage(ctx, sqlcgen.UpdatePageParams{
				TenantID: tenantID, ID: pageID, Slug: input.Slug, Title: input.Title, Body: input.Body,
				SeoDescription: input.SEODescription, Status: input.Status, ShowInFooter: input.ShowInFooter, Version: input.Version,
			})
			if err == nil && affected == 0 {
				if _, lookup := q.GetPage(ctx, sqlcgen.GetPageParams{TenantID: tenantID, ID: pageID}); errors.Is(lookup, pgx.ErrNoRows) {
					return notFound()
				}
				return apperr.New(apperr.KindConflict, "VERSION_CONFLICT", "Someone else changed this page. Reload it and try again.")
			}
		}
		if isUniqueViolation(err) {
			return apperr.Validation(apperr.FieldError{Field: "slug", Code: "taken", Message: "Another page already uses this address."})
		}
		if err != nil {
			return fmt.Errorf("save page: %w", err)
		}
		return audit.Record(ctx, tx, audit.Entry{
			TenantID: tenantID, ActorType: audit.ActorUser, ActorID: principal.UserID.String(),
			Action: "content.page.saved", TargetType: "page", TargetID: pageID.String(), Metadata: map[string]string{"status": input.Status},
		})
	})
	return pageID, err
}

// DeletePage removes a page.
func (m *Module) DeletePage(ctx context.Context, principal authz.Principal, id uuid.UUID) error {
	return m.in(ctx, func(tx pgx.Tx, q *sqlcgen.Queries, tenantID uuid.UUID) error {
		affected, err := q.DeletePage(ctx, sqlcgen.DeletePageParams{TenantID: tenantID, ID: id})
		if err != nil {
			return err
		}
		if affected == 0 {
			return notFound()
		}
		return audit.Record(ctx, tx, audit.Entry{
			TenantID: tenantID, ActorType: audit.ActorUser, ActorID: principal.UserID.String(),
			Action: "content.page.deleted", TargetType: "page", TargetID: id.String(),
		})
	})
}

// PublishedPage returns a page as buyers see it.
func (m *Module) PublishedPage(ctx context.Context, pageSlug string) (Page, error) {
	var page Page
	err := m.in(ctx, func(_ pgx.Tx, q *sqlcgen.Queries, tenantID uuid.UUID) error {
		row, err := q.GetPublishedPage(ctx, sqlcgen.GetPublishedPageParams{TenantID: tenantID, Slug: pageSlug})
		if errors.Is(err, pgx.ErrNoRows) {
			return notFound()
		}
		if err != nil {
			return err
		}
		page = toPage(row)
		return nil
	})
	return page, err
}

// FooterLinks lists the published pages shown in the footer of the store.
func (m *Module) FooterLinks(ctx context.Context) ([]Link, error) {
	links := []Link{}
	err := m.in(ctx, func(_ pgx.Tx, q *sqlcgen.Queries, tenantID uuid.UUID) error {
		rows, err := q.ListFooterPages(ctx, tenantID)
		for _, row := range rows {
			links = append(links, Link{Slug: row.Slug, Title: row.Title})
		}
		return err
	})
	return links, err
}

// SitemapLinks lists every published page with its last change.
func (m *Module) SitemapLinks(ctx context.Context) ([]Link, error) {
	links := []Link{}
	err := m.in(ctx, func(_ pgx.Tx, q *sqlcgen.Queries, tenantID uuid.UUID) error {
		rows, err := q.ListPublishedPages(ctx, tenantID)
		for _, row := range rows {
			links = append(links, Link{Slug: row.Slug, UpdatedAt: &row.UpdatedAt})
		}
		return err
	})
	return links, err
}

func (m *Module) route(router chi.Router, method, pattern string, policy authz.Policy, fn httpx.HandlerFunc) {
	router.Method(method, pattern, authz.Protect(m.logger, policy, httpx.Handle(m.logger, fn)))
}

// RegisterStoreRoutes adds the public pages of a store.
func (m *Module) RegisterStoreRoutes(router chi.Router) {
	m.route(router, http.MethodGet, "/content/pages", authz.Public, func(w http.ResponseWriter, r *http.Request) error {
		links, err := m.FooterLinks(r.Context())
		if err != nil {
			return err
		}
		return httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": links})
	})
	m.route(router, http.MethodGet, "/content/pages/{slug}", authz.Public, func(w http.ResponseWriter, r *http.Request) error {
		page, err := m.PublishedPage(r.Context(), chi.URLParam(r, "slug"))
		if err != nil {
			return err
		}
		return httpx.WriteJSON(w, http.StatusOK, page)
	})
}

// RegisterAdminRoutes adds page management for the store team.
func (m *Module) RegisterAdminRoutes(router chi.Router) {
	manage := authz.Permission("content:manage")
	principal := func(r *http.Request) authz.Principal {
		p, _ := authz.FromContext(r.Context())
		return p
	}
	m.route(router, http.MethodGet, "/content/pages", manage, func(w http.ResponseWriter, r *http.Request) error {
		pages, err := m.Pages(r.Context())
		if err != nil {
			return err
		}
		return httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": pages})
	})
	save := func(w http.ResponseWriter, r *http.Request, id *uuid.UUID) error {
		var body PageInput
		if err := httpx.DecodeJSON(r, &body); err != nil {
			return err
		}
		saved, err := m.SavePage(r.Context(), principal(r), id, body)
		if err != nil {
			return err
		}
		return httpx.WriteJSON(w, http.StatusOK, map[string]any{"id": saved})
	}
	pageID := func(r *http.Request) (uuid.UUID, error) {
		id, err := uuid.Parse(chi.URLParam(r, "id"))
		if err != nil {
			return uuid.Nil, notFound()
		}
		return id, nil
	}
	m.route(router, http.MethodPost, "/content/pages", manage, func(w http.ResponseWriter, r *http.Request) error {
		return save(w, r, nil)
	})
	m.route(router, http.MethodPut, "/content/pages/{id}", manage, func(w http.ResponseWriter, r *http.Request) error {
		id, err := pageID(r)
		if err != nil {
			return err
		}
		return save(w, r, &id)
	})
	m.route(router, http.MethodDelete, "/content/pages/{id}", manage, func(w http.ResponseWriter, r *http.Request) error {
		id, err := pageID(r)
		if err != nil {
			return err
		}
		if err := m.DeletePage(r.Context(), principal(r), id); err != nil {
			return err
		}
		w.WriteHeader(http.StatusNoContent)
		return nil
	})
}
