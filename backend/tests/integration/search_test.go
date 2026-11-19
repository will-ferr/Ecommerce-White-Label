//go:build integration

package integration

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestProductSearch(t *testing.T) {
	sh := newShop(t, 5)
	for _, product := range []map[string]any{
		{"sku": "MOEDOR", "name": "Moedor Manual de Café", "brand": "Torra Fina", "description": "Moagem ajustável em aço inoxidável."},
		{"sku": "XICARAS", "name": "Xícaras de Porcelana", "brand": "Casa", "description": "Conjunto com duas xícaras para espresso."},
	} {
		product["status"] = "active"
		product["variants"] = []map[string]any{{"sku": product["sku"], "price": 9900}}
		delete(product, "sku")
		sh.owner.expect(http.StatusCreated, "", sh.owner.call(http.MethodPost, "/catalog/products", product))
	}
	buyer := sh.buyer("alfa")

	names := func(query string) []string {
		t.Helper()
		response := buyer.call(http.MethodGet, "/catalog/products?q="+url.QueryEscape(query), nil)
		buyer.expect(http.StatusOK, "", response)
		var result []string
		for _, item := range items(response, "items") {
			name, _ := item["name"].(string)
			result = append(result, name)
		}
		return result
	}

	cases := []struct {
		name, query, want string
	}{
		{"ignores accents", "xicara", "Xícaras de Porcelana"},
		{"matches singular and plural", "moedores", "Moedor Manual de Café"},
		{"searches the brand", "torra fina", "Moedor Manual de Café"},
		{"searches the description", "inoxidavel", "Moedor Manual de Café"},
		{"tolerates a typo in the name", "porcelna", "Xícaras de Porcelana"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := names(tc.query)
			if len(got) == 0 || got[0] != tc.want {
				t.Errorf("search %q = %v, want %q first", tc.query, got, tc.want)
			}
		})
	}

	t.Run("ranks the name above the description", func(t *testing.T) {
		got := names("café")
		if len(got) < 2 || got[0] != "Café Especial" {
			t.Errorf("search café = %v", got)
		}
	})

	t.Run("unrelated words find nothing", func(t *testing.T) {
		if got := names("bicicleta"); len(got) != 0 {
			t.Errorf("search bicicleta = %v", got)
		}
	})

	t.Run("other stores are not searched", func(t *testing.T) {
		other := sh.buyer("beta")
		response := other.call(http.MethodGet, "/catalog/products?q=moedor", nil)
		other.expect(http.StatusOK, "", response)
		if got := items(response, "items"); len(got) != 0 {
			t.Errorf("another store found %v", got)
		}
	})

	t.Run("long queries are refused", func(t *testing.T) {
		buyer.expect(http.StatusBadRequest, "VALIDATION_FAILED",
			buyer.call(http.MethodGet, "/catalog/products?q="+strings.Repeat("a", 101), nil))
	})
}
