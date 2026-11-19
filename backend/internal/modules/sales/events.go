package sales

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/modules/sales/internal/domain"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/messaging"
)

// Events published by the sales module. They carry identifiers only.
const (
	EventOrderCreated    = "orders.order.created.v1"
	EventOrderPaid       = "orders.order.paid.v1"
	EventOrderShipped    = "orders.order.shipped.v1"
	EventOrderCanceled   = "orders.order.canceled.v1"
	EventRefundRequested = "payments.refund.requested.v1"
	EventRefundCompleted = "payments.refund.completed.v1"
)

const eventSource = "/modules/sales"

// statusEvents maps order statuses to the event announced when an order reaches them.
var statusEvents = map[string]string{
	domain.Paid:     EventOrderPaid,
	domain.Shipped:  EventOrderShipped,
	domain.Canceled: EventOrderCanceled,
}

// OrderEvent is the data of order events.
type OrderEvent struct {
	OrderID uuid.UUID `json:"orderId"`
}

// PaymentEvent is the data of payment events.
type PaymentEvent struct {
	PaymentID uuid.UUID `json:"paymentId"`
	OrderID   uuid.UUID `json:"orderId"`
}

func recordOrderEvent(ctx context.Context, tx pgx.Tx, tenantID, orderID uuid.UUID, eventType string) error {
	return messaging.Record(ctx, tx, tenantID, eventSource, eventType, orderID, OrderEvent{OrderID: orderID})
}

func recordPaymentEvent(ctx context.Context, tx pgx.Tx, tenantID, paymentID, orderID uuid.UUID, eventType string) error {
	return messaging.Record(ctx, tx, tenantID, eventSource, eventType, paymentID, PaymentEvent{PaymentID: paymentID, OrderID: orderID})
}
