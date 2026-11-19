// Package payments defines how the platform talks to payment providers.
package payments

import (
	"context"

	"github.com/google/uuid"
)

// Charge asks a provider to collect an amount for an order.
type Charge struct {
	OrderID  uuid.UUID
	Amount   int64
	Currency string
}

// Provider is implemented by each payment gateway. Real gateways arrive in
// Phase 6; card data never passes through the platform (ADR-012).
type Provider interface {
	Name() string
	Label() string
	CreateCharge(ctx context.Context, charge Charge) (reference string, err error)
	Refund(ctx context.Context, reference string, amount int64) error
}

// TestProvider simulates a gateway so the whole purchase can be exercised. It is
// labeled as a test everywhere and refused by the configuration in production.
type TestProvider struct{}

// Name implements Provider.
func (TestProvider) Name() string { return "test" }

// Label implements Provider.
func (TestProvider) Label() string { return "Pagamento de teste" }

// CreateCharge implements Provider.
func (TestProvider) CreateCharge(context.Context, Charge) (string, error) {
	return "test_" + uuid.NewString(), nil
}

// Refund implements Provider.
func (TestProvider) Refund(context.Context, string, int64) error { return nil }
