//go:build integration

package integration

import (
	"net/http"
	"testing"
)

func TestStoreTeamManagesItsStore(t *testing.T) {
	sh := newShop(t, 1)
	owner := sh.owner
	shopper := sh.buyer("alfa")

	t.Run("the team changes the name and search settings", func(t *testing.T) {
		current := owner.call(http.MethodGet, "/store/settings", nil)
		owner.expect(http.StatusOK, "", current)
		if current.str("currency") != "BRL" {
			t.Fatalf("settings = %v", current.body)
		}
		owner.expect(http.StatusNoContent, "", owner.call(http.MethodPut, "/store/settings", map[string]any{
			"storeName": "Alfa Renovada", "seoTitle": "Alfa", "seoDescription": "Cafés especiais", "seoIndexable": true,
			"currency": "USD",
		}))
		config := shopper.call(http.MethodGet, "/storefront/config", nil)
		store, _ := config.body["store"].(map[string]any)
		if store["name"] != "Alfa Renovada" || store["currency"] == "USD" {
			t.Errorf("storefront store = %v", store)
		}
		owner.expect(http.StatusBadRequest, "VALIDATION_FAILED", owner.call(http.MethodPut, "/store/settings", map[string]any{"storeName": ""}))
	})

	t.Run("the team publishes and rolls back the theme", func(t *testing.T) {
		theme := owner.call(http.MethodGet, "/store/theme", nil)
		owner.expect(http.StatusOK, "", theme)
		tokens, _ := theme.body["tokens"].(map[string]any)
		colors, _ := tokens["colors"].(map[string]any)
		original := colors["primary"]
		before := number(theme.body["version"])

		colors["primary"] = "#5a1a8a"
		published := owner.call(http.MethodPost, "/store/theme", map[string]any{"tokens": tokens})
		owner.expect(http.StatusCreated, "", published)
		if number(published.body["version"]) != before+1 {
			t.Errorf("published version = %v", published.body)
		}
		if got := storefrontPrimary(shopper); got != "#5a1a8a" {
			t.Errorf("storefront primary = %q", got)
		}

		colors["primary"] = "#f0f0f0"
		owner.expect(http.StatusBadRequest, "VALIDATION_FAILED", owner.call(http.MethodPost, "/store/theme", map[string]any{"tokens": tokens}))
		tokens["css"] = "body{display:none}"
		owner.expect(http.StatusBadRequest, "VALIDATION_FAILED", owner.call(http.MethodPost, "/store/theme", map[string]any{"tokens": tokens}))

		owner.expect(http.StatusNoContent, "", owner.call(http.MethodPost, "/store/theme/rollback", map[string]any{"version": before}))
		if got := storefrontPrimary(shopper); got != original {
			t.Errorf("primary after rollback = %q, want %v", got, original)
		}
		owner.expect(http.StatusNotFound, "THEME_VERSION_NOT_FOUND", owner.call(http.MethodPost, "/store/theme/rollback", map[string]any{"version": 99}))
	})

	t.Run("other stores keep their theme", func(t *testing.T) {
		if got := storefrontPrimary(sh.buyer("beta")); got == "#5a1a8a" {
			t.Error("another store received the theme")
		}
	})

	t.Run("roles without permission cannot change the store", func(t *testing.T) {
		support := sh.s.inviteStaff(t, sh.handler, "alfa", "suporte@alfa.test", "support")
		support.base = "/staff"
		support.expect(http.StatusForbidden, "PERMISSION_DENIED", support.call(http.MethodGet, "/store/settings", nil))
		support.expect(http.StatusForbidden, "PERMISSION_DENIED", support.call(http.MethodPost, "/store/theme/rollback", map[string]any{"version": 1}))
	})
}

func storefrontPrimary(client *apiClient) string {
	config := client.call(http.MethodGet, "/storefront/config", nil)
	theme, _ := config.body["theme"].(map[string]any)
	tokens, _ := theme["tokens"].(map[string]any)
	colors, _ := tokens["colors"].(map[string]any)
	primary, _ := colors["primary"].(string)
	return primary
}
