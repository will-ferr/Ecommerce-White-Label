package app

import (
	"log/slog"
	"net/http"

	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/modules/catalog"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/modules/content"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/modules/tenants"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/authz"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/httpx"
)

// sitemapHandler lists the public addresses of the store in ctx for its
// sitemap. A store that asked not to be indexed lists nothing.
func sitemapHandler(logger *slog.Logger, store *tenants.Module, products *catalog.Module, pages *content.Module) http.Handler {
	return authz.Protect(logger, authz.Public, httpx.Handle(logger, func(w http.ResponseWriter, r *http.Request) error {
		profile, err := store.Profile(r.Context())
		if err != nil {
			return err
		}
		entries := []catalog.SitemapEntry{}
		if profile.SEOIndexable {
			entries = append(entries, catalog.SitemapEntry{Path: "/"})
			catalogEntries, err := products.Service().SitemapEntries(r.Context())
			if err != nil {
				return err
			}
			entries = append(entries, catalogEntries...)
			links, err := pages.SitemapLinks(r.Context())
			if err != nil {
				return err
			}
			for _, link := range links {
				entries = append(entries, catalog.SitemapEntry{Path: "/paginas/" + link.Slug, UpdatedAt: link.UpdatedAt})
			}
		}
		return httpx.WriteJSON(w, http.StatusOK, map[string]any{"indexable": profile.SEOIndexable, "entries": entries})
	}))
}
