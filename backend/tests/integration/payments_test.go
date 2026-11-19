//go:build integration

package integration

import (
	"bytes"
	"context"
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/app"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/config"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/mail"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/messaging"
)

// deliverEvents hands every unpublished outbox event to the consumers that
// subscribe to it, as the worker does through RabbitMQ, until none is left.
func (sh *shop) deliverEvents() {
	sh.t.Helper()
	consumers := map[string][]*messaging.Consumer{}
	for _, subscription := range sh.modules.Subscriptions {
		consumer := messaging.NewConsumer(sh.s.runtime, nil, quietLogger(), subscription, nil)
		for _, eventType := range subscription.Events {
			consumers[eventType] = append(consumers[eventType], consumer)
		}
	}
	for range 10 {
		rows, err := sh.s.admin.Query(sh.s.ctx, `
			UPDATE messaging.outbox SET published_at = now() WHERE published_at IS NULL
			RETURNING json_build_object('specversion', '1.0', 'id', id, 'source', source, 'type', type, 'time', created_at,
			          'tenantid', tenant_id, 'aggregateid', aggregate_id, 'datacontenttype', 'application/json', 'data', payload)::text`)
		if err != nil {
			sh.t.Fatal(err)
		}
		bodies, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			sh.t.Fatal(err)
		}
		if len(bodies) == 0 {
			return
		}
		for _, body := range bodies {
			var event messaging.Event
			if err := json.Unmarshal([]byte(body), &event); err != nil {
				sh.t.Fatal(err)
			}
			for _, consumer := range consumers[event.Type] {
				outcome, err := consumer.Process(sh.s.ctx, []byte(body), 1)
				if err != nil || (outcome.Status != "handled" && outcome.Status != "duplicate") {
					sh.t.Fatalf("event %s: %+v, %v", event.Type, outcome, err)
				}
			}
		}
	}
	sh.t.Fatal("events kept producing events")
}

func (sh *shop) eventCount(eventType string) int {
	sh.t.Helper()
	var count int
	if err := sh.s.admin.QueryRow(sh.s.ctx, `SELECT count(*) FROM messaging.outbox WHERE type = $1`, eventType).Scan(&count); err != nil {
		sh.t.Fatal(err)
	}
	return count
}

// paymentOf returns the provider reference and status of the payment of an order.
func (sh *shop) paymentOf(orderNumber int64) (reference, status string) {
	sh.t.Helper()
	err := sh.s.admin.QueryRow(sh.s.ctx, `
		SELECT p.provider_reference, p.status FROM payments.payments p
		JOIN orders.orders o ON o.tenant_id = p.tenant_id AND o.id = p.order_id
		WHERE o.tenant_id = $1 AND o.number = $2`, sh.s.tenants["alfa"], orderNumber).Scan(&reference, &status)
	if err != nil {
		sh.t.Fatal(err)
	}
	return reference, status
}

func (sh *shop) orderStatus(orderNumber int64) string {
	sh.t.Helper()
	var status string
	if err := sh.s.admin.QueryRow(sh.s.ctx, `SELECT status FROM orders.orders WHERE tenant_id = $1 AND number = $2`,
		sh.s.tenants["alfa"], orderNumber).Scan(&status); err != nil {
		sh.t.Fatal(err)
	}
	return status
}

// notification signs a test provider notification the way the provider does.
type notification struct {
	ID        string `json:"id"`
	Reference string `json:"reference"`
	Status    string `json:"status"`
	Amount    int64  `json:"amount"`
}

func signNotification(t *testing.T, n notification, at time.Time) (http.Header, []byte) {
	t.Helper()
	key, err := hkdf.Key(sha256.New, testAuthConfig().SigningSeed, nil, "test-payments-v1", 32)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(n)
	timestamp := strconv.FormatInt(at.Unix(), 10)
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(timestamp + "."))
	mac.Write(body)
	header := http.Header{}
	header.Set("Test-Signature", "t="+timestamp+",v1="+hex.EncodeToString(mac.Sum(nil)))
	return header, body
}

func (sh *shop) postWebhook(store, provider string, header http.Header, body []byte) apiResponse {
	sh.t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/payments/"+provider, bytes.NewReader(body))
	req.Host = store + "." + baseDomain
	for name, values := range header {
		req.Header[name] = values
	}
	rec := httptest.NewRecorder()
	sh.handler.ServeHTTP(rec, req)
	response := apiResponse{code: rec.Code, body: map[string]any{}}
	_ = json.Unmarshal(rec.Body.Bytes(), &response.body)
	return response
}

func (sh *shop) expectOutcome(response apiResponse, want string) {
	sh.t.Helper()
	if response.code != http.StatusOK || response.str("outcome") != want {
		sh.t.Fatalf("webhook = %d %v, want outcome %s", response.code, response.body, want)
	}
}

func TestPaymentWebhooks(t *testing.T) {
	sh := newShop(t, 20)
	variant := sh.variants[0]
	pending := func(quantity int) (int64, string, int64) {
		buyer := sh.buyer("alfa")
		placed := sh.placeOrder(buyer, variant, quantity)
		buyer.expect(http.StatusCreated, "", placed)
		orderNumber := number(placed.body["orderNumber"])
		reference, status := sh.paymentOf(orderNumber)
		if reference == "" || status != "pending" {
			t.Fatalf("payment = %q %q", reference, status)
		}
		return orderNumber, reference, number(placed.body["total"])
	}

	t.Run("a signed capture pays the order once", func(t *testing.T) {
		orderNumber, reference, total := pending(2)
		onHandBefore, _ := sh.stock(variant)
		header, body := signNotification(t, notification{ID: "evt_" + uuid.NewString(), Reference: reference, Status: "captured", Amount: total}, time.Now())

		sh.expectOutcome(sh.postWebhook("alfa", "test", header, body), "captured")
		if status := sh.orderStatus(orderNumber); status != "paid" {
			t.Fatalf("order status = %s", status)
		}
		paidEvents := sh.eventCount("orders.order.paid.v1")

		// The gateway delivers the same notification again.
		sh.expectOutcome(sh.postWebhook("alfa", "test", header, body), "duplicate")
		if got := sh.eventCount("orders.order.paid.v1"); got != paidEvents {
			t.Errorf("a duplicate published %d paid events", got-paidEvents)
		}
		onHand, reserved := sh.stock(variant)
		if onHand != onHandBefore-2 || reserved != 0 {
			t.Errorf("stock on hand=%d reserved=%d, want %d and 0", onHand, reserved, onHandBefore-2)
		}
		var captures int
		_ = sh.s.admin.QueryRow(sh.s.ctx, `SELECT count(*) FROM payments.transactions WHERE kind = 'capture' AND provider_reference = $1`, reference).Scan(&captures)
		if captures != 1 {
			t.Errorf("captures = %d, want 1", captures)
		}
	})

	t.Run("simultaneous duplicates have one effect", func(t *testing.T) {
		orderNumber, reference, total := pending(1)
		header, body := signNotification(t, notification{ID: "evt_" + uuid.NewString(), Reference: reference, Status: "captured", Amount: total}, time.Now())
		var wg sync.WaitGroup
		outcomes := make(chan string, 8)
		for range 8 {
			wg.Go(func() { outcomes <- sh.postWebhook("alfa", "test", header, body).str("outcome") })
		}
		wg.Wait()
		close(outcomes)
		counts := map[string]int{}
		for outcome := range outcomes {
			counts[outcome]++
		}
		if counts["captured"] != 1 || counts["duplicate"] != 7 {
			t.Errorf("outcomes = %v", counts)
		}
		if status := sh.orderStatus(orderNumber); status != "paid" {
			t.Errorf("order status = %s", status)
		}
	})

	t.Run("forged or stale notifications are refused", func(t *testing.T) {
		orderNumber, reference, total := pending(1)
		header, body := signNotification(t, notification{ID: "evt_forged", Reference: reference, Status: "captured", Amount: total}, time.Now())
		tampered := bytes.Replace(body, []byte(`"evt_forged"`), []byte(`"evt_other"`), 1)
		for name, response := range map[string]apiResponse{
			"changed body":      sh.postWebhook("alfa", "test", header, tampered),
			"missing signature": sh.postWebhook("alfa", "test", http.Header{}, body),
			"old signature": func() apiResponse {
				h, b := signNotification(t, notification{ID: "evt_old", Reference: reference, Status: "captured", Amount: total}, time.Now().Add(-10*time.Minute))
				return sh.postWebhook("alfa", "test", h, b)
			}(),
		} {
			if response.code != http.StatusUnauthorized || response.str("code") != "WEBHOOK_SIGNATURE_INVALID" {
				t.Errorf("%s: %d %v", name, response.code, response.body)
			}
		}
		if response := sh.postWebhook("alfa", "unknown-gateway", header, body); response.code != http.StatusNotFound {
			t.Errorf("unknown provider = %d", response.code)
		}
		var stored int
		_ = sh.s.admin.QueryRow(sh.s.ctx, `SELECT count(*) FROM payments.webhook_events WHERE provider_event_id IN ('evt_forged', 'evt_other', 'evt_old')`).Scan(&stored)
		if stored != 0 || sh.orderStatus(orderNumber) != "pending_payment" {
			t.Errorf("a refused notification had an effect: stored=%d status=%s", stored, sh.orderStatus(orderNumber))
		}
	})

	t.Run("a notification sent to another store does not reach the order", func(t *testing.T) {
		orderNumber, reference, total := pending(1)
		header, body := signNotification(t, notification{ID: "evt_" + uuid.NewString(), Reference: reference, Status: "captured", Amount: total}, time.Now())
		sh.expectOutcome(sh.postWebhook("beta", "test", header, body), "unknown_payment")
		if status := sh.orderStatus(orderNumber); status != "pending_payment" {
			t.Errorf("order status = %s", status)
		}
	})

	t.Run("a capture with another amount is not accepted", func(t *testing.T) {
		orderNumber, reference, total := pending(1)
		header, body := signNotification(t, notification{ID: "evt_" + uuid.NewString(), Reference: reference, Status: "captured", Amount: total - 1}, time.Now())
		sh.expectOutcome(sh.postWebhook("alfa", "test", header, body), "amount_mismatch")
		if status := sh.orderStatus(orderNumber); status != "pending_payment" {
			t.Errorf("order status = %s", status)
		}
	})

	t.Run("a refused payment cancels the order and releases stock", func(t *testing.T) {
		_, reservedBefore := sh.stock(variant)
		orderNumber, reference, total := pending(2)
		header, body := signNotification(t, notification{ID: "evt_" + uuid.NewString(), Reference: reference, Status: "failed", Amount: total}, time.Now())
		sh.expectOutcome(sh.postWebhook("alfa", "test", header, body), "failed")
		if _, reserved := sh.stock(variant); sh.orderStatus(orderNumber) != "canceled" || reserved != reservedBefore {
			t.Errorf("status=%s reserved=%d, want canceled and %d", sh.orderStatus(orderNumber), reserved, reservedBefore)
		}
	})

	t.Run("money received after cancellation is refunded", func(t *testing.T) {
		orderNumber, reference, total := pending(1)
		orders := items(sh.owner.call(http.MethodGet, "/orders?status=pending_payment", nil), "items")
		var id string
		for _, order := range orders {
			if number(order["number"]) == orderNumber {
				id, _ = order["id"].(string)
			}
		}
		sh.owner.expect(http.StatusNoContent, "", sh.owner.call(http.MethodPost, "/orders/"+id+"/status", map[string]string{"status": "canceled"}))

		header, body := signNotification(t, notification{ID: "evt_" + uuid.NewString(), Reference: reference, Status: "captured", Amount: total}, time.Now())
		sh.expectOutcome(sh.postWebhook("alfa", "test", header, body), "late_capture_refund_requested")
		if _, status := sh.paymentOf(orderNumber); status != "refund_pending" {
			t.Fatalf("payment status = %s", status)
		}
		sh.deliverEvents()
		if _, status := sh.paymentOf(orderNumber); status != "refunded" || sh.orderStatus(orderNumber) != "canceled" {
			t.Errorf("payment=%s order=%s", status, sh.orderStatus(orderNumber))
		}
	})

	t.Run("a refund made at the provider cancels and restocks the order", func(t *testing.T) {
		orderNumber, reference, total := pending(3)
		header, body := signNotification(t, notification{ID: "evt_" + uuid.NewString(), Reference: reference, Status: "captured", Amount: total}, time.Now())
		sh.expectOutcome(sh.postWebhook("alfa", "test", header, body), "captured")
		onHand, _ := sh.stock(variant)
		header, body = signNotification(t, notification{ID: "evt_" + uuid.NewString(), Reference: reference, Status: "refunded", Amount: total}, time.Now())
		sh.expectOutcome(sh.postWebhook("alfa", "test", header, body), "refunded")
		after, _ := sh.stock(variant)
		if _, status := sh.paymentOf(orderNumber); status != "refunded" || sh.orderStatus(orderNumber) != "canceled" || after != onHand+3 {
			t.Errorf("payment=%s order=%s stock %d -> %d", status, sh.orderStatus(orderNumber), onHand, after)
		}
	})
}

func TestCheckoutWithoutPaymentProvider(t *testing.T) {
	s := newSuite(t)
	modules, err := app.BuildModules(app.ModuleDeps{
		Pool: s.runtime, Logger: quietLogger(), Mailer: &mailbox{}, Auth: testAuthConfig(),
		Commerce: config.Commerce{PaymentTTL: 30 * time.Minute},
	})
	if err != nil {
		t.Fatal(err)
	}
	handler := app.NewRouter(routerDeps(modules))
	buyer := &apiClient{t: t, handler: handler, host: "alfa." + baseDomain, headers: map[string]string{"Idempotency-Key": idempotencyKey()}}
	buyer.expect(http.StatusServiceUnavailable, "PAYMENT_UNAVAILABLE", buyer.call(http.MethodPost, "/checkout/orders", map[string]any{}))
	if response := buyer.call(http.MethodPost, "/orders/1/test-payment", map[string]any{}); response.code != http.StatusNotFound {
		t.Errorf("test payment route without the test provider = %d", response.code)
	}
}

// storedMail decrypts the e-mails waiting in the outbox of a store.
func (sh *shop) storedMail(slug string) []mail.Message {
	sh.t.Helper()
	return sh.storedMailByID(sh.s.tenants[slug].String())
}

func (sh *shop) storedMailByID(tenantID string) []mail.Message {
	sh.t.Helper()
	sealer, err := mail.NewSealer(testAuthConfig().MFAKey)
	if err != nil {
		sh.t.Fatal(err)
	}
	rows, err := sh.s.admin.Query(sh.s.ctx, `SELECT id, kind, sealed FROM notifications.emails WHERE tenant_id = $1 AND status = 'pending' ORDER BY id`, tenantID)
	if err != nil {
		sh.t.Fatal(err)
	}
	defer rows.Close()
	var messages []mail.Message
	for rows.Next() {
		var id uuid.UUID
		var kind string
		var sealed []byte
		if err := rows.Scan(&id, &kind, &sealed); err != nil {
			sh.t.Fatal(err)
		}
		plain, err := sealer.Open(sealed, []byte(tenantID+"|"+id.String()))
		if err != nil {
			sh.t.Fatal(err)
		}
		var message mail.Message
		_ = json.Unmarshal(plain, &message)
		message.Kind = kind
		messages = append(messages, message)
	}
	return messages
}

type fakeSender struct {
	mu   sync.Mutex
	sent []mail.Message
	fail error
}

func (f *fakeSender) Send(_ context.Context, message mail.Message) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return f.fail
	}
	f.sent = append(f.sent, message)
	return nil
}

func TestOrderEmails(t *testing.T) {
	sh := newShop(t, 10)
	buyer := sh.buyer("alfa")
	placed := sh.placeOrder(buyer, sh.variants[0], 1)
	buyer.expect(http.StatusCreated, "", placed)
	orderNumber := number(placed.body["orderNumber"])
	buyer.expect(http.StatusOK, "", buyer.call(http.MethodPost, fmt.Sprintf("/orders/%d/test-payment", orderNumber),
		map[string]any{"token": placed.str("orderToken"), "approved": true}))
	sh.deliverEvents()

	messages := sh.storedMail("alfa")
	kinds := []string{}
	for _, message := range messages {
		kinds = append(kinds, message.Kind)
		if message.To != "maria@exemplo.com" {
			t.Errorf("message to %q", message.To)
		}
	}
	if strings.Join(kinds, ",") != "orders.created,orders.paid" {
		t.Fatalf("kinds = %v", kinds)
	}
	link := fmt.Sprintf("https://alfa.%s/pedido/%d?token=%s", baseDomain, orderNumber, placed.str("orderToken"))
	if !strings.Contains(messages[0].Body, link) || !strings.Contains(messages[0].Subject, "Loja Alfa") {
		t.Errorf("confirmation = %+v", messages[0])
	}

	t.Run("replayed events send nothing new", func(t *testing.T) {
		if _, err := sh.s.admin.Exec(sh.s.ctx, `UPDATE messaging.outbox SET published_at = NULL`); err != nil {
			t.Fatal(err)
		}
		sh.deliverEvents()
		if got := len(sh.storedMail("alfa")); got != 2 {
			t.Errorf("messages after replay = %d, want 2", got)
		}
	})

	t.Run("other stores keep their messages apart", func(t *testing.T) {
		if got := len(sh.storedMail("beta")); got != 0 {
			t.Errorf("store beta has %d messages", got)
		}
	})

	t.Run("the dispatcher delivers, retries and erases messages", func(t *testing.T) {
		sealer, _ := mail.NewSealer(testAuthConfig().MFAKey)
		failing := &fakeSender{fail: errors.New("550 mailbox <maria@exemplo.com> unavailable")}
		dispatcher := mail.NewDispatcher(sh.s.runtime, failing, sealer, quietLogger())
		if count, err := dispatcher.DispatchBatch(sh.s.ctx); err != nil || count != 2 {
			t.Fatalf("DispatchBatch() = %d, %v", count, err)
		}
		var attempts int
		var lastError string
		err := sh.s.admin.QueryRow(sh.s.ctx, `SELECT max(attempts), max(last_error) FROM notifications.emails WHERE status = 'pending'`).Scan(&attempts, &lastError)
		if err != nil || attempts != 1 || strings.Contains(lastError, "maria@") {
			t.Errorf("after failure: attempts=%d error=%q (%v)", attempts, lastError, err)
		}
		// Messages wait before the next attempt.
		if count, _ := dispatcher.DispatchBatch(sh.s.ctx); count != 0 {
			t.Errorf("a failed message was retried immediately")
		}

		if _, err := sh.s.admin.Exec(sh.s.ctx, `UPDATE notifications.emails SET next_attempt_at = now()`); err != nil {
			t.Fatal(err)
		}
		working := &fakeSender{}
		if count, err := mail.NewDispatcher(sh.s.runtime, working, sealer, quietLogger()).DispatchBatch(sh.s.ctx); err != nil || count != 2 {
			t.Fatalf("DispatchBatch() = %d, %v", count, err)
		}
		if len(working.sent) != 2 {
			t.Fatalf("sent = %d", len(working.sent))
		}
		var remaining int
		_ = sh.s.admin.QueryRow(sh.s.ctx, `SELECT count(*) FROM notifications.emails WHERE sealed IS NOT NULL OR status <> 'sent'`).Scan(&remaining)
		if remaining != 0 {
			t.Errorf("%d messages kept their content after delivery", remaining)
		}
	})

	t.Run("the runtime role cannot read stored messages", func(t *testing.T) {
		if _, err := sh.s.runtime.Exec(sh.s.ctx, `SELECT sealed FROM notifications.emails`); err == nil {
			t.Error("the runtime role read the e-mail outbox")
		}
	})
}

func TestShippingTable(t *testing.T) {
	sh := newShop(t, 10)
	rates := []map[string]any{
		{"postalCodeStart": "01000000", "postalCodeEnd": "19999999", "maxWeightGrams": 1000, "price": 1200, "minDays": 1, "maxDays": 2},
		{"postalCodeStart": "01000000", "postalCodeEnd": "19999999", "maxWeightGrams": 10000, "price": 2400, "minDays": 1, "maxDays": 3},
		{"postalCodeStart": "20000000", "postalCodeEnd": "99999999", "maxWeightGrams": 10000, "price": 3900, "minDays": 4, "maxDays": 9},
	}
	created := sh.owner.call(http.MethodPost, "/shipping/methods", map[string]any{"name": "Transportadora", "kind": "table", "active": true, "rates": rates})
	sh.owner.expect(http.StatusOK, "", created)
	method := created.str("id")

	product := sh.owner.call(http.MethodPost, "/catalog/products", map[string]any{
		"name": "Moedor Elétrico", "status": "active",
		"variants": []map[string]any{{"sku": "MOEDOR-E", "price": 30000, "weightGrams": 800}},
	})
	sh.owner.expect(http.StatusCreated, "", product)
	variant, _ := items(sh.owner.call(http.MethodGet, "/catalog/products/"+product.str("id"), nil), "variants")[0]["id"].(string)
	sh.owner.expect(http.StatusOK, "", sh.owner.call(http.MethodPost, "/inventory/adjustments", map[string]any{"variantId": variant, "delta": 5, "reason": "Inicial"}))

	buyer := sh.buyer("alfa")
	buyer.expect(http.StatusOK, "", buyer.call(http.MethodPost, "/cart/items", map[string]any{"variantId": variant, "quantity": 1}))
	option := func(postalCode string) map[string]any {
		summary := buyer.call(http.MethodPost, "/checkout/summary", map[string]any{"postalCode": postalCode})
		buyer.expect(http.StatusOK, "", summary)
		for _, item := range items(summary, "shippingOptions") {
			if item["id"] == method {
				return item
			}
		}
		return nil
	}

	if option("") != nil {
		t.Error("a table option was offered before the postal code")
	}
	if got := option("01310-100"); got == nil || got["price"] != float64(1200) || got["maxDays"] != float64(2) {
		t.Errorf("light parcel in São Paulo = %v", got)
	}
	if got := option("69900-000"); got == nil || got["price"] != float64(3900) {
		t.Errorf("parcel to Acre = %v", got)
	}
	buyer.expect(http.StatusOK, "", buyer.call(http.MethodPut, "/cart/items/"+variant, map[string]any{"quantity": 2}))
	if got := option("01310-100"); got == nil || got["price"] != float64(2400) {
		t.Errorf("1.6 kg parcel in São Paulo = %v", got)
	}

	t.Run("the order uses the rate of the delivery address", func(t *testing.T) {
		summary := buyer.call(http.MethodPost, "/checkout/summary", map[string]any{"postalCode": "01310100", "shippingMethodId": method})
		body := sh.checkoutBody(number(summary.body["total"]))
		body["shippingMethodId"] = method
		buyer.headers["Idempotency-Key"] = idempotencyKey()
		placed := buyer.call(http.MethodPost, "/checkout/orders", body)
		buyer.expect(http.StatusCreated, "", placed)
		if placed.body["total"] != float64(2*30000+2400) {
			t.Errorf("total = %v", placed.body["total"])
		}
	})

	t.Run("invalid tables are refused", func(t *testing.T) {
		bad := []map[string]any{{"postalCodeStart": "99999999", "postalCodeEnd": "01000000", "maxWeightGrams": 0, "price": 100, "minDays": 1, "maxDays": 2}}
		response := sh.owner.call(http.MethodPost, "/shipping/methods", map[string]any{"name": "Errada", "kind": "table", "active": true, "rates": bad})
		sh.owner.expect(http.StatusBadRequest, "VALIDATION_FAILED", response)
		sh.owner.expect(http.StatusBadRequest, "VALIDATION_FAILED",
			sh.owner.call(http.MethodPost, "/shipping/methods", map[string]any{"name": "Vazia", "kind": "table", "active": true}))
	})

	t.Run("the store team sees the rates", func(t *testing.T) {
		for _, item := range items(sh.owner.call(http.MethodGet, "/shipping/methods", nil), "items") {
			if item["id"] == method {
				if got, _ := item["rates"].([]any); len(got) != 3 {
					t.Errorf("rates = %v", item["rates"])
				}
				return
			}
		}
		t.Error("the table method is missing")
	})
}

func TestAccountEmailsAreStoredDurably(t *testing.T) {
	s := newSuite(t)
	modules, err := app.BuildModules(app.ModuleDeps{Pool: s.runtime, Logger: quietLogger(), Auth: testAuthConfig()})
	if err != nil {
		t.Fatal(err)
	}
	customer := customerClient(t, app.NewRouter(routerDeps(modules)), "gama")
	customer.expect(http.StatusAccepted, "", customer.call(http.MethodPost, "/register", map[string]string{
		"name": "Maria", "email": "maria@gama.test", "password": strongPassword,
	}))
	sh := &shop{s: s, t: t}
	messages := sh.storedMail("gama")
	if len(messages) != 1 || messages[0].Kind != "identity.email_verification" || messages[0].To != "maria@gama.test" ||
		!strings.Contains(messages[0].Body, "/verificar-email?token=") {
		t.Fatalf("stored messages = %+v", messages)
	}
	var plain int
	_ = s.admin.QueryRow(s.ctx, `SELECT count(*) FROM notifications.emails WHERE position('maria@gama.test' in convert_from(sealed, 'LATIN1')) > 0`).Scan(&plain)
	if plain != 0 {
		t.Error("the stored message is readable without the key")
	}
}
