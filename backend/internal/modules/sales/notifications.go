package sales

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/modules/sales/internal/sqlcgen"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/modules/tenants"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/mail"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/messaging"
)

// NotificationSubscription e-mails buyers about their orders. The message is
// stored in the same transaction that records the event as handled, so each
// event produces exactly one message.
func (s *Service) NotificationSubscription() messaging.Subscription {
	return messaging.Subscription{
		Name:   "sales-notifications",
		Events: []string{EventOrderCreated, EventOrderPaid, EventOrderShipped, EventOrderCanceled, EventRefundCompleted},
		Handle: s.notify,
	}
}

func (s *Service) notify(ctx context.Context, tx pgx.Tx, event messaging.Event) error {
	if s.mail == nil {
		return messaging.Permanent(errors.New("e-mail is not configured"))
	}
	var data struct {
		OrderID uuid.UUID `json:"orderId"`
	}
	if err := event.Decode(&data); err != nil {
		return err
	}
	q := sqlcgen.New(tx)
	order, err := q.GetOrder(ctx, sqlcgen.GetOrderParams{TenantID: event.TenantID, ID: data.OrderID})
	if errors.Is(err, pgx.ErrNoRows) {
		return messaging.Permanent(fmt.Errorf("order %s not found", data.OrderID))
	}
	if err != nil {
		return err
	}
	if order.StoreHost == "" {
		// Orders placed before store addresses were recorded have no link to send.
		return nil
	}
	store, err := tenants.StoreName(ctx, tx, event.TenantID)
	if err != nil {
		return err
	}
	items, err := q.ListOrderItems(ctx, sqlcgen.ListOrderItemsParams{TenantID: event.TenantID, OrderIds: []uuid.UUID{order.ID}})
	if err != nil {
		return err
	}
	link := s.storeLink(order.StoreHost, "/pedido/"+strconv.FormatInt(order.Number, 10)+"?token="+s.accessToken(order.TenantID, order.ID))
	message, ok := orderMessage(event.Type, store, order, items, link)
	if !ok {
		return nil
	}
	return s.mail.Write(ctx, tx, event.TenantID, message)
}

// orderMessage writes the e-mail for an order event.
func orderMessage(eventType, store string, order sqlcgen.OrdersOrder, items []sqlcgen.OrdersItem, link string) (mail.Message, bool) {
	number := strconv.FormatInt(order.Number, 10)
	greeting := "Olá, " + firstName(order.CustomerName) + ".\n\n"
	footer := "\n\nAcompanhe seu pedido em:\n" + link + "\n\n" + store
	message := mail.Message{To: order.CustomerEmail}

	switch eventType {
	case EventOrderCreated:
		var lines strings.Builder
		for _, item := range items {
			label := item.ProductName
			if item.VariantLabel != "" {
				label += " (" + item.VariantLabel + ")"
			}
			fmt.Fprintf(&lines, "- %d× %s: %s\n", item.Quantity, label, formatMoney(item.TotalAmount, order.Currency))
		}
		message.Kind, message.Subject = "orders.created", "Recebemos seu pedido "+number+" | "+store
		message.Body = greeting + "Recebemos seu pedido " + number + ".\n\n" + lines.String() +
			"\nFrete (" + order.ShippingMethod + "): " + formatMoney(order.ShippingAmount, order.Currency) +
			"\nTotal: " + formatMoney(order.TotalAmount, order.Currency) +
			"\n\nO pedido será separado assim que o pagamento for confirmado." + footer
	case EventOrderPaid:
		message.Kind, message.Subject = "orders.paid", "Pagamento confirmado: pedido "+number+" | "+store
		message.Body = greeting + "O pagamento do pedido " + number + " foi confirmado. Agora vamos separar seus produtos." + footer
	case EventOrderShipped:
		message.Kind, message.Subject = "orders.shipped", "Pedido "+number+" enviado | "+store
		message.Body = greeting + "O pedido " + number + " foi enviado."
		if order.TrackingCode != "" {
			message.Body += "\n\nCódigo de rastreio: " + order.TrackingCode
		}
		message.Body += footer
	case EventOrderCanceled:
		message.Kind, message.Subject = "orders.canceled", "Pedido "+number+" cancelado | "+store
		message.Body = greeting + "O pedido " + number + " foi cancelado."
		if order.CancelReason != "" {
			message.Body += "\n\nMotivo: " + order.CancelReason
		}
		message.Body += "\n\nSe algum valor foi pago, ele será estornado e você receberá uma confirmação." + footer
	case EventRefundCompleted:
		message.Kind, message.Subject = "orders.refunded", "Estorno do pedido "+number+" concluído | "+store
		message.Body = greeting + "O estorno de " + formatMoney(order.TotalAmount, order.Currency) + " do pedido " + number +
			" foi concluído. O prazo para o valor aparecer depende do meio de pagamento." + footer
	default:
		return mail.Message{}, false
	}
	return message, true
}

func firstName(name string) string {
	if fields := strings.Fields(name); len(fields) > 0 {
		return fields[0]
	}
	return name
}

// formatMoney writes an amount in the Brazilian format, such as "R$ 1.234,56".
func formatMoney(cents int64, currency string) string {
	sign := ""
	if cents < 0 {
		sign, cents = "-", -cents
	}
	whole := strconv.FormatInt(cents/100, 10)
	var grouped strings.Builder
	for i, digit := range whole {
		if i > 0 && (len(whole)-i)%3 == 0 {
			grouped.WriteByte('.')
		}
		grouped.WriteRune(digit)
	}
	symbol := currency + " "
	if currency == "BRL" {
		symbol = "R$ "
	}
	return fmt.Sprintf("%s%s%s,%02d", sign, symbol, grouped.String(), cents%100)
}
