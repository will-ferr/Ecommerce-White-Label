package domain

import (
	"math"
	"slices"
	"testing"
)

func TestTransitions(t *testing.T) {
	allowed := [][2]string{
		{PendingPayment, Paid}, {PendingPayment, Canceled}, {Paid, Processing}, {Processing, Shipped},
		{Shipped, Delivered}, {Shipped, DeliveryFailed}, {DeliveryFailed, Shipped}, {Delivered, Completed},
		{Paid, Canceled}, {Processing, Canceled}, {DeliveryFailed, Canceled},
	}
	for _, pair := range allowed {
		if !CanTransition(pair[0], pair[1]) {
			t.Errorf("%s -> %s must be allowed", pair[0], pair[1])
		}
	}
	refused := [][2]string{
		{PendingPayment, Shipped}, {Canceled, Paid}, {Completed, Canceled}, {Delivered, Canceled},
		{Shipped, Canceled}, {Paid, PendingPayment}, {Canceled, Canceled}, {"unknown", Paid},
	}
	for _, pair := range refused {
		if CanTransition(pair[0], pair[1]) {
			t.Errorf("%s -> %s must be refused", pair[0], pair[1])
		}
	}
}

func TestStaffCannotMarkOrdersAsPaid(t *testing.T) {
	if StaffCanSet(PendingPayment, Paid) {
		t.Error("only a confirmed payment may mark an order as paid")
	}
	if !slices.Equal(Next(PendingPayment), []string{Canceled}) {
		t.Errorf("Next(pending) = %v", Next(PendingPayment))
	}
	if len(Next(Completed)) != 0 || len(Next(Canceled)) != 0 {
		t.Error("final statuses have no next step")
	}
}

func TestRefundRule(t *testing.T) {
	for status, want := range map[string]bool{PendingPayment: false, Paid: true, Processing: true, DeliveryFailed: true} {
		if NeedsRefund(status) != want {
			t.Errorf("NeedsRefund(%s) != %v", status, want)
		}
	}
}

func TestTotals(t *testing.T) {
	subtotal, total, err := Totals([]Line{{Quantity: 2, UnitPrice: 4990}, {Quantity: 1, UnitPrice: 1000}}, 1500)
	if err != nil || subtotal != 10980 || total != 12480 {
		t.Errorf("subtotal=%d total=%d err=%v", subtotal, total, err)
	}
}

func TestTotalsRefusesInvalidOrOverflowingLines(t *testing.T) {
	cases := map[string][]Line{
		"zero quantity":     {{Quantity: 0, UnitPrice: 100}},
		"negative quantity": {{Quantity: -1, UnitPrice: 100}},
		"negative price":    {{Quantity: 1, UnitPrice: -100}},
		"overflow":          {{Quantity: 99, UnitPrice: math.MaxInt64 / 50}},
	}
	for name, lines := range cases {
		t.Run(name, func(t *testing.T) {
			if _, _, err := Totals(lines, 0); err == nil {
				t.Error("invalid lines were accepted")
			}
		})
	}
	if _, _, err := Totals([]Line{{Quantity: 1, UnitPrice: 1}}, -5); err == nil {
		t.Error("negative shipping was accepted")
	}
}
