//go:build integration

package integration

import (
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"
)

func (sh *shop) coupon(body map[string]any) string {
	sh.t.Helper()
	created := sh.owner.call(http.MethodPost, "/promotions/coupons", body)
	sh.owner.expect(http.StatusOK, "", created)
	return created.str("id")
}

// buyWithCoupon fills a cart and checks out with a coupon and buyer e-mail.
func (sh *shop) buyWithCoupon(code, email string, quantity int) apiResponse {
	sh.t.Helper()
	buyer := sh.buyer("alfa")
	buyer.expect(http.StatusOK, "", buyer.call(http.MethodPost, "/cart/items", map[string]any{"variantId": sh.variants[0], "quantity": quantity}))
	summary := buyer.call(http.MethodPost, "/checkout/summary", map[string]any{"shippingMethodId": sh.shipping, "couponCode": code})
	buyer.expect(http.StatusOK, "", summary)
	body := sh.checkoutBody(number(summary.body["total"]))
	body["customerEmail"], body["couponCode"] = email, code
	buyer.headers["Idempotency-Key"] = idempotencyKey()
	return buyer.call(http.MethodPost, "/checkout/orders", body)
}

func TestCoupons(t *testing.T) {
	sh := newShop(t, 50)
	buyer := sh.buyer("alfa")
	buyer.expect(http.StatusOK, "", buyer.call(http.MethodPost, "/cart/items", map[string]any{"variantId": sh.variants[0], "quantity": 2}))
	summary := func(code string) apiResponse {
		response := buyer.call(http.MethodPost, "/checkout/summary", map[string]any{"shippingMethodId": sh.shipping, "couponCode": code})
		buyer.expect(http.StatusOK, "", response)
		return response
	}

	t.Run("the summary applies a percent coupon", func(t *testing.T) {
		sh.coupon(map[string]any{"code": "dez", "kind": "percent", "value": 10, "active": true, "description": "10% de desconto"})
		got := summary(" Dez ")
		// 2 x 49,90 = 99,80; 10% = 9,98; shipping 15,00.
		if got.body["discount"] != float64(998) || got.body["total"] != float64(9980-998+1500) {
			t.Errorf("summary = %v", got.body)
		}
		coupon, _ := got.body["coupon"].(map[string]any)
		if coupon["code"] != "DEZ" {
			t.Errorf("coupon = %v", got.body["coupon"])
		}
	})

	t.Run("problems are reported without failing the summary", func(t *testing.T) {
		yesterday := time.Now().Add(-48 * time.Hour).UTC().Format(time.RFC3339)
		ended := time.Now().Add(-24 * time.Hour).UTC().Format(time.RFC3339)
		sh.coupon(map[string]any{"code": "VENCIDO", "kind": "fixed", "value": 500, "active": true, "startsAt": yesterday, "endsAt": ended})
		sh.coupon(map[string]any{"code": "MINIMO", "kind": "fixed", "value": 500, "active": true, "minSubtotal": 20000})
		sh.coupon(map[string]any{"code": "PAUSADO", "kind": "fixed", "value": 500, "active": false})
		for code, want := range map[string]string{
			"NAOEXISTE": "COUPON_NOT_FOUND", "VENCIDO": "COUPON_EXPIRED", "MINIMO": "COUPON_MIN_SUBTOTAL", "PAUSADO": "COUPON_NOT_FOUND",
		} {
			got := summary(code)
			if got.str("couponError") != want || got.body["discount"] != float64(0) || got.body["total"] != float64(9980+1500) {
				t.Errorf("%s: %v", code, got.body)
			}
		}
	})

	t.Run("free shipping and fixed amounts", func(t *testing.T) {
		sh.coupon(map[string]any{"code": "FRETEGRATIS", "kind": "free_shipping", "value": 0, "active": true})
		sh.coupon(map[string]any{"code": "MIL", "kind": "fixed", "value": 100000, "active": true})
		if got := summary("FRETEGRATIS"); got.body["discount"] != float64(1500) || got.body["total"] != float64(9980) {
			t.Errorf("free shipping = %v", got.body)
		}
		if got := summary("MIL"); got.body["discount"] != float64(9980) || got.body["total"] != float64(1500) {
			t.Errorf("fixed above subtotal = %v", got.body)
		}
	})

	t.Run("the order records the discount and the coupon", func(t *testing.T) {
		placed := sh.buyWithCoupon("DEZ", "ana@exemplo.com", 2)
		buyer.expect(http.StatusCreated, "", placed)
		order := sh.buyer("alfa").call(http.MethodGet, fmt.Sprintf("/orders/%d?token=%s", number(placed.body["orderNumber"]), placed.str("orderToken")), nil)
		if order.body["discount"] != float64(998) || order.str("couponCode") != "DEZ" || order.body["total"] != float64(9980-998+1500) {
			t.Errorf("order = %v", order.body)
		}
		got := sh.buyWithCoupon("NAOEXISTE", "ana@exemplo.com", 1)
		if got.code != http.StatusUnprocessableEntity || got.str("code") != "COUPON_NOT_FOUND" {
			t.Errorf("unknown coupon at checkout = %d %v", got.code, got.body)
		}
	})

	t.Run("a coupon is used once per customer when limited", func(t *testing.T) {
		sh.coupon(map[string]any{"code": "PRIMEIRA", "kind": "percent", "value": 5, "active": true, "perCustomerLimit": 1})
		buyer.expect(http.StatusCreated, "", sh.buyWithCoupon("PRIMEIRA", "bia@exemplo.com", 1))
		again := sh.buyWithCoupon("PRIMEIRA", "bia@exemplo.com", 1)
		if again.code != http.StatusUnprocessableEntity || again.str("code") != "COUPON_ALREADY_USED" {
			t.Errorf("second use = %d %v", again.code, again.body)
		}
		buyer.expect(http.StatusCreated, "", sh.buyWithCoupon("PRIMEIRA", "caio@exemplo.com", 1))
	})

	t.Run("the last use of a coupon goes to one buyer", func(t *testing.T) {
		sh.coupon(map[string]any{"code": "ULTIMO", "kind": "fixed", "value": 1000, "active": true, "usageLimit": 1})
		const buyers = 10
		results := make(chan apiResponse, buyers)
		var wg sync.WaitGroup
		for i := range buyers {
			wg.Go(func() { results <- sh.buyWithCoupon("ULTIMO", fmt.Sprintf("corrida%d@exemplo.com", i), 1) })
		}
		wg.Wait()
		close(results)
		created, exhausted := 0, 0
		for result := range results {
			switch {
			case result.code == http.StatusCreated:
				created++
			case result.str("code") == "COUPON_EXHAUSTED":
				exhausted++
			default:
				t.Errorf("unexpected answer %d %v", result.code, result.body)
			}
		}
		var used int
		_ = sh.s.admin.QueryRow(sh.s.ctx, `SELECT used_count FROM promotions.coupons WHERE code = 'ULTIMO'`).Scan(&used)
		if created != 1 || exhausted != buyers-1 || used != 1 {
			t.Errorf("created=%d exhausted=%d used=%d", created, exhausted, used)
		}
	})

	t.Run("canceling an order gives the coupon back", func(t *testing.T) {
		sh.coupon(map[string]any{"code": "UNICO", "kind": "fixed", "value": 500, "active": true, "usageLimit": 1})
		placed := sh.buyWithCoupon("UNICO", "davi@exemplo.com", 1)
		buyer.expect(http.StatusCreated, "", placed)
		for _, order := range items(sh.owner.call(http.MethodGet, "/orders?status=pending_payment", nil), "items") {
			if order["number"] == placed.body["orderNumber"] {
				id, _ := order["id"].(string)
				sh.owner.expect(http.StatusNoContent, "", sh.owner.call(http.MethodPost, "/orders/"+id+"/status", map[string]string{"status": "canceled"}))
			}
		}
		buyer.expect(http.StatusCreated, "", sh.buyWithCoupon("UNICO", "eva@exemplo.com", 1))
	})

	t.Run("coupons are managed by the store team only", func(t *testing.T) {
		list := sh.owner.call(http.MethodGet, "/promotions/coupons", nil)
		if len(items(list, "items")) < 5 {
			t.Errorf("coupons = %v", list.body)
		}
		sh.owner.expect(http.StatusBadRequest, "VALIDATION_FAILED", sh.owner.call(http.MethodPost, "/promotions/coupons",
			map[string]any{"code": "DEZ", "kind": "percent", "value": 20, "active": true}))
		sh.owner.expect(http.StatusBadRequest, "VALIDATION_FAILED", sh.owner.call(http.MethodPost, "/promotions/coupons",
			map[string]any{"code": "X", "kind": "percent", "value": 500}))
		viewer := sh.s.inviteStaff(t, sh.handler, "alfa", "leitura@alfa.test", "viewer")
		viewer.base = "/staff"
		viewer.expect(http.StatusForbidden, "PERMISSION_DENIED", viewer.call(http.MethodGet, "/promotions/coupons", nil))
		other := sh.buyer("beta")
		other.expect(http.StatusOK, "", other.call(http.MethodGet, "/cart", nil))
		if got := other.call(http.MethodPost, "/checkout/summary", map[string]any{"couponCode": "DEZ"}); got.body["coupon"] != nil {
			t.Error("a coupon of one store worked in another")
		}
	})
}
