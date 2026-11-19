// Package domain holds the sales rules: the order life cycle and totals.
package domain

import (
	"math"
	"slices"

	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/shared/apperr"
)

// Order statuses.
const (
	PendingPayment = "pending_payment"
	Paid           = "paid"
	Processing     = "processing"
	Shipped        = "shipped"
	DeliveryFailed = "delivery_failed"
	Delivered      = "delivered"
	Completed      = "completed"
	Canceled       = "canceled"
)

// transitions is the only place that defines how an order may change.
var transitions = map[string][]string{
	PendingPayment: {Paid, Canceled},
	Paid:           {Processing, Canceled},
	Processing:     {Shipped, Canceled},
	Shipped:        {Delivered, DeliveryFailed},
	DeliveryFailed: {Shipped, Canceled},
	Delivered:      {Completed},
}

// CanTransition reports whether an order may go from one status to another.
func CanTransition(from, to string) bool {
	return slices.Contains(transitions[from], to)
}

// StaffCanSet reports whether the store team may make a change. Only a
// confirmed payment marks an order as paid.
func StaffCanSet(from, to string) bool {
	return to != Paid && CanTransition(from, to)
}

// Next lists the statuses the store team may choose for an order.
func Next(from string) []string {
	var options []string
	for _, to := range transitions[from] {
		if StaffCanSet(from, to) {
			options = append(options, to)
		}
	}
	return options
}

// NeedsRefund reports whether canceling from this status returns money to the buyer.
func NeedsRefund(from string) bool {
	return from == Paid || from == Processing || from == DeliveryFailed
}

// Line is a quantity of one variant at a unit price.
type Line struct {
	Quantity  int32
	UnitPrice int64
}

// Total computes the amount of a line.
func (l Line) Total() int64 { return int64(l.Quantity) * l.UnitPrice }

// MaxOrderAmount bounds an order to keep every sum far from overflow.
const MaxOrderAmount = math.MaxInt64 / 1000

// Totals computes subtotal and total. Taxes are zero until fiscal rules are
// configured and validated by a specialist.
func Totals(lines []Line, shipping int64) (subtotal, total int64, err error) {
	tooLarge := apperr.New(apperr.KindUnprocessable, "ORDER_TOO_LARGE", "The order exceeds the allowed amount.")
	for _, line := range lines {
		if line.Quantity <= 0 || line.UnitPrice < 0 {
			return 0, 0, apperr.New(apperr.KindInvalid, "INVALID_LINE", "The cart has an invalid item.")
		}
		if line.UnitPrice > MaxOrderAmount/int64(line.Quantity) || subtotal > MaxOrderAmount-line.Total() {
			return 0, 0, tooLarge
		}
		subtotal += line.Total()
	}
	if shipping < 0 || shipping > MaxOrderAmount-subtotal {
		return 0, 0, tooLarge
	}
	return subtotal, subtotal + shipping, nil
}
