package sales

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"go.opentelemetry.io/otel/attribute"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/modules/inventory"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/modules/sales/internal/domain"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/modules/sales/internal/payments"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/modules/sales/internal/sqlcgen"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/observability"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/security"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/shared/apperr"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/shared/br"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/shared/email"
)

const checkoutScope = "checkout"

var idempotencyKeyPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{16,100}$`)

// ShippingOption is a delivery choice offered at checkout.
type ShippingOption struct {
	ID      uuid.UUID `json:"id"`
	Name    string    `json:"name"`
	Kind    string    `json:"kind"`
	Price   int64     `json:"price"`
	MinDays int32     `json:"minDays"`
	MaxDays int32     `json:"maxDays"`
}

// Summary is everything the buyer confirms before placing the order.
type Summary struct {
	Cart            CartView         `json:"cart"`
	ShippingOptions []ShippingOption `json:"shippingOptions"`
	Shipping        *ShippingOption  `json:"shipping"`
	Subtotal        int64            `json:"subtotal"`
	ShippingAmount  int64            `json:"shippingAmount"`
	Discount        int64            `json:"discount"`
	Tax             int64            `json:"tax"`
	Total           int64            `json:"total"`
	Coupon          *AppliedCoupon   `json:"coupon"`
	// CouponError explains why the coupon sent was not applied.
	CouponError  string `json:"couponError,omitempty"`
	Currency     string `json:"currency"`
	PaymentLabel string `json:"paymentLabel"`
	// TestPayment tells the buyer that nothing is charged.
	TestPayment bool `json:"testPayment"`
}

// CheckoutInput is what the buyer sends to place an order. It carries no
// prices: they are recalculated from the catalog. ExpectedTotal only detects a
// price that changed after the buyer saw it.
type CheckoutInput struct {
	CustomerName     string     `json:"customerName"`
	CustomerEmail    string     `json:"customerEmail"`
	Address          br.Address `json:"address"`
	ShippingMethodID uuid.UUID  `json:"shippingMethodId"`
	ExpectedTotal    int64      `json:"expectedTotal"`
	CouponCode       string     `json:"couponCode"`

	// StoreHost is the address where the order was placed, used in later
	// messages. It comes from the request, never from the body.
	StoreHost string `json:"-"`
}

// PlacedOrder is the answer to a successful checkout.
type PlacedOrder struct {
	OrderNumber  int64  `json:"orderNumber"`
	Status       string `json:"status"`
	Total        int64  `json:"total"`
	Currency     string `json:"currency"`
	OrderToken   string `json:"orderToken"`
	PaymentLabel string `json:"paymentLabel"`

	orderID uuid.UUID
}

type storedOrder struct {
	OrderID      uuid.UUID `json:"orderId"`
	OrderNumber  int64     `json:"orderNumber"`
	Status       string    `json:"status"`
	Total        int64     `json:"total"`
	Currency     string    `json:"currency"`
	PaymentLabel string    `json:"paymentLabel"`
}

// ReviewError means the order differs from what the buyer reviewed: a price
// changed or an item ran out. It carries the new totals for a second review.
type ReviewError struct {
	Code    string
	Summary Summary
}

func (e *ReviewError) Error() string { return strings.ToLower(e.Code) }

// Message describes the failure to the buyer.
func (e *ReviewError) Message() string {
	if e.Code == "OUT_OF_STOCK" {
		return "Some items are no longer available in the requested quantity."
	}
	return "Prices changed. Review the order and confirm again."
}

// Unwrap maps the failure to the API error format.
func (e *ReviewError) Unwrap() error {
	return apperr.New(apperr.KindConflict, e.Code, e.Message())
}

func toOption(method sqlcgen.ShippingMethod) ShippingOption {
	return ShippingOption{ID: method.ID, Name: method.Name, Kind: method.Kind, Price: method.PriceAmount, MinDays: method.MinDays, MaxDays: method.MaxDays}
}

// SummaryRequest is the delivery choice and coupon the buyer is considering.
type SummaryRequest struct {
	ShippingMethodID *uuid.UUID `json:"shippingMethodId"`
	PostalCode       string     `json:"postalCode"`
	CouponCode       string     `json:"couponCode"`
}

func (s *Service) summarize(ctx context.Context, tx pgx.Tx, q *sqlcgen.Queries, tenantID uuid.UUID, cart *sqlcgen.CartCart, request SummaryRequest) (Summary, error) {
	methodID, postalCode := request.ShippingMethodID, br.PostalCodeDigits(request.PostalCode)
	view, err := s.buildView(ctx, tx, q, tenantID, cart)
	if err != nil {
		return Summary{}, err
	}
	summary := Summary{Cart: view, Currency: view.Currency}
	if s.checkoutProvider != nil {
		summary.PaymentLabel = s.checkoutProvider.Label()
		summary.TestPayment = s.testProvider != nil && s.checkoutProvider.Name() == s.testProvider.Name()
	}
	var weight int32
	for _, line := range view.Lines {
		if line.Available {
			weight += line.weightGrams
		}
	}
	summary.ShippingOptions, err = s.shippingOptions(ctx, q, tenantID, postalCode, weight)
	if err != nil {
		return Summary{}, err
	}
	for _, option := range summary.ShippingOptions {
		if methodID != nil && option.ID == *methodID {
			summary.Shipping = &option
		}
	}
	lines := make([]domain.Line, 0, len(view.Lines))
	for _, line := range view.Lines {
		if line.Available {
			lines = append(lines, domain.Line{Quantity: line.Quantity, UnitPrice: line.UnitPrice})
		}
	}
	if summary.Shipping != nil {
		summary.ShippingAmount = summary.Shipping.Price
	}
	summary.Subtotal, summary.Total, err = domain.Totals(lines, summary.ShippingAmount)
	if err != nil {
		return Summary{}, err
	}
	if code := domain.NormalizeCouponCode(request.CouponCode); code != "" && len(lines) > 0 {
		if err := s.previewCoupon(ctx, q, tenantID, code, &summary); err != nil {
			return Summary{}, err
		}
	}
	return summary, nil
}

// Summary prices the cart with an optional delivery choice. Options priced by
// postal code appear once the postal code is known.
func (s *Service) Summary(ctx context.Context, ref CartRef, request SummaryRequest) (Summary, error) {
	var summary Summary
	err := s.in(ctx, func(tx pgx.Tx, q *sqlcgen.Queries, tenantID uuid.UUID) error {
		cart, err := s.findCart(ctx, q, tenantID, ref)
		if err != nil {
			return err
		}
		summary, err = s.summarize(ctx, tx, q, tenantID, cart, request)
		return err
	})
	return summary, err
}

func (input CheckoutInput) normalize() (CheckoutInput, error) {
	var fields []apperr.FieldError
	name, err := normalizeName(input.CustomerName)
	if err != nil {
		fields = append(fields, apperr.FieldError{Field: "customerName", Code: "invalid", Message: "Enter your name."})
	}
	input.CustomerName = name
	if input.CustomerEmail, err = email.Normalize(input.CustomerEmail); err != nil {
		fields = append(fields, apperr.FieldError{Field: "customerEmail", Code: "invalid", Message: "Enter a valid e-mail address."})
	}
	if input.Address, err = input.Address.Normalize(); err != nil {
		if appErr, ok := apperr.As(err); ok {
			fields = append(fields, appErr.Fields...)
		}
	}
	if len(fields) > 0 {
		return CheckoutInput{}, apperr.Validation(fields...)
	}
	return input, nil
}

// requestHash identifies a checkout request by its buyer and content. It does
// not depend on the cart, which is consumed by the first successful attempt.
func requestHash(ref CartRef, input CheckoutInput) ([]byte, error) {
	buyer := "visitor:" + string(security.HashToken(ref.Token))
	if ref.UserID != nil {
		buyer = "user:" + ref.UserID.String()
	}
	encoded, err := json.Marshal(struct {
		Buyer string
		Input CheckoutInput
	}{buyer, input})
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(encoded)
	return digest[:], nil
}

// PlaceOrder turns the cart into an order. Stock reservation, order, payment
// record and the idempotency answer are committed in a single transaction, so a
// failure leaves nothing behind and a repeated request returns the first order.
func (s *Service) PlaceOrder(ctx context.Context, ref CartRef, key string, input CheckoutInput) (PlacedOrder, error) {
	if s.checkoutProvider == nil {
		return PlacedOrder{}, apperr.New(apperr.KindUnavailable, "PAYMENT_UNAVAILABLE", "The store cannot take payments at the moment.")
	}
	if !idempotencyKeyPattern.MatchString(key) {
		return PlacedOrder{}, apperr.New(apperr.KindInvalid, "IDEMPOTENCY_KEY_REQUIRED", "Send a unique Idempotency-Key header with 16 to 100 characters.")
	}
	input, err := input.normalize()
	if err != nil {
		return PlacedOrder{}, err
	}

	var placed PlacedOrder
	err = s.in(ctx, func(tx pgx.Tx, q *sqlcgen.Queries, tenantID uuid.UUID) error {
		cart, err := s.findCart(ctx, q, tenantID, ref)
		if err != nil {
			return err
		}
		hash, err := requestHash(ref, input)
		if err != nil {
			return err
		}
		claimed, err := q.ClaimIdempotencyKey(ctx, sqlcgen.ClaimIdempotencyKeyParams{TenantID: tenantID, Scope: checkoutScope, Key: key, RequestHash: hash})
		if err != nil {
			return fmt.Errorf("claim idempotency key: %w", err)
		}
		if claimed == 0 {
			placed, err = s.replay(ctx, q, tenantID, key, hash)
			return err
		}

		if cart == nil {
			return apperr.New(apperr.KindUnprocessable, "CART_EMPTY", "The cart is empty.")
		}
		if _, err := q.LockCart(ctx, sqlcgen.LockCartParams{TenantID: tenantID, ID: cart.ID}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return apperr.New(apperr.KindConflict, "CART_ALREADY_USED", "This cart was already turned into an order.")
			}
			return err
		}
		summary, err := s.summarize(ctx, tx, q, tenantID, cart, SummaryRequest{
			ShippingMethodID: &input.ShippingMethodID, PostalCode: input.Address.PostalCode, CouponCode: input.CouponCode,
		})
		if err != nil {
			return err
		}
		switch {
		case len(summary.Cart.Lines) == 0:
			return apperr.New(apperr.KindUnprocessable, "CART_EMPTY", "The cart is empty.")
		case summary.Shipping == nil:
			return apperr.Validation(apperr.FieldError{Field: "shippingMethodId", Code: "invalid", Message: "Choose a delivery option."})
		case summary.Cart.HasUnavailable:
			return &ReviewError{Code: "OUT_OF_STOCK", Summary: summary}
		case summary.CouponError != "":
			return apperr.New(apperr.KindUnprocessable, summary.CouponError, "The coupon cannot be used.")
		case summary.Total != input.ExpectedTotal:
			return &ReviewError{Code: "PRICE_CHANGED", Summary: summary}
		}

		var coupon *sqlcgen.PromotionsCoupon
		if summary.Coupon != nil {
			row, discount, err := s.redeemCoupon(ctx, q, tenantID, summary.Coupon.Code, input.CustomerEmail, summary)
			if err != nil {
				return err
			}
			if discount != summary.Discount {
				return &ReviewError{Code: "PRICE_CHANGED", Summary: summary}
			}
			coupon = &row
		}
		placed, err = s.createOrder(ctx, tx, q, tenantID, ref, input, summary)
		if err != nil {
			return err
		}
		if coupon != nil {
			if err := recordRedemption(ctx, q, *coupon, placed.orderID, input.CustomerEmail, summary.Discount); err != nil {
				return err
			}
		}
		if err := q.SetCartStatus(ctx, sqlcgen.SetCartStatusParams{TenantID: tenantID, ID: cart.ID, Status: "converted"}); err != nil {
			return err
		}
		stored, err := json.Marshal(storedOrder{
			OrderID: placed.orderID, OrderNumber: placed.OrderNumber, Status: placed.Status, Total: placed.Total,
			Currency: placed.Currency, PaymentLabel: placed.PaymentLabel,
		})
		if err != nil {
			return err
		}
		return q.SaveIdempotencyResponse(ctx, sqlcgen.SaveIdempotencyResponseParams{
			TenantID: tenantID, Scope: checkoutScope, Key: key, Response: stored,
		})
	})
	if err != nil {
		var review *ReviewError
		if errors.As(err, &review) {
			observability.Count(ctx, "orders.checkout", attribute.String("outcome", strings.ToLower(review.Code)))
		}
		return PlacedOrder{}, err
	}
	observability.Count(ctx, "orders.checkout", attribute.String("outcome", "placed"))
	s.startCharge(ctx, placed)
	return placed, nil
}

func (s *Service) replay(ctx context.Context, q *sqlcgen.Queries, tenantID uuid.UUID, key string, hash []byte) (PlacedOrder, error) {
	row, err := q.GetIdempotencyKey(ctx, sqlcgen.GetIdempotencyKeyParams{TenantID: tenantID, Scope: checkoutScope, Key: key})
	if err != nil {
		return PlacedOrder{}, fmt.Errorf("load idempotency key: %w", err)
	}
	if string(row.RequestHash) != string(hash) {
		return PlacedOrder{}, apperr.New(apperr.KindUnprocessable, "IDEMPOTENCY_KEY_REUSED", "This key was already used for a different request.")
	}
	var stored storedOrder
	if row.Response == nil || json.Unmarshal(row.Response, &stored) != nil {
		return PlacedOrder{}, apperr.New(apperr.KindConflict, "CHECKOUT_IN_PROGRESS", "This order is still being processed.")
	}
	return PlacedOrder{
		OrderNumber: stored.OrderNumber, Status: stored.Status, Total: stored.Total, Currency: stored.Currency,
		PaymentLabel: stored.PaymentLabel, OrderToken: s.accessToken(tenantID, stored.OrderID), orderID: stored.OrderID,
	}, nil
}

func (s *Service) createOrder(ctx context.Context, tx pgx.Tx, q *sqlcgen.Queries, tenantID uuid.UUID, ref CartRef, input CheckoutInput, summary Summary) (PlacedOrder, error) {
	number, err := q.NextOrderNumber(ctx, tenantID)
	if err != nil {
		return PlacedOrder{}, fmt.Errorf("next order number: %w", err)
	}
	address, err := json.Marshal(input.Address)
	if err != nil {
		return PlacedOrder{}, err
	}
	orderID := newID()
	err = q.InsertOrder(ctx, sqlcgen.InsertOrderParams{
		TenantID: tenantID, ID: orderID, Number: number, CustomerUserID: ref.UserID, CustomerName: input.CustomerName,
		CustomerEmail: input.CustomerEmail, ShippingAddress: address, ShippingMethod: summary.Shipping.Name, StoreHost: input.StoreHost,
		DiscountAmount: summary.Discount, CouponCode: couponCode(summary),
		ShippingAmount: summary.ShippingAmount, SubtotalAmount: summary.Subtotal, TotalAmount: summary.Total,
		Currency: summary.Currency, PaymentDueAt: s.now().Add(s.paymentTTL),
	})
	if err != nil {
		return PlacedOrder{}, fmt.Errorf("insert order: %w", err)
	}

	lines := make([]inventory.Line, 0, len(summary.Cart.Lines))
	for _, line := range summary.Cart.Lines {
		if err := q.InsertOrderItem(ctx, sqlcgen.InsertOrderItemParams{
			TenantID: tenantID, ID: newID(), OrderID: orderID, VariantID: line.VariantID, ProductName: line.ProductName,
			VariantLabel: line.Label, Sku: line.SKU, Quantity: line.Quantity, UnitAmount: line.UnitPrice, TotalAmount: line.Total,
		}); err != nil {
			return PlacedOrder{}, fmt.Errorf("insert order item: %w", err)
		}
		lines = append(lines, inventory.Line{VariantID: line.VariantID, Quantity: line.Quantity})
	}
	if err := inventory.Reserve(ctx, tx, tenantID, orderID, lines, s.now().Add(s.paymentTTL)); err != nil {
		return PlacedOrder{}, err
	}

	actorType, actorID := "customer", "visitante"
	if ref.UserID != nil {
		actorID = ref.UserID.String()
	}
	if err := q.InsertStatusHistory(ctx, sqlcgen.InsertStatusHistoryParams{
		TenantID: tenantID, ID: newID(), OrderID: orderID, FromStatus: "", ToStatus: domain.PendingPayment,
		ActorType: actorType, ActorID: actorID, Note: "Pedido criado",
	}); err != nil {
		return PlacedOrder{}, fmt.Errorf("insert history: %w", err)
	}
	if err := q.InsertPayment(ctx, sqlcgen.InsertPaymentParams{
		TenantID: tenantID, ID: newID(), OrderID: orderID, Provider: s.checkoutProvider.Name(), Method: s.checkoutProvider.Name(),
		Amount: summary.Total, Currency: summary.Currency,
	}); err != nil {
		return PlacedOrder{}, fmt.Errorf("insert payment: %w", err)
	}
	if err := recordOrderEvent(ctx, tx, tenantID, orderID, EventOrderCreated); err != nil {
		return PlacedOrder{}, err
	}
	return PlacedOrder{
		OrderNumber: number, Status: domain.PendingPayment, Total: summary.Total, Currency: summary.Currency,
		OrderToken: s.accessToken(tenantID, orderID), PaymentLabel: s.checkoutProvider.Label(), orderID: orderID,
	}, nil
}

// startCharge asks the provider to create the charge after the order is
// committed, so a slow gateway never holds database locks (ADR-009). The
// payment ID is the idempotency key, and a payment that already has a
// reference is not charged again.
func (s *Service) startCharge(ctx context.Context, placed PlacedOrder) {
	var payment sqlcgen.PaymentsPayment
	err := s.in(ctx, func(_ pgx.Tx, q *sqlcgen.Queries, tenantID uuid.UUID) error {
		var err error
		payment, err = q.GetPaymentByOrder(ctx, sqlcgen.GetPaymentByOrderParams{TenantID: tenantID, OrderID: placed.orderID})
		return err
	})
	if err == nil && payment.ProviderReference != "" {
		return
	}
	var reference string
	if err == nil {
		provider, ok := s.providers[payment.Provider]
		if !ok {
			err = fmt.Errorf("payment provider %q is not configured", payment.Provider)
		} else {
			reference, err = provider.CreateCharge(ctx, payments.Charge{PaymentID: payment.ID, OrderID: placed.orderID, Amount: payment.Amount, Currency: payment.Currency})
		}
	}
	if err == nil {
		err = s.in(ctx, func(tx pgx.Tx, q *sqlcgen.Queries, tenantID uuid.UUID) error {
			payment, err := q.LockPaymentByOrder(ctx, sqlcgen.LockPaymentByOrderParams{TenantID: tenantID, OrderID: placed.orderID})
			if err != nil || payment.ProviderReference != "" {
				return err
			}
			if err := q.UpdatePayment(ctx, sqlcgen.UpdatePaymentParams{TenantID: tenantID, ID: payment.ID, Status: payment.Status, ProviderReference: reference}); err != nil {
				return err
			}
			return q.InsertPaymentTransaction(ctx, sqlcgen.InsertPaymentTransactionParams{
				TenantID: tenantID, ID: newID(), PaymentID: payment.ID, Kind: "charge", Amount: payment.Amount, ProviderReference: reference,
			})
		})
	}
	if err != nil {
		s.logger.ErrorContext(ctx, "payments.charge_failed", "order_number", placed.OrderNumber, "error", err)
	}
}
