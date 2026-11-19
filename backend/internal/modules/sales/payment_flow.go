package sales

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/modules/inventory"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/modules/sales/internal/domain"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/modules/sales/internal/payments"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/modules/sales/internal/sqlcgen"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/messaging"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/shared/apperr"
)

// Payment statuses.
const (
	paymentPending       = "pending"
	paymentCaptured      = "captured"
	paymentFailed        = "failed"
	paymentCanceled      = "canceled"
	paymentRefundPending = "refund_pending"
	paymentRefunded      = "refunded"
)

// Outcomes of a payment notification, stored with it for support.
const (
	outcomeDuplicate      = "duplicate"
	outcomeUnknownPayment = "unknown_payment"
	outcomeAmountMismatch = "amount_mismatch"
	outcomeCaptured       = "captured"
	outcomeLateCapture    = "late_capture_refund_requested"
	outcomeFailed         = "failed"
	outcomeRefunded       = "refunded"
	outcomeIgnored        = "ignored"
)

// HandlePaymentNotification authenticates a webhook of a provider and applies
// it to the payment and its order. A notification is applied once: repeated
// deliveries return "duplicate" without any effect.
func (s *Service) HandlePaymentNotification(ctx context.Context, providerName string, header http.Header, body []byte) (string, error) {
	provider, ok := s.providers[providerName]
	if !ok {
		return "", apperr.New(apperr.KindNotFound, "ROUTE_NOT_FOUND", "The requested resource does not exist.")
	}
	notification, err := provider.ParseNotification(header, body)
	if err != nil {
		s.logger.WarnContext(ctx, "payments.notification_rejected", "provider", providerName, "error", err)
		return "", apperr.New(apperr.KindUnauthenticated, "WEBHOOK_SIGNATURE_INVALID", "The notification could not be authenticated.")
	}

	var outcome string
	err = s.in(ctx, func(tx pgx.Tx, q *sqlcgen.Queries, tenantID uuid.UUID) error {
		webhookID := newID()
		inserted, err := q.InsertWebhookEvent(ctx, sqlcgen.InsertWebhookEventParams{
			TenantID: tenantID, ID: webhookID, Provider: providerName, ProviderEventID: notification.EventID,
			ProviderReference: notification.Reference, Status: notification.Status, Amount: notification.Amount,
		})
		if err != nil {
			return fmt.Errorf("record notification: %w", err)
		}
		if inserted == 0 {
			outcome = outcomeDuplicate
			return nil
		}
		outcome, err = s.applyNotification(ctx, tx, q, tenantID, providerName, notification)
		if err != nil {
			return err
		}
		return q.SetWebhookOutcome(ctx, sqlcgen.SetWebhookOutcomeParams{TenantID: tenantID, ID: webhookID, Outcome: outcome})
	})
	if err != nil {
		return "", err
	}
	s.logger.InfoContext(ctx, "payments.notification_applied", "provider", providerName, "status", notification.Status, "outcome", outcome)
	return outcome, nil
}

func (s *Service) applyNotification(ctx context.Context, tx pgx.Tx, q *sqlcgen.Queries, tenantID uuid.UUID, providerName string, n payments.Notification) (string, error) {
	payment, err := q.LockPaymentByReference(ctx, sqlcgen.LockPaymentByReferenceParams{TenantID: tenantID, Provider: providerName, ProviderReference: n.Reference})
	if errors.Is(err, pgx.ErrNoRows) {
		return outcomeUnknownPayment, nil
	}
	if err != nil {
		return "", fmt.Errorf("lock payment: %w", err)
	}
	if n.Amount != nil && *n.Amount != payment.Amount {
		s.logger.ErrorContext(ctx, "payments.amount_mismatch", "payment_id", payment.ID, "expected", payment.Amount, "received", *n.Amount)
		return outcomeAmountMismatch, nil
	}
	order, err := q.LockOrder(ctx, sqlcgen.LockOrderParams{TenantID: tenantID, ID: payment.OrderID})
	if err != nil {
		return "", fmt.Errorf("lock order: %w", err)
	}
	system := actor{kind: "system", id: providerName}
	label := s.providerLabel(providerName)

	switch n.Status {
	case payments.StatusCaptured:
		switch {
		case payment.Status == paymentPending && order.Status == domain.PendingPayment:
			if err := s.setPayment(ctx, q, payment, paymentCaptured, "capture", payment.Amount); err != nil {
				return "", err
			}
			if err := inventory.Commit(ctx, tx, tenantID, order.ID, fmt.Sprintf("Pedido %d", order.Number)); err != nil {
				return "", err
			}
			return outcomeCaptured, s.changeStatus(ctx, tx, q, order, domain.Paid, "", label+": pagamento aprovado", system)
		case payment.Status == paymentPending || payment.Status == paymentCanceled || payment.Status == paymentFailed:
			// The money arrived after the order was canceled, for example after
			// the payment deadline. It goes back to the buyer.
			if err := s.setPayment(ctx, q, payment, paymentCaptured, "capture", payment.Amount); err != nil {
				return "", err
			}
			payment.Status = paymentCaptured
			s.logger.WarnContext(ctx, "payments.late_capture", "payment_id", payment.ID, "order_status", order.Status)
			return outcomeLateCapture, s.requestRefund(ctx, tx, q, payment)
		}
	case payments.StatusFailed:
		if payment.Status == paymentPending && order.Status == domain.PendingPayment {
			if err := s.setPayment(ctx, q, payment, paymentFailed, "failure", 0); err != nil {
				return "", err
			}
			if err := inventory.Release(ctx, tx, tenantID, order.ID); err != nil {
				return "", err
			}
			if err := releaseCoupon(ctx, q, tenantID, order.ID); err != nil {
				return "", err
			}
			return outcomeFailed, s.changeStatus(ctx, tx, q, order, domain.Canceled, "", label+": pagamento recusado", system)
		}
	case payments.StatusRefunded:
		if payment.Status == paymentRefundPending || payment.Status == paymentCaptured {
			wasPending := payment.Status == paymentRefundPending
			if err := s.completeRefund(ctx, tx, q, payment); err != nil {
				return "", err
			}
			// A refund made directly at the provider also cancels the order
			// when it still can be canceled.
			if !wasPending && domain.CanTransition(order.Status, domain.Canceled) {
				if err := inventory.Restock(ctx, tx, tenantID, order.ID, fmt.Sprintf("Pedido %d", order.Number)); err != nil {
					return "", err
				}
				if err := releaseCoupon(ctx, q, tenantID, order.ID); err != nil {
					return "", err
				}
				if err := s.changeStatus(ctx, tx, q, order, domain.Canceled, "", label+": pagamento estornado", system); err != nil {
					return "", err
				}
			}
			return outcomeRefunded, nil
		}
	}
	return outcomeIgnored, nil
}

func (s *Service) completeRefund(ctx context.Context, tx pgx.Tx, q *sqlcgen.Queries, payment sqlcgen.PaymentsPayment) error {
	if err := s.setPayment(ctx, q, payment, paymentRefunded, "refund", payment.Amount); err != nil {
		return err
	}
	return recordPaymentEvent(ctx, tx, payment.TenantID, payment.ID, payment.OrderID, EventRefundCompleted)
}

// RefundSubscription returns money for payments marked for refund. The
// provider receives the payment ID as idempotency key, so a retried event
// never refunds twice.
func (s *Service) RefundSubscription() messaging.Subscription {
	return messaging.Subscription{
		Name: "sales-refunds", Events: []string{EventRefundRequested},
		Handle: func(ctx context.Context, tx pgx.Tx, event messaging.Event) error {
			var data PaymentEvent
			if err := event.Decode(&data); err != nil {
				return err
			}
			q := sqlcgen.New(tx)
			payment, err := q.GetPayment(ctx, sqlcgen.GetPaymentParams{TenantID: event.TenantID, ID: data.PaymentID})
			if errors.Is(err, pgx.ErrNoRows) {
				return messaging.Permanent(fmt.Errorf("payment %s not found", data.PaymentID))
			}
			if err != nil || payment.Status != paymentRefundPending {
				return err
			}
			provider, ok := s.providers[payment.Provider]
			if !ok {
				return messaging.Permanent(fmt.Errorf("payment provider %q is not configured", payment.Provider))
			}
			// The call happens before the row is locked, so a slow gateway
			// does not hold the payment.
			if err := provider.Refund(ctx, payment.ProviderReference, payment.Amount, payment.ID.String()); err != nil {
				return fmt.Errorf("refund at %s: %w", payment.Provider, err)
			}
			payment, err = q.LockPayment(ctx, sqlcgen.LockPaymentParams{TenantID: event.TenantID, ID: data.PaymentID})
			if err != nil || payment.Status != paymentRefundPending {
				return err
			}
			return s.completeRefund(ctx, tx, q, payment)
		},
	}
}
