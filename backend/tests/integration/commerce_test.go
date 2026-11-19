//go:build integration

package integration

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"

	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/app"
)

type shop struct {
	s        *suite
	t        *testing.T
	handler  http.Handler
	modules  *app.Modules
	owner    *apiClient
	product  string
	variants []string
	shipping string
}

var keyCounter atomic.Int64

func idempotencyKey() string {
	return fmt.Sprintf("test-key-%016d-%s", keyCounter.Add(1), uuid.NewString()[:8])
}

func number(v any) int64 {
	value, _ := v.(float64)
	return int64(value)
}

func items(response apiResponse, key string) []map[string]any {
	raw, _ := response.body[key].([]any)
	result := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		value, _ := item.(map[string]any)
		result = append(result, value)
	}
	return result
}

// newShop prepares store "alfa" with one product, two variants, stock and a delivery option.
func newShop(t *testing.T, stock int) *shop {
	s := newSuite(t)
	modules := s.modules(&mailbox{}, nil)
	sh := &shop{s: s, t: t, modules: modules, handler: app.NewRouter(routerDeps(modules))}
	sh.owner = s.inviteStaff(t, sh.handler, "alfa", "dono@alfa.test", "owner")
	sh.owner.base = "/staff"

	category := sh.owner.call(http.MethodPost, "/catalog/categories", map[string]any{"name": "Cafés"})
	sh.owner.expect(http.StatusCreated, "", category)

	created := sh.owner.call(http.MethodPost, "/catalog/products", map[string]any{
		"name": "Café Especial", "status": "active", "categoryId": category.str("id"),
		"description": "Torra média.", "featured": true,
		"variants": []map[string]any{
			{"sku": "CAFE-250", "price": 4990, "costPrice": 2000, "options": map[string]string{"Peso": "250 g"}},
			{"sku": "CAFE-500", "price": 8990, "options": map[string]string{"Peso": "500 g"}},
		},
	})
	sh.owner.expect(http.StatusCreated, "", created)
	sh.product = created.str("id")

	detail := sh.owner.call(http.MethodGet, "/catalog/products/"+sh.product, nil)
	for _, variant := range items(detail, "variants") {
		id, _ := variant["id"].(string)
		sh.variants = append(sh.variants, id)
		sh.owner.expect(http.StatusOK, "", sh.owner.call(http.MethodPost, "/inventory/adjustments", map[string]any{
			"variantId": id, "delta": stock, "reason": "Estoque inicial",
		}))
	}
	method := sh.owner.call(http.MethodPost, "/shipping/methods", map[string]any{
		"name": "Entrega padrão", "kind": "flat", "price": 1500, "minDays": 3, "maxDays": 7, "active": true,
	})
	sh.owner.expect(http.StatusOK, "", method)
	sh.shipping = method.str("id")
	return sh
}

func (sh *shop) buyer(store string) *apiClient {
	return &apiClient{t: sh.t, handler: sh.handler, host: store + "." + baseDomain, base: "", headers: map[string]string{}}
}

func (sh *shop) checkoutBody(expected int64) map[string]any {
	return map[string]any{
		"customerName": "Maria Compradora", "customerEmail": "maria@exemplo.com",
		"shippingMethodId": sh.shipping, "expectedTotal": expected,
		"address": map[string]string{
			"recipient": "Maria Compradora", "postalCode": "01310-100", "street": "Avenida Paulista", "number": "1000",
			"district": "Bela Vista", "city": "São Paulo", "state": "SP",
		},
	}
}

// placeOrder fills a cart and checks out, returning the response.
func (sh *shop) placeOrder(buyer *apiClient, variant string, quantity int) apiResponse {
	sh.t.Helper()
	buyer.expect(http.StatusOK, "", buyer.call(http.MethodPost, "/cart/items", map[string]any{"variantId": variant, "quantity": quantity}))
	summary := buyer.call(http.MethodPost, "/checkout/summary", map[string]any{"shippingMethodId": sh.shipping})
	buyer.expect(http.StatusOK, "", summary)
	buyer.headers["Idempotency-Key"] = idempotencyKey()
	return buyer.call(http.MethodPost, "/checkout/orders", sh.checkoutBody(number(summary.body["total"])))
}

func (sh *shop) stock(variant string) (onHand, reserved int) {
	sh.t.Helper()
	err := sh.s.admin.QueryRow(sh.s.ctx, `SELECT on_hand, reserved FROM inventory.stock_levels WHERE variant_id = $1`, variant).Scan(&onHand, &reserved)
	if err != nil {
		sh.t.Fatal(err)
	}
	return onHand, reserved
}

func TestStorefrontCatalog(t *testing.T) {
	sh := newShop(t, 5)
	buyer := sh.buyer("alfa")

	list := buyer.call(http.MethodGet, "/catalog/products", nil)
	buyer.expect(http.StatusOK, "", list)
	products := items(list, "items")
	if len(products) != 1 || products[0]["price"] != float64(4990) || products[0]["hasPriceRange"] != true || products[0]["available"] != true {
		t.Fatalf("listing = %v", list.body)
	}

	detail := buyer.call(http.MethodGet, "/catalog/products/cafe-especial", nil)
	buyer.expect(http.StatusOK, "", detail)
	text := fmt.Sprint(detail.body)
	for _, private := range []string{"costPrice", "2000", "onHand", "reserved"} {
		if strings.Contains(text, private) {
			t.Errorf("the storefront exposed %q: %s", private, text)
		}
	}

	t.Run("other store does not see the product", func(t *testing.T) {
		other := sh.buyer("beta")
		other.expect(http.StatusNotFound, "PRODUCT_NOT_FOUND", other.call(http.MethodGet, "/catalog/products/cafe-especial", nil))
		if got := items(other.call(http.MethodGet, "/catalog/products", nil), "items"); len(got) != 0 {
			t.Errorf("listing of another store = %v", got)
		}
	})

	t.Run("draft products are hidden", func(t *testing.T) {
		current := sh.owner.call(http.MethodGet, "/catalog/products/"+sh.product, nil)
		body := map[string]any{"name": "Café Especial", "status": "draft", "version": current.body["version"], "variants": []map[string]any{
			{"id": sh.variants[0], "sku": "CAFE-250", "price": 4990, "options": map[string]string{"Peso": "250 g"}},
		}}
		sh.owner.expect(http.StatusNoContent, "", sh.owner.call(http.MethodPut, "/catalog/products/"+sh.product, body))
		buyer.expect(http.StatusNotFound, "PRODUCT_NOT_FOUND", buyer.call(http.MethodGet, "/catalog/products/cafe-especial", nil))

		stale := sh.owner.call(http.MethodPut, "/catalog/products/"+sh.product, body)
		sh.owner.expect(http.StatusConflict, "VERSION_CONFLICT", stale)
	})

	t.Run("search ignores wildcard characters", func(t *testing.T) {
		if got := items(buyer.call(http.MethodGet, "/catalog/products?q=%25", nil), "items"); len(got) != 0 {
			t.Errorf("a %% search matched %v", got)
		}
	})
}

func TestCheckoutAndPayment(t *testing.T) {
	sh := newShop(t, 5)
	variant := sh.variants[0]

	buyer := sh.buyer("alfa")
	placed := sh.placeOrder(buyer, variant, 2)
	buyer.expect(http.StatusCreated, "", placed)
	if placed.body["total"] != float64(2*4990+1500) || placed.str("paymentLabel") != "Pagamento de teste" {
		t.Fatalf("order = %v", placed.body)
	}
	orderNumber := fmt.Sprint(number(placed.body["orderNumber"]))
	token := placed.str("orderToken")
	if _, reserved := sh.stock(variant); reserved != 2 {
		t.Errorf("reserved = %d, want 2", reserved)
	}

	t.Run("repeating the request returns the same order", func(t *testing.T) {
		replay := buyer.call(http.MethodPost, "/checkout/orders", sh.checkoutBody(number(placed.body["total"])))
		buyer.expect(http.StatusCreated, "", replay)
		if replay.body["orderNumber"] != placed.body["orderNumber"] || replay.str("orderToken") != token {
			t.Errorf("replay = %v", replay.body)
		}
		changed := sh.checkoutBody(number(placed.body["total"]))
		changed["customerName"] = "Outra Pessoa"
		buyer.expect(http.StatusUnprocessableEntity, "IDEMPOTENCY_KEY_REUSED", buyer.call(http.MethodPost, "/checkout/orders", changed))
		if _, reserved := sh.stock(variant); reserved != 2 {
			t.Errorf("a repeated request reserved stock again: %d", reserved)
		}
	})

	t.Run("order is visible only with its link", func(t *testing.T) {
		buyer.expect(http.StatusOK, "", buyer.call(http.MethodGet, "/orders/"+orderNumber+"?token="+token, nil))
		buyer.expect(http.StatusNotFound, "ORDER_NOT_FOUND", buyer.call(http.MethodGet, "/orders/"+orderNumber+"?token=wrong", nil))
		buyer.expect(http.StatusNotFound, "ORDER_NOT_FOUND", buyer.call(http.MethodGet, "/orders/"+orderNumber, nil))
		other := sh.buyer("beta")
		other.expect(http.StatusNotFound, "ORDER_NOT_FOUND", other.call(http.MethodGet, "/orders/"+orderNumber+"?token="+token, nil))
	})

	t.Run("approved payment commits the stock once", func(t *testing.T) {
		paid := buyer.call(http.MethodPost, "/orders/"+orderNumber+"/test-payment", map[string]any{"token": token, "approved": true})
		buyer.expect(http.StatusOK, "", paid)
		if paid.str("status") != "paid" {
			t.Fatalf("order = %v", paid.body)
		}
		if onHand, reserved := sh.stock(variant); onHand != 3 || reserved != 0 {
			t.Errorf("stock = %d/%d, want 3/0", onHand, reserved)
		}
		buyer.expect(http.StatusConflict, "PAYMENT_NOT_PENDING",
			buyer.call(http.MethodPost, "/orders/"+orderNumber+"/test-payment", map[string]any{"token": token, "approved": true}))
		if onHand, _ := sh.stock(variant); onHand != 3 {
			t.Errorf("a second confirmation changed the stock: %d", onHand)
		}
	})

	t.Run("declined payment cancels and releases the stock", func(t *testing.T) {
		declined := sh.buyer("alfa")
		order := sh.placeOrder(declined, variant, 1)
		declined.expect(http.StatusCreated, "", order)
		result := declined.call(http.MethodPost, fmt.Sprintf("/orders/%d/test-payment", number(order.body["orderNumber"])),
			map[string]any{"token": order.str("orderToken"), "approved": false})
		if result.str("status") != "canceled" {
			t.Fatalf("order = %v", result.body)
		}
		if _, reserved := sh.stock(variant); reserved != 0 {
			t.Errorf("reserved = %d, want 0", reserved)
		}
	})

	t.Run("overdue orders are canceled by the expiry job", func(t *testing.T) {
		late := sh.buyer("alfa")
		order := sh.placeOrder(late, variant, 1)
		late.expect(http.StatusCreated, "", order)
		if _, err := sh.s.admin.Exec(sh.s.ctx, `UPDATE orders.orders SET payment_due_at = now() - interval '1 minute' WHERE number = $1`,
			number(order.body["orderNumber"])); err != nil {
			t.Fatal(err)
		}
		expired, err := sh.modules.Sales.Service().ExpireOverdue(sh.s.ctx)
		if err != nil || expired != 1 {
			t.Fatalf("expired=%d err=%v", expired, err)
		}
		if _, reserved := sh.stock(variant); reserved != 0 {
			t.Errorf("reserved = %d, want 0", reserved)
		}
		view := late.call(http.MethodGet, fmt.Sprintf("/orders/%d?token=%s", number(order.body["orderNumber"]), order.str("orderToken")), nil)
		if view.str("status") != "canceled" {
			t.Errorf("order = %v", view.body)
		}
	})
}

func TestCheckoutRefusesManipulation(t *testing.T) {
	sh := newShop(t, 5)
	buyer := sh.buyer("alfa")
	buyer.expect(http.StatusOK, "", buyer.call(http.MethodPost, "/cart/items", map[string]any{"variantId": sh.variants[0], "quantity": 1}))

	t.Run("prices sent by the client are refused", func(t *testing.T) {
		body := sh.checkoutBody(1)
		body["unitPrice"] = 1
		buyer.headers["Idempotency-Key"] = idempotencyKey()
		buyer.expect(http.StatusBadRequest, "INVALID_BODY", buyer.call(http.MethodPost, "/checkout/orders", body))
	})

	t.Run("a lower total than the real one is refused", func(t *testing.T) {
		buyer.headers["Idempotency-Key"] = idempotencyKey()
		response := buyer.call(http.MethodPost, "/checkout/orders", sh.checkoutBody(1))
		buyer.expect(http.StatusConflict, "PRICE_CHANGED", response)
		summary, _ := response.body["summary"].(map[string]any)
		if summary["total"] != float64(4990+1500) {
			t.Errorf("summary = %v", summary)
		}
	})

	t.Run("price change between review and payment is detected", func(t *testing.T) {
		current := sh.owner.call(http.MethodGet, "/catalog/products/"+sh.product, nil)
		sh.owner.expect(http.StatusNoContent, "", sh.owner.call(http.MethodPut, "/catalog/products/"+sh.product, map[string]any{
			"name": "Café Especial", "status": "active", "version": current.body["version"], "variants": []map[string]any{
				{"id": sh.variants[0], "sku": "CAFE-250", "price": 5990, "options": map[string]string{"Peso": "250 g"}},
				{"id": sh.variants[1], "sku": "CAFE-500", "price": 8990, "options": map[string]string{"Peso": "500 g"}},
			},
		}))
		buyer.headers["Idempotency-Key"] = idempotencyKey()
		buyer.expect(http.StatusConflict, "PRICE_CHANGED", buyer.call(http.MethodPost, "/checkout/orders", sh.checkoutBody(4990+1500)))
		buyer.headers["Idempotency-Key"] = idempotencyKey()
		buyer.expect(http.StatusCreated, "", buyer.call(http.MethodPost, "/checkout/orders", sh.checkoutBody(5990+1500)))
	})

	t.Run("quantity and identifiers are validated", func(t *testing.T) {
		fresh := sh.buyer("alfa")
		for _, quantity := range []int{0, -1, 100} {
			fresh.expect(http.StatusBadRequest, "VALIDATION_FAILED", fresh.call(http.MethodPost, "/cart/items", map[string]any{"variantId": sh.variants[0], "quantity": quantity}))
		}
		fresh.expect(http.StatusNotFound, "VARIANT_NOT_AVAILABLE", fresh.call(http.MethodPost, "/cart/items", map[string]any{"variantId": uuid.NewString(), "quantity": 1}))
	})

	t.Run("variant of another store cannot be added", func(t *testing.T) {
		other := sh.buyer("beta")
		other.expect(http.StatusNotFound, "VARIANT_NOT_AVAILABLE", other.call(http.MethodPost, "/cart/items", map[string]any{"variantId": sh.variants[0], "quantity": 1}))
	})

	t.Run("cart changes from another site are refused", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/cart/items", strings.NewReader(`{"variantId":"`+sh.variants[0]+`","quantity":1}`))
		req.Host = "alfa." + baseDomain
		req.Header.Set("Origin", "https://evil.test")
		rec := httptest.NewRecorder()
		sh.handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Errorf("status = %d", rec.Code)
		}
	})

	t.Run("checkout requires an idempotency key", func(t *testing.T) {
		delete(buyer.headers, "Idempotency-Key")
		buyer.expect(http.StatusBadRequest, "IDEMPOTENCY_KEY_REQUIRED", buyer.call(http.MethodPost, "/checkout/orders", sh.checkoutBody(1)))
	})
}

func TestLastUnitIsSoldOnlyOnce(t *testing.T) {
	sh := newShop(t, 1)
	variant := sh.variants[0]

	const buyers = 20
	carts := make([]*apiClient, buyers)
	var total int64
	for i := range carts {
		carts[i] = sh.buyer("alfa")
		carts[i].expect(http.StatusOK, "", carts[i].call(http.MethodPost, "/cart/items", map[string]any{"variantId": variant, "quantity": 1}))
		carts[i].headers["Idempotency-Key"] = idempotencyKey()
		total = number(carts[i].call(http.MethodPost, "/checkout/summary", map[string]any{"shippingMethodId": sh.shipping}).body["total"])
	}

	var wg sync.WaitGroup
	codes := make([]string, buyers)
	for i, client := range carts {
		wg.Go(func() {
			response := client.call(http.MethodPost, "/checkout/orders", sh.checkoutBody(total))
			codes[i] = fmt.Sprintf("%d:%s", response.code, response.str("code"))
		})
	}
	wg.Wait()

	sold, refused := 0, 0
	for _, code := range codes {
		switch code {
		case "201:":
			sold++
		case "409:OUT_OF_STOCK":
			refused++
		default:
			t.Errorf("unexpected answer %s", code)
		}
	}
	if sold != 1 || refused != buyers-1 {
		t.Errorf("sold=%d refused=%d", sold, refused)
	}
	if onHand, reserved := sh.stock(variant); onHand != 1 || reserved != 1 {
		t.Errorf("stock = %d/%d, want 1/1", onHand, reserved)
	}
}

func TestSimultaneousRetriesCreateOneOrder(t *testing.T) {
	sh := newShop(t, 10)
	buyer := sh.buyer("alfa")
	buyer.expect(http.StatusOK, "", buyer.call(http.MethodPost, "/cart/items", map[string]any{"variantId": sh.variants[0], "quantity": 1}))
	total := number(buyer.call(http.MethodPost, "/checkout/summary", map[string]any{"shippingMethodId": sh.shipping}).body["total"])
	key := idempotencyKey()

	var wg sync.WaitGroup
	numbers := make([]int64, 10)
	for i := range numbers {
		wg.Go(func() {
			retry := &apiClient{t: t, handler: sh.handler, host: buyer.host, cart: buyer.cart, headers: map[string]string{"Idempotency-Key": key}}
			response := retry.call(http.MethodPost, "/checkout/orders", sh.checkoutBody(total))
			if response.code == http.StatusCreated {
				numbers[i] = number(response.body["orderNumber"])
			}
		})
	}
	wg.Wait()

	var count int
	if err := sh.s.admin.QueryRow(sh.s.ctx, `SELECT count(*) FROM orders.orders`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("%d orders were created", count)
	}
	for _, n := range numbers {
		if n != 0 && n != numbers[0] && numbers[0] != 0 {
			t.Errorf("answers point to different orders: %v", numbers)
		}
	}
	if _, reserved := sh.stock(sh.variants[0]); reserved != 1 {
		t.Errorf("reserved = %d, want 1", reserved)
	}
}

func TestOrderManagement(t *testing.T) {
	sh := newShop(t, 10)
	variant := sh.variants[0]
	buyer := sh.buyer("alfa")
	placed := sh.placeOrder(buyer, variant, 2)
	buyer.expect(http.StatusCreated, "", placed)
	buyer.expect(http.StatusOK, "", buyer.call(http.MethodPost, fmt.Sprintf("/orders/%d/test-payment", number(placed.body["orderNumber"])),
		map[string]any{"token": placed.str("orderToken"), "approved": true}))

	list := sh.owner.call(http.MethodGet, "/orders?status=paid", nil)
	orders := items(list, "items")
	if len(orders) != 1 {
		t.Fatalf("orders = %v", list.body)
	}
	id, _ := orders[0]["id"].(string)
	status := func(to, tracking string) apiResponse {
		return sh.owner.call(http.MethodPost, "/orders/"+id+"/status", map[string]string{"status": to, "trackingCode": tracking})
	}

	t.Run("staff cannot mark an order as paid or skip steps", func(t *testing.T) {
		sh.owner.expect(http.StatusConflict, "INVALID_STATUS_CHANGE", status("delivered", ""))
		sh.owner.expect(http.StatusConflict, "INVALID_STATUS_CHANGE", status("paid", ""))
	})

	t.Run("viewer cannot change orders", func(t *testing.T) {
		viewer := sh.s.inviteStaff(t, sh.handler, "alfa", "leitor@alfa.test", "viewer")
		viewer.base = "/staff"
		viewer.expect(http.StatusOK, "", viewer.call(http.MethodGet, "/orders/"+id, nil))
		viewer.expect(http.StatusForbidden, "PERMISSION_DENIED", viewer.call(http.MethodPost, "/orders/"+id+"/status", map[string]string{"status": "processing"}))
	})

	t.Run("staff of another store cannot see the order", func(t *testing.T) {
		other := sh.s.inviteStaff(t, sh.handler, "beta", "dono@beta.test", "owner")
		other.base = "/staff"
		other.expect(http.StatusNotFound, "ORDER_NOT_FOUND", other.call(http.MethodGet, "/orders/"+id, nil))
		other.expect(http.StatusNotFound, "ORDER_NOT_FOUND", other.call(http.MethodPost, "/orders/"+id+"/status", map[string]string{"status": "canceled"}))
	})

	t.Run("order follows the life cycle", func(t *testing.T) {
		sh.owner.expect(http.StatusNoContent, "", status("processing", ""))
		sh.owner.expect(http.StatusNoContent, "", status("shipped", "BR123456789"))
		sh.owner.expect(http.StatusNoContent, "", sh.owner.call(http.MethodPost, "/orders/"+id+"/notes", map[string]string{"body": "Cliente pediu embrulho."}))
		detail := sh.owner.call(http.MethodGet, "/orders/"+id, nil)
		if detail.str("trackingCode") != "BR123456789" || len(items(detail, "history")) != 4 || len(items(detail, "notes")) != 1 {
			t.Errorf("detail = %v", detail.body)
		}
		sh.owner.expect(http.StatusConflict, "INVALID_STATUS_CHANGE", status("canceled", ""))
		sh.owner.expect(http.StatusNoContent, "", status("delivered", ""))
	})

	t.Run("canceling a paid order refunds and restocks", func(t *testing.T) {
		second := sh.buyer("alfa")
		order := sh.placeOrder(second, variant, 3)
		second.expect(http.StatusOK, "", second.call(http.MethodPost, fmt.Sprintf("/orders/%d/test-payment", number(order.body["orderNumber"])),
			map[string]any{"token": order.str("orderToken"), "approved": true}))
		before, _ := sh.stock(variant)

		orders := items(sh.owner.call(http.MethodGet, "/orders?status=paid", nil), "items")
		paidID, _ := orders[0]["id"].(string)
		sh.owner.expect(http.StatusNoContent, "", sh.owner.call(http.MethodPost, "/orders/"+paidID+"/status", map[string]string{"status": "canceled", "note": "Pedido do cliente"}))

		after, reserved := sh.stock(variant)
		if after != before+3 || reserved != 0 {
			t.Errorf("stock before=%d after=%d reserved=%d", before, after, reserved)
		}
		paymentStatus := func() string {
			detail := sh.owner.call(http.MethodGet, "/orders/"+paidID, nil)
			if detail.str("cancelReason") != "Pedido do cliente" {
				t.Errorf("detail = %v", detail.body)
			}
			payment, _ := detail.body["payment"].(map[string]any)
			status, _ := payment["status"].(string)
			return status
		}
		// The refund runs in the background, after the order is canceled.
		if got := paymentStatus(); got != "refund_pending" {
			t.Errorf("payment status right after canceling = %q", got)
		}
		sh.deliverEvents()
		if got := paymentStatus(); got != "refunded" {
			t.Errorf("payment status after the refund job = %q", got)
		}
	})

	t.Run("stock cannot drop below what is reserved", func(t *testing.T) {
		pending := sh.buyer("alfa")
		pending.expect(http.StatusCreated, "", sh.placeOrder(pending, sh.variants[1], 2))
		onHand, _ := sh.stock(sh.variants[1])
		sh.owner.expect(http.StatusConflict, "STOCK_BELOW_RESERVED", sh.owner.call(http.MethodPost, "/inventory/adjustments",
			map[string]any{"variantId": sh.variants[1], "delta": -onHand}))
		sh.owner.expect(http.StatusNotFound, "VARIANT_NOT_FOUND", sh.owner.call(http.MethodPost, "/inventory/adjustments",
			map[string]any{"variantId": uuid.NewString(), "delta": 5}))
	})

	t.Run("export neutralizes spreadsheet formulas", func(t *testing.T) {
		attacker := sh.buyer("alfa")
		attacker.expect(http.StatusOK, "", attacker.call(http.MethodPost, "/cart/items", map[string]any{"variantId": variant, "quantity": 1}))
		total := number(attacker.call(http.MethodPost, "/checkout/summary", map[string]any{"shippingMethodId": sh.shipping}).body["total"])
		body := sh.checkoutBody(total)
		body["customerName"] = `=HYPERLINK("https://evil.test","Clique")`
		attacker.headers["Idempotency-Key"] = idempotencyKey()
		attacker.expect(http.StatusCreated, "", attacker.call(http.MethodPost, "/checkout/orders", body))

		req := httptest.NewRequest(http.MethodGet, "/api/v1/staff/orders/export", nil)
		req.Host = adminHost
		req.Header.Set("Authorization", "Bearer "+sh.owner.token)
		rec := httptest.NewRecorder()
		sh.handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `'=HYPERLINK`) {
			t.Errorf("export = %d %s", rec.Code, rec.Body.String())
		}
	})
}

func TestCustomerAccountAndCart(t *testing.T) {
	sh := newShop(t, 10)
	mailer := &mailbox{}

	customer := sh.buyer("alfa")
	customer.base = "/auth"
	customer.signUp(mailer, "cliente@exemplo.com")
	customer.base = ""

	t.Run("visitor cart is merged after sign in", func(t *testing.T) {
		customer.token = ""
		customer.expect(http.StatusOK, "", customer.call(http.MethodPost, "/cart/items", map[string]any{"variantId": sh.variants[0], "quantity": 2}))
		customer.base = "/auth"
		customer.expect(http.StatusOK, "", customer.call(http.MethodPost, "/login", map[string]string{"email": "cliente@exemplo.com", "password": strongPassword}))
		customer.base = ""
		merged := customer.call(http.MethodPost, "/cart/merge", nil)
		customer.expect(http.StatusOK, "", merged)
		if lines := items(merged, "lines"); len(lines) != 1 || lines[0]["quantity"] != float64(2) {
			t.Errorf("cart = %v", merged.body)
		}
	})

	t.Run("customer sees only their own orders", func(t *testing.T) {
		total := number(customer.call(http.MethodPost, "/checkout/summary", map[string]any{"shippingMethodId": sh.shipping}).body["total"])
		customer.headers["Idempotency-Key"] = idempotencyKey()
		mine := customer.call(http.MethodPost, "/checkout/orders", sh.checkoutBody(total))
		customer.expect(http.StatusCreated, "", mine)

		guest := sh.buyer("alfa")
		guest.expect(http.StatusCreated, "", sh.placeOrder(guest, sh.variants[1], 1))

		orders := items(customer.call(http.MethodGet, "/account/orders", nil), "items")
		if len(orders) != 1 || orders[0]["number"] != mine.body["orderNumber"] {
			t.Errorf("orders = %v", orders)
		}
		customer.expect(http.StatusOK, "", customer.call(http.MethodGet, fmt.Sprintf("/orders/%d", number(mine.body["orderNumber"])), nil))
	})

	t.Run("address book belongs to its owner", func(t *testing.T) {
		created := customer.call(http.MethodPost, "/account/addresses", map[string]any{
			"label": "Casa", "recipient": "Cliente", "postalCode": "01310100", "street": "Rua A", "number": "1",
			"district": "Centro", "city": "São Paulo", "state": "SP",
		})
		customer.expect(http.StatusCreated, "", created)
		addresses := items(customer.call(http.MethodGet, "/account/addresses", nil), "items")
		if len(addresses) != 1 || addresses[0]["isDefault"] != true {
			t.Errorf("addresses = %v", addresses)
		}

		intruder := sh.buyer("alfa")
		intruder.base = "/auth"
		intruder.signUp(mailer, "intruso@exemplo.com")
		intruder.base = ""
		intruder.expect(http.StatusNotFound, "ADDRESS_NOT_FOUND", intruder.call(http.MethodDelete, "/account/addresses/"+created.str("id"), nil))
		customer.expect(http.StatusNoContent, "", customer.call(http.MethodDelete, "/account/addresses/"+created.str("id"), nil))
	})

	t.Run("account routes require sign in", func(t *testing.T) {
		anonymous := sh.buyer("alfa")
		anonymous.expect(http.StatusUnauthorized, "AUTHENTICATION_REQUIRED", anonymous.call(http.MethodGet, "/account/orders", nil))
	})
}
