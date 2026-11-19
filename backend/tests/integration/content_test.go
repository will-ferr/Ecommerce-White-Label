//go:build integration

package integration

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestContentPagesAndSitemap(t *testing.T) {
	sh := newShop(t, 1)
	owner, shopper := sh.owner, sh.buyer("alfa")

	created := owner.call(http.MethodPost, "/content/pages", map[string]any{
		"title": "Política de Trocas", "body": "## Prazo\n\nVocê tem 7 dias.\n\n<script>alert(1)</script>",
		"status": "published", "showInFooter": true,
	})
	owner.expect(http.StatusOK, "", created)
	pageID := created.str("id")
	draft := owner.call(http.MethodPost, "/content/pages", map[string]any{"title": "Rascunho", "body": "Ainda não.", "status": "draft"})
	owner.expect(http.StatusOK, "", draft)

	t.Run("buyers read published pages only", func(t *testing.T) {
		page := shopper.call(http.MethodGet, "/content/pages/politica-de-trocas", nil)
		shopper.expect(http.StatusOK, "", page)
		// The body is stored as written; the storefront renders it as text, never as HTML.
		if page.str("title") != "Política de Trocas" || !strings.Contains(page.str("body"), "<script>") {
			t.Errorf("page = %v", page.body)
		}
		shopper.expect(http.StatusNotFound, "PAGE_NOT_FOUND", shopper.call(http.MethodGet, "/content/pages/rascunho", nil))
		footer := items(shopper.call(http.MethodGet, "/content/pages", nil), "items")
		if len(footer) != 1 || footer[0]["slug"] != "politica-de-trocas" {
			t.Errorf("footer = %v", footer)
		}
	})

	t.Run("pages belong to their store", func(t *testing.T) {
		other := sh.buyer("beta")
		other.expect(http.StatusNotFound, "PAGE_NOT_FOUND", other.call(http.MethodGet, "/content/pages/politica-de-trocas", nil))
	})

	t.Run("editing detects concurrent changes and duplicate addresses", func(t *testing.T) {
		update := map[string]any{"title": "Política de Trocas", "slug": "politica-de-trocas", "body": "## Prazo\n\n30 dias.", "status": "published", "showInFooter": true, "version": 1}
		owner.expect(http.StatusOK, "", owner.call(http.MethodPut, "/content/pages/"+pageID, update))
		owner.expect(http.StatusConflict, "VERSION_CONFLICT", owner.call(http.MethodPut, "/content/pages/"+pageID, update))
		owner.expect(http.StatusBadRequest, "VALIDATION_FAILED", owner.call(http.MethodPost, "/content/pages",
			map[string]any{"title": "Outra", "slug": "politica-de-trocas", "body": "", "status": "draft"}))
		owner.expect(http.StatusBadRequest, "VALIDATION_FAILED", owner.call(http.MethodPost, "/content/pages",
			map[string]any{"title": "Ruim", "slug": "Com Espaço", "body": "", "status": "draft"}))
	})

	t.Run("the sitemap follows the indexing setting", func(t *testing.T) {
		sitemap := shopper.call(http.MethodGet, "/storefront/sitemap", nil)
		shopper.expect(http.StatusOK, "", sitemap)
		if sitemap.body["indexable"] != false || len(items(sitemap, "entries")) != 0 {
			t.Errorf("a store not open to search engines listed %v", sitemap.body)
		}
		owner.expect(http.StatusNoContent, "", owner.call(http.MethodPut, "/store/settings", map[string]any{"storeName": "Loja Alfa", "seoIndexable": true}))
		paths := []string{}
		for _, entry := range items(shopper.call(http.MethodGet, "/storefront/sitemap", nil), "entries") {
			paths = append(paths, fmt.Sprint(entry["path"]))
		}
		joined := strings.Join(paths, " ")
		for _, want := range []string{"/", "/p/cafe-especial", "/c/cafes", "/paginas/politica-de-trocas"} {
			if !strings.Contains(" "+joined+" ", " "+want+" ") {
				t.Errorf("sitemap %v misses %s", paths, want)
			}
		}
		if strings.Contains(joined, "rascunho") {
			t.Error("the sitemap lists a draft")
		}
	})

	t.Run("deleting and permissions", func(t *testing.T) {
		support := sh.s.inviteStaff(t, sh.handler, "alfa", "atendimento@alfa.test", "support")
		support.base = "/staff"
		support.expect(http.StatusForbidden, "PERMISSION_DENIED", support.call(http.MethodGet, "/content/pages", nil))
		owner.expect(http.StatusNoContent, "", owner.call(http.MethodDelete, "/content/pages/"+draft.str("id"), nil))
		owner.expect(http.StatusNotFound, "PAGE_NOT_FOUND", owner.call(http.MethodDelete, "/content/pages/"+draft.str("id"), nil))
	})
}
