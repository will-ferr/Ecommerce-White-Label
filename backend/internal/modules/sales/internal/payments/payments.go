// Package payments defines how the platform talks to payment providers. Card
// data never passes through the platform (ADR-012).
package payments

import (
	"context"
	"errors"
	"net/http"

	"github.com/google/uuid"
)

// Notification statuses understood by the order flow.
const (
	StatusCaptured = "captured"
	StatusFailed   = "failed"
	StatusRefunded = "refunded"
)

// ErrInvalidNotification means a notification could not be authenticated or read.
var ErrInvalidNotification = errors.New("invalid payment notification")

// Charge asks a provider to collect an amount for an order.
type Charge struct {
	PaymentID uuid.UUID
	OrderID   uuid.UUID
	Amount    int64
	Currency  string
}

// Notification is a verified message from a provider about a payment.
type Notification struct {
	EventID   string
	Reference string
	Status    string
	Amount    *int64
}

// Provider is implemented by each payment gateway. A gateway confirms payments
// through signed notifications (webhooks); its answer to a request is never
// trusted alone to mark an order as paid.
type Provider interface {
	// Name identifies the provider in stored payments and webhook addresses.
	Name() string
	// Label is shown to buyers and to the store team.
	Label() string
	// CreateCharge starts the collection and returns the provider reference.
	CreateCharge(ctx context.Context, charge Charge) (reference string, err error)
	// Refund returns an amount. The idempotency key makes repeated calls safe.
	Refund(ctx context.Context, reference string, amount int64, idempotencyKey string) error
	// ParseNotification authenticates a webhook and reads it. It returns
	// ErrInvalidNotification when the signature or content is not valid.
	ParseNotification(header http.Header, body []byte) (Notification, error)
}
