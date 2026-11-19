package sales

import (
	"strings"
	"testing"

	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/modules/sales/internal/sqlcgen"
)

func TestFormatMoney(t *testing.T) {
	for cents, want := range map[int64]string{0: "R$ 0,00", 5: "R$ 0,05", 4990: "R$ 49,90", 123456789: "R$ 1.234.567,89", -1500: "-R$ 15,00"} {
		if got := formatMoney(cents, "BRL"); got != want {
			t.Errorf("formatMoney(%d) = %q, want %q", cents, got, want)
		}
	}
	if got := formatMoney(100, "USD"); got != "USD 1,00" {
		t.Errorf("formatMoney(USD) = %q", got)
	}
}

func TestOrderMessages(t *testing.T) {
	order := sqlcgen.OrdersOrder{
		Number: 1001, CustomerName: "Maria da Silva", CustomerEmail: "maria@exemplo.com", Currency: "BRL",
		ShippingMethod: "Entrega padrão", ShippingAmount: 1500, SubtotalAmount: 9980, TotalAmount: 11480, TrackingCode: "BR123",
	}
	items := []sqlcgen.OrdersItem{{ProductName: "Café", VariantLabel: "250 g", Quantity: 2, TotalAmount: 9980}}
	link := "https://loja.test/pedido/1001?token=abc"

	created, ok := orderMessage(EventOrderCreated, "Loja", order, items, link)
	if !ok || created.To != "maria@exemplo.com" || created.Kind != "orders.created" ||
		!strings.Contains(created.Subject, "1001") || !strings.Contains(created.Body, "Olá, Maria.") ||
		!strings.Contains(created.Body, "2× Café (250 g): R$ 99,80") || !strings.Contains(created.Body, "Total: R$ 114,80") ||
		!strings.Contains(created.Body, link) {
		t.Errorf("created message = %+v", created)
	}
	shipped, _ := orderMessage(EventOrderShipped, "Loja", order, items, link)
	if !strings.Contains(shipped.Body, "Código de rastreio: BR123") {
		t.Errorf("shipped message = %q", shipped.Body)
	}
	if _, ok := orderMessage("orders.order.delivered.v1", "Loja", order, items, link); ok {
		t.Error("an event without a message produced one")
	}
}
