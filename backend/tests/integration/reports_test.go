//go:build integration

package integration

import (
	"fmt"
	"net/http"
	"testing"
	"time"
)

func TestSalesReport(t *testing.T) {
	sh := newShop(t, 20)
	pay := func(placed apiResponse) {
		buyer := sh.buyer("alfa")
		buyer.expect(http.StatusOK, "", buyer.call(http.MethodPost, fmt.Sprintf("/orders/%d/test-payment", number(placed.body["orderNumber"])),
			map[string]any{"token": placed.str("orderToken"), "approved": true}))
	}
	first := sh.placeOrder(sh.buyer("alfa"), sh.variants[0], 2) // 2 x 49,90 + 15,00 = 114,80
	pay(first)
	second := sh.placeOrder(sh.buyer("alfa"), sh.variants[1], 1) // 89,90 + 15,00 = 104,90
	pay(second)
	sh.placeOrder(sh.buyer("alfa"), sh.variants[0], 1) // stays pending
	refused := sh.placeOrder(sh.buyer("alfa"), sh.variants[0], 1)
	buyer := sh.buyer("alfa")
	buyer.expect(http.StatusOK, "", buyer.call(http.MethodPost, fmt.Sprintf("/orders/%d/test-payment", number(refused.body["orderNumber"])),
		map[string]any{"token": refused.str("orderToken"), "approved": false}))

	report := sh.owner.call(http.MethodGet, "/reports/sales", nil)
	sh.owner.expect(http.StatusOK, "", report)
	want := map[string]float64{"paidOrders": 2, "revenue": 11480 + 10490, "averageTicket": (11480 + 10490) / 2, "canceledOrders": 1, "allOrders": 4}
	for key, value := range want {
		if report.body[key] != value {
			t.Errorf("%s = %v, want %v", key, report.body[key], value)
		}
	}
	days := items(report, "days")
	if len(days) != 30 || report.str("timezone") != "America/Sao_Paulo" {
		t.Fatalf("days = %d, timezone = %s", len(days), report.str("timezone"))
	}
	today := time.Now().In(mustLocation(t, "America/Sao_Paulo")).Format("2006-01-02")
	if last := days[len(days)-1]; last["date"] != today || last["orders"] != float64(2) {
		t.Errorf("today = %v", last)
	}
	top := items(report, "topProducts")
	if len(top) != 1 || top[0]["name"] != "Café Especial" || top[0]["quantity"] != float64(3) {
		t.Errorf("top products = %v", top)
	}

	t.Run("a past period has no sales", func(t *testing.T) {
		old := sh.owner.call(http.MethodGet, "/reports/sales?from=2025-01-01&to=2025-01-31", nil)
		sh.owner.expect(http.StatusOK, "", old)
		if old.body["paidOrders"] != float64(0) || len(items(old, "days")) != 31 {
			t.Errorf("old period = %v", old.body)
		}
		sh.owner.expect(http.StatusBadRequest, "VALIDATION_FAILED", sh.owner.call(http.MethodGet, "/reports/sales?from=2020-01-01&to=2026-01-01", nil))
	})

	t.Run("reports are per store and need permission", func(t *testing.T) {
		other := sh.s.inviteStaff(t, sh.handler, "beta", "dono@beta.test", "owner")
		other.base = "/staff"
		if got := other.call(http.MethodGet, "/reports/sales", nil); got.body["allOrders"] != float64(0) {
			t.Errorf("another store sees %v", got.body)
		}
		support := sh.s.inviteStaff(t, sh.handler, "alfa", "suporte@alfa.test", "support")
		support.base = "/staff"
		support.expect(http.StatusForbidden, "PERMISSION_DENIED", support.call(http.MethodGet, "/reports/sales", nil))
	})
}

func mustLocation(t *testing.T, name string) *time.Location {
	t.Helper()
	location, err := time.LoadLocation(name)
	if err != nil {
		t.Fatal(err)
	}
	return location
}
