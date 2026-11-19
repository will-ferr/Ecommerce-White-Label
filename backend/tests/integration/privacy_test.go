//go:build integration

package integration

import (
	"net/http"
	"strings"
	"testing"
)

func TestCustomerDataRights(t *testing.T) {
	sh := newShop(t, 10)
	mailer := sh.mailer

	customer := customerClient(t, sh.handler, "alfa")
	customer.signUp(mailer, "lia@exemplo.com")
	customer.base = ""
	customer.headers = map[string]string{}
	customer.expect(http.StatusCreated, "", customer.call(http.MethodPost, "/account/addresses", map[string]any{
		"label": "Casa", "recipient": "Lia", "postalCode": "01310100", "street": "Avenida Paulista", "number": "10",
		"district": "Bela Vista", "city": "São Paulo", "state": "SP",
	}))
	placed := sh.placeOrder(customer, sh.variants[0], 1)
	customer.expect(http.StatusCreated, "", placed)

	t.Run("the store team lists and searches customers", func(t *testing.T) {
		found := items(sh.owner.call(http.MethodGet, "/customers?q=lia", nil), "items")
		if len(found) != 1 || found[0]["email"] != "lia@exemplo.com" {
			t.Fatalf("customers = %v", found)
		}
		id, _ := found[0]["id"].(string)
		orders := items(sh.owner.call(http.MethodGet, "/orders?customer="+id, nil), "items")
		if len(orders) != 1 {
			t.Errorf("orders of the customer = %v", orders)
		}
		support := sh.s.inviteStaff(t, sh.handler, "alfa", "suporte@alfa.test", "support")
		support.base = "/staff"
		support.expect(http.StatusOK, "", support.call(http.MethodGet, "/customers", nil))
		viewer := sh.s.inviteStaff(t, sh.handler, "alfa", "leitor@alfa.test", "viewer")
		viewer.base = "/staff"
		viewer.expect(http.StatusForbidden, "PERMISSION_DENIED", viewer.call(http.MethodGet, "/customers", nil))
		other := sh.s.inviteStaff(t, sh.handler, "beta", "dono@beta.test", "owner")
		other.base = "/staff"
		if got := items(other.call(http.MethodGet, "/customers", nil), "items"); len(got) != 0 {
			t.Errorf("another store sees %v", got)
		}
	})

	t.Run("the customer downloads their data", func(t *testing.T) {
		export := customer.call(http.MethodGet, "/account/privacy/export", nil)
		customer.expect(http.StatusOK, "", export)
		account, _ := export.body["account"].(map[string]any)
		if account["email"] != "lia@exemplo.com" || len(items(export, "addresses")) != 1 || len(items(export, "orders")) != 1 {
			t.Errorf("export = %v", export.body)
		}
		if !strings.Contains(export.header.Get("Content-Disposition"), "attachment") {
			t.Errorf("export is not a download: %v", export.header)
		}
		anonymous := sh.buyer("alfa")
		anonymous.expect(http.StatusUnauthorized, "", anonymous.call(http.MethodGet, "/account/privacy/export", nil))
	})

	t.Run("erasing requires the password", func(t *testing.T) {
		customer.expect(http.StatusBadRequest, "INVALID_PASSWORD", customer.call(http.MethodPost, "/account/privacy/erase", map[string]string{"password": "errada"}))
	})

	t.Run("the customer erases the account and the order stays as a tax record", func(t *testing.T) {
		customer.expect(http.StatusNoContent, "", customer.call(http.MethodPost, "/account/privacy/erase", map[string]string{"password": strongPassword}))

		var email, name, status string
		var hash *string
		err := sh.s.admin.QueryRow(sh.s.ctx, `SELECT email, name, status, password_hash FROM identity.users WHERE tenant_id = $1 AND realm = 'customer'`,
			sh.s.tenants["alfa"]).Scan(&email, &name, &status, &hash)
		if err != nil || strings.Contains(email, "lia") || name == "Lia" || status != "erased" || hash != nil {
			t.Errorf("user after erasure: %s %s %s %v (%v)", email, name, status, hash, err)
		}
		var addresses, linked int
		_ = sh.s.admin.QueryRow(sh.s.ctx, `SELECT count(*) FROM customers.addresses`).Scan(&addresses)
		_ = sh.s.admin.QueryRow(sh.s.ctx, `SELECT count(*) FROM orders.orders WHERE customer_user_id IS NOT NULL`).Scan(&linked)
		if addresses != 0 || linked != 0 {
			t.Errorf("addresses=%d linked orders=%d", addresses, linked)
		}
		var orderEmail string
		_ = sh.s.admin.QueryRow(sh.s.ctx, `SELECT customer_email FROM orders.orders WHERE number = $1`, number(placed.body["orderNumber"])).Scan(&orderEmail)
		if orderEmail != "maria@exemplo.com" {
			t.Errorf("order e-mail = %q, the tax record changed", orderEmail)
		}

		customer.base = "/auth"
		customer.expect(http.StatusUnauthorized, "", customer.call(http.MethodGet, "/me", nil))
		customer.token = ""
		customer.expect(http.StatusUnauthorized, "", customer.call(http.MethodPost, "/login", map[string]string{"email": "lia@exemplo.com", "password": strongPassword}))
		if got := items(sh.owner.call(http.MethodGet, "/customers?q=lia", nil), "items"); len(got) != 0 {
			t.Errorf("an erased customer is still listed: %v", got)
		}

		// The address can be used again for a new account.
		again := customerClient(t, sh.handler, "alfa")
		again.signUp(mailer, "lia@exemplo.com")
	})
}
