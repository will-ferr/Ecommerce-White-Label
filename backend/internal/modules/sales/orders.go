package sales

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/modules/inventory"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/modules/sales/internal/domain"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/modules/sales/internal/payments"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/modules/sales/internal/sqlcgen"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/authz"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/tenancy"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/shared/apperr"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/shared/br"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/shared/pagination"
)

func normalizeName(raw string) (string, error) {
	name := strings.Join(strings.Fields(raw), " ")
	length := utf8.RuneCountInString(name)
	if length < 2 || length > 120 || strings.ContainsFunc(name, unicode.IsControl) {
		return "", errors.New("invalid name")
	}
	return name, nil
}

func orderNotFound() error {
	return apperr.New(apperr.KindNotFound, "ORDER_NOT_FOUND", "The order does not exist.")
}

// OrderItem is one line of an order, frozen at the time of purchase.
type OrderItem struct {
	VariantID   uuid.UUID `json:"variantId"`
	ProductName string    `json:"productName"`
	Label       string    `json:"label"`
	SKU         string    `json:"sku"`
	Quantity    int32     `json:"quantity"`
	UnitPrice   int64     `json:"unitPrice"`
	Total       int64     `json:"total"`
}

// HistoryEntry is one change of status.
type HistoryEntry struct {
	From      string    `json:"from"`
	To        string    `json:"to"`
	ActorType string    `json:"actorType"`
	Note      string    `json:"note"`
	CreatedAt time.Time `json:"createdAt"`
}

// PaymentView summarizes the payment of an order.
type PaymentView struct {
	Provider string `json:"provider"`
	Label    string `json:"label"`
	Status   string `json:"status"`
	Amount   int64  `json:"amount"`
}

// Note is an internal comment of the store team.
type Note struct {
	AuthorID  uuid.UUID `json:"authorId"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"createdAt"`
}

// OrderView is an order with its items. Notes and next steps are filled only
// for the store team.
type OrderView struct {
	ID              uuid.UUID      `json:"id"`
	Number          int64          `json:"number"`
	Status          string         `json:"status"`
	CustomerName    string         `json:"customerName"`
	CustomerEmail   string         `json:"customerEmail"`
	ShippingAddress br.Address     `json:"shippingAddress"`
	ShippingMethod  string         `json:"shippingMethod"`
	Subtotal        int64          `json:"subtotal"`
	ShippingAmount  int64          `json:"shippingAmount"`
	Discount        int64          `json:"discount"`
	CouponCode      string         `json:"couponCode"`
	Tax             int64          `json:"tax"`
	Total           int64          `json:"total"`
	Currency        string         `json:"currency"`
	TrackingCode    string         `json:"trackingCode"`
	CancelReason    string         `json:"cancelReason"`
	PaymentDueAt    time.Time      `json:"paymentDueAt"`
	CreatedAt       time.Time      `json:"createdAt"`
	Items           []OrderItem    `json:"items"`
	Payment         *PaymentView   `json:"payment,omitempty"`
	History         []HistoryEntry `json:"history,omitempty"`
	Notes           []Note         `json:"notes,omitempty"`
	NextStatuses    []string       `json:"nextStatuses,omitempty"`
	TestPayment     bool           `json:"testPayment"`
}

func (s *Service) view(order sqlcgen.OrdersOrder, items []sqlcgen.OrdersItem) OrderView {
	view := OrderView{
		ID: order.ID, Number: order.Number, Status: order.Status, CustomerName: order.CustomerName,
		CustomerEmail: order.CustomerEmail, ShippingMethod: order.ShippingMethod, Subtotal: order.SubtotalAmount,
		ShippingAmount: order.ShippingAmount, Discount: order.DiscountAmount, CouponCode: order.CouponCode, Tax: order.TaxAmount, Total: order.TotalAmount,
		Currency: order.Currency, TrackingCode: order.TrackingCode, CancelReason: order.CancelReason,
		PaymentDueAt: order.PaymentDueAt, CreatedAt: order.CreatedAt, Items: []OrderItem{},
	}
	_ = json.Unmarshal(order.ShippingAddress, &view.ShippingAddress)
	for _, item := range items {
		if item.OrderID != order.ID {
			continue
		}
		view.Items = append(view.Items, OrderItem{
			VariantID: item.VariantID, ProductName: item.ProductName, Label: item.VariantLabel, SKU: item.Sku,
			Quantity: item.Quantity, UnitPrice: item.UnitAmount, Total: item.TotalAmount,
		})
	}
	return view
}

func (s *Service) detail(ctx context.Context, q *sqlcgen.Queries, order sqlcgen.OrdersOrder) (OrderView, error) {
	items, err := q.ListOrderItems(ctx, sqlcgen.ListOrderItemsParams{TenantID: order.TenantID, OrderIds: []uuid.UUID{order.ID}})
	if err != nil {
		return OrderView{}, fmt.Errorf("list items: %w", err)
	}
	view := s.view(order, items)
	payment, err := q.GetPaymentByOrder(ctx, sqlcgen.GetPaymentByOrderParams{TenantID: order.TenantID, OrderID: order.ID})
	if err == nil {
		view.Payment = &PaymentView{Provider: payment.Provider, Label: s.providerLabel(payment.Provider), Status: payment.Status, Amount: payment.Amount}
		view.TestPayment = s.testProvider != nil && payment.Provider == s.testProvider.Name() && payment.Status == paymentPending && order.Status == domain.PendingPayment
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return OrderView{}, err
	}
	history, err := q.ListStatusHistory(ctx, sqlcgen.ListStatusHistoryParams{TenantID: order.TenantID, OrderID: order.ID})
	if err != nil {
		return OrderView{}, err
	}
	for _, entry := range history {
		view.History = append(view.History, HistoryEntry{From: entry.FromStatus, To: entry.ToStatus, ActorType: entry.ActorType, Note: entry.Note, CreatedAt: entry.CreatedAt})
	}
	return view, nil
}

// canSee lets a buyer open an order with their account or the guest link.
func (s *Service) canSee(order sqlcgen.OrdersOrder, principal *authz.Principal, token string) bool {
	if principal != nil && order.CustomerUserID != nil && *order.CustomerUserID == principal.UserID {
		return true
	}
	return s.validAccessToken(order.TenantID, order.ID, token)
}

// CustomerOrder returns an order to its buyer. Orders of other buyers look nonexistent.
func (s *Service) CustomerOrder(ctx context.Context, number int64, principal *authz.Principal, token string) (OrderView, error) {
	var view OrderView
	err := s.in(ctx, func(_ pgx.Tx, q *sqlcgen.Queries, tenantID uuid.UUID) error {
		order, err := q.GetOrderByNumber(ctx, sqlcgen.GetOrderByNumberParams{TenantID: tenantID, Number: number})
		if errors.Is(err, pgx.ErrNoRows) || (err == nil && !s.canSee(order, principal, token)) {
			return orderNotFound()
		}
		if err != nil {
			return err
		}
		view, err = s.detail(ctx, q, order)
		return err
	})
	return view, err
}

// CustomerOrders lists the orders of a signed-in buyer.
func (s *Service) CustomerOrders(ctx context.Context, principal authz.Principal, cursor string) (pagination.Page[OrderView], error) {
	return s.listOrders(ctx, OrderFilter{CustomerUserID: &principal.UserID, Cursor: cursor})
}

// OrderFilter selects orders.
type OrderFilter struct {
	Status         string
	Search         string
	CustomerUserID *uuid.UUID
	Cursor         string
	Limit          int32
}

func (s *Service) listOrders(ctx context.Context, filter OrderFilter) (pagination.Page[OrderView], error) {
	page := pagination.Page[OrderView]{Items: []OrderView{}}
	cursor, err := pagination.Decode(filter.Cursor)
	if err != nil {
		return page, err
	}
	limit := filter.Limit
	if limit <= 0 || limit > pagination.MaxLimit {
		limit = pagination.DefaultLimit
	}
	err = s.in(ctx, func(_ pgx.Tx, q *sqlcgen.Queries, tenantID uuid.UUID) error {
		params := sqlcgen.ListOrdersParams{TenantID: tenantID, CustomerUserID: filter.CustomerUserID, RowLimit: limit + 1}
		if filter.Status != "" {
			params.Status = &filter.Status
		}
		if search := strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(strings.TrimSpace(filter.Search)); search != "" && len(search) <= 100 {
			params.Search = &search
		}
		if cursor != nil {
			params.AfterAt, params.AfterID = &cursor.At, &cursor.ID
		}
		orders, err := q.ListOrders(ctx, params)
		if err != nil {
			return fmt.Errorf("list orders: %w", err)
		}
		if len(orders) > int(limit) {
			orders = orders[:limit]
			last := orders[len(orders)-1]
			page.NextCursor = pagination.Cursor{At: last.CreatedAt, ID: last.ID}.Encode()
		}
		ids := make([]uuid.UUID, 0, len(orders))
		for _, order := range orders {
			ids = append(ids, order.ID)
		}
		items, err := q.ListOrderItems(ctx, sqlcgen.ListOrderItemsParams{TenantID: tenantID, OrderIds: ids})
		if err != nil {
			return err
		}
		for _, order := range orders {
			page.Items = append(page.Items, s.view(order, items))
		}
		return nil
	})
	return page, err
}

type actor struct {
	kind string
	id   string
}

func (s *Service) changeStatus(ctx context.Context, tx pgx.Tx, q *sqlcgen.Queries, order sqlcgen.OrdersOrder, to, tracking, reason string, by actor) error {
	if tracking == "" {
		tracking = order.TrackingCode
	}
	if err := q.UpdateOrderStatus(ctx, sqlcgen.UpdateOrderStatusParams{
		TenantID: order.TenantID, ID: order.ID, Status: to, TrackingCode: tracking, CancelReason: reason,
	}); err != nil {
		return fmt.Errorf("update order: %w", err)
	}
	note := reason
	if note == "" && tracking != order.TrackingCode {
		note = "Código de rastreio: " + tracking
	}
	if err := q.InsertStatusHistory(ctx, sqlcgen.InsertStatusHistoryParams{
		TenantID: order.TenantID, ID: newID(), OrderID: order.ID, FromStatus: order.Status, ToStatus: to,
		ActorType: by.kind, ActorID: by.id, Note: note,
	}); err != nil {
		return fmt.Errorf("insert history: %w", err)
	}
	if eventType, ok := statusEvents[to]; ok {
		return recordOrderEvent(ctx, tx, order.TenantID, order.ID, eventType)
	}
	return nil
}

func (s *Service) setPayment(ctx context.Context, q *sqlcgen.Queries, payment sqlcgen.PaymentsPayment, status, kind string, amount int64) error {
	if err := q.UpdatePayment(ctx, sqlcgen.UpdatePaymentParams{TenantID: payment.TenantID, ID: payment.ID, Status: status, ProviderReference: payment.ProviderReference}); err != nil {
		return fmt.Errorf("update payment: %w", err)
	}
	if kind == "" {
		return nil
	}
	return q.InsertPaymentTransaction(ctx, sqlcgen.InsertPaymentTransactionParams{
		TenantID: payment.TenantID, ID: newID(), PaymentID: payment.ID, Kind: kind, Amount: amount, ProviderReference: payment.ProviderReference,
	})
}

// requestRefund marks a captured payment for refund. The refund itself runs
// in the background (RefundSubscription), so a slow gateway never holds the
// order transaction.
func (s *Service) requestRefund(ctx context.Context, tx pgx.Tx, q *sqlcgen.Queries, payment sqlcgen.PaymentsPayment) error {
	if err := s.setPayment(ctx, q, payment, paymentRefundPending, "", 0); err != nil {
		return err
	}
	return recordPaymentEvent(ctx, tx, payment.TenantID, payment.ID, payment.OrderID, EventRefundRequested)
}

// cancel ends an order and undoes its effects: held stock is released, or
// paid stock returns to the shelf and the payment is refunded.
func (s *Service) cancel(ctx context.Context, tx pgx.Tx, q *sqlcgen.Queries, order sqlcgen.OrdersOrder, reason string, by actor) error {
	if !domain.CanTransition(order.Status, domain.Canceled) {
		return apperr.New(apperr.KindConflict, "INVALID_STATUS_CHANGE", "This order can no longer be canceled.")
	}
	payment, err := q.LockPaymentByOrder(ctx, sqlcgen.LockPaymentByOrderParams{TenantID: order.TenantID, OrderID: order.ID})
	if err != nil {
		return fmt.Errorf("lock payment: %w", err)
	}
	reference := fmt.Sprintf("Pedido %d", order.Number)
	if domain.NeedsRefund(order.Status) {
		if payment.Status == paymentCaptured {
			if err := s.requestRefund(ctx, tx, q, payment); err != nil {
				return err
			}
		}
		if err := inventory.Restock(ctx, tx, order.TenantID, order.ID, reference); err != nil {
			return err
		}
	} else {
		if payment.Status == paymentPending {
			if err := s.setPayment(ctx, q, payment, paymentCanceled, "cancel", 0); err != nil {
				return err
			}
		}
		if err := inventory.Release(ctx, tx, order.TenantID, order.ID); err != nil {
			return err
		}
	}
	if err := releaseCoupon(ctx, q, order.TenantID, order.ID); err != nil {
		return err
	}
	return s.changeStatus(ctx, tx, q, order, domain.Canceled, "", reason, by)
}

// ConfirmTestPayment lets the buyer approve or refuse a test payment. It
// produces the signed notification a gateway would send and delivers it to
// the same webhook flow, so the test exercises the real confirmation path.
func (s *Service) ConfirmTestPayment(ctx context.Context, number int64, principal *authz.Principal, token string, approved bool) (OrderView, error) {
	if s.testProvider == nil {
		return OrderView{}, apperr.New(apperr.KindNotFound, "ROUTE_NOT_FOUND", "The requested resource does not exist.")
	}
	var payment sqlcgen.PaymentsPayment
	var orderID uuid.UUID
	err := s.in(ctx, func(_ pgx.Tx, q *sqlcgen.Queries, tenantID uuid.UUID) error {
		order, err := q.GetOrderByNumber(ctx, sqlcgen.GetOrderByNumberParams{TenantID: tenantID, Number: number})
		if errors.Is(err, pgx.ErrNoRows) || (err == nil && !s.canSee(order, principal, token)) {
			return orderNotFound()
		}
		if err != nil {
			return err
		}
		payment, err = q.GetPaymentByOrder(ctx, sqlcgen.GetPaymentByOrderParams{TenantID: tenantID, OrderID: order.ID})
		if err != nil {
			return err
		}
		if order.Status != domain.PendingPayment || payment.Status != paymentPending || payment.Provider != s.testProvider.Name() || payment.ProviderReference == "" {
			return apperr.New(apperr.KindConflict, "PAYMENT_NOT_PENDING", "This order is not waiting for payment.")
		}
		if !s.now().Before(order.PaymentDueAt) {
			return apperr.New(apperr.KindConflict, "PAYMENT_EXPIRED", "The payment deadline has passed.")
		}
		orderID = order.ID
		return nil
	})
	if err != nil {
		return OrderView{}, err
	}

	status := payments.StatusFailed
	if approved {
		status = payments.StatusCaptured
	}
	header, body, err := s.testProvider.Simulate(payment.ProviderReference, status, payment.Amount)
	if err != nil {
		return OrderView{}, err
	}
	if _, err := s.HandlePaymentNotification(ctx, s.testProvider.Name(), header, body); err != nil {
		return OrderView{}, err
	}

	var view OrderView
	err = s.in(ctx, func(_ pgx.Tx, q *sqlcgen.Queries, tenantID uuid.UUID) error {
		order, err := q.GetOrder(ctx, sqlcgen.GetOrderParams{TenantID: tenantID, ID: orderID})
		if err != nil {
			return err
		}
		view, err = s.detail(ctx, q, order)
		return err
	})
	return view, err
}

// ExpireOverdue cancels orders whose payment deadline has passed and releases
// their stock. A transaction-scoped advisory lock keeps instances from
// processing the same orders at once.
func (s *Service) ExpireOverdue(ctx context.Context) (int, error) {
	expired := 0
	err := pgx.BeginFunc(ctx, s.pool, func(lockTx pgx.Tx) error {
		var acquired bool
		if err := lockTx.QueryRow(ctx, `SELECT pg_try_advisory_xact_lock(hashtext('sales.expire_overdue'))`).Scan(&acquired); err != nil || !acquired {
			return err
		}
		rows, err := lockTx.Query(ctx, `SELECT tenant_id, order_id FROM orders.overdue_orders(100)`)
		if err != nil {
			return fmt.Errorf("list overdue orders: %w", err)
		}
		type overdue struct{ tenantID, orderID uuid.UUID }
		overdueOrders, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (overdue, error) {
			var item overdue
			return item, row.Scan(&item.tenantID, &item.orderID)
		})
		if err != nil {
			return err
		}
		for _, item := range overdueOrders {
			err := s.in(tenancy.WithTenant(ctx, item.tenantID), func(tx pgx.Tx, q *sqlcgen.Queries, tenantID uuid.UUID) error {
				order, err := q.LockOrder(ctx, sqlcgen.LockOrderParams{TenantID: tenantID, ID: item.orderID})
				if err != nil || order.Status != domain.PendingPayment || s.now().Before(order.PaymentDueAt) {
					return err
				}
				return s.cancel(ctx, tx, q, order, "Prazo de pagamento expirado", actor{kind: "system", id: "expiry"})
			})
			if err != nil {
				s.logger.ErrorContext(ctx, "orders.expiry_failed", "tenant_id", item.tenantID, "order_id", item.orderID, "error", err)
				continue
			}
			expired++
		}
		return nil
	})
	return expired, err
}

// RunExpiry checks for overdue orders until ctx is canceled.
func (s *Service) RunExpiry(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if count, err := s.ExpireOverdue(ctx); err != nil {
				s.logger.ErrorContext(ctx, "orders.expiry_failed", "error", err)
			} else if count > 0 {
				s.logger.InfoContext(ctx, "orders.expired", "count", count)
			}
		}
	}
}
