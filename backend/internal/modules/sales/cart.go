package sales

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/modules/catalog"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/modules/inventory"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/modules/sales/internal/sqlcgen"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/modules/tenants"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/security"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/shared/apperr"
)

const (
	maxCartLines    = 50
	maxLineQuantity = 99
)

// CartRef identifies the cart of a request: the account of a signed-in
// customer, or the token kept in a cookie for a visitor.
type CartRef struct {
	UserID *uuid.UUID
	Token  string
}

// CartLine is one item of the cart, priced and checked against stock.
type CartLine struct {
	VariantID   uuid.UUID `json:"variantId"`
	ProductName string    `json:"productName"`
	ProductSlug string    `json:"productSlug"`
	Label       string    `json:"label"`
	SKU         string    `json:"sku"`
	Quantity    int32     `json:"quantity"`
	UnitPrice   int64     `json:"unitPrice"`
	Total       int64     `json:"total"`
	Available   bool      `json:"available"`

	weightGrams int32
}

// CartView is the cart as shown to the buyer. Prices always come from the catalog.
type CartView struct {
	Lines          []CartLine `json:"lines"`
	ItemCount      int32      `json:"itemCount"`
	Subtotal       int64      `json:"subtotal"`
	Currency       string     `json:"currency"`
	HasUnavailable bool       `json:"hasUnavailable"`
}

func (s *Service) findCart(ctx context.Context, q *sqlcgen.Queries, tenantID uuid.UUID, ref CartRef) (*sqlcgen.CartCart, error) {
	var cart sqlcgen.CartCart
	var err error
	switch {
	case ref.UserID != nil:
		cart, err = q.GetActiveCartByUser(ctx, sqlcgen.GetActiveCartByUserParams{TenantID: tenantID, UserID: ref.UserID})
	case ref.Token != "":
		cart, err = q.GetActiveCartByToken(ctx, sqlcgen.GetActiveCartByTokenParams{TenantID: tenantID, TokenHash: security.HashToken(ref.Token)})
	default:
		return nil, nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load cart: %w", err)
	}
	return &cart, nil
}

// ensureCart returns the cart of the request, creating it when needed. A new
// visitor cart comes with the token that must be stored in the cookie.
func (s *Service) ensureCart(ctx context.Context, q *sqlcgen.Queries, tenantID uuid.UUID, ref CartRef) (sqlcgen.CartCart, string, error) {
	cart, err := s.findCart(ctx, q, tenantID, ref)
	if err != nil || cart != nil {
		if cart == nil {
			return sqlcgen.CartCart{}, "", err
		}
		return *cart, "", nil
	}
	params := sqlcgen.InsertCartParams{TenantID: tenantID, ID: newID(), UserID: ref.UserID}
	token := ""
	if ref.UserID == nil {
		if token, err = security.NewToken(); err != nil {
			return sqlcgen.CartCart{}, "", err
		}
		params.TokenHash = security.HashToken(token)
	}
	if err := q.InsertCart(ctx, params); err != nil {
		return sqlcgen.CartCart{}, "", fmt.Errorf("create cart: %w", err)
	}
	return sqlcgen.CartCart{TenantID: tenantID, ID: params.ID, UserID: ref.UserID, Status: "active"}, token, nil
}

func (s *Service) buildView(ctx context.Context, tx pgx.Tx, q *sqlcgen.Queries, tenantID uuid.UUID, cart *sqlcgen.CartCart) (CartView, error) {
	currency, err := tenants.StoreCurrency(ctx, tx, tenantID)
	if err != nil {
		return CartView{}, err
	}
	view := CartView{Lines: []CartLine{}, Currency: currency}
	if cart == nil {
		return view, nil
	}
	items, err := q.ListCartItems(ctx, sqlcgen.ListCartItemsParams{TenantID: tenantID, CartID: cart.ID})
	if err != nil {
		return CartView{}, fmt.Errorf("list cart items: %w", err)
	}
	ids := make([]uuid.UUID, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.VariantID)
	}
	variants, err := catalog.VariantsForSale(ctx, tx, tenantID, ids)
	if err != nil {
		return CartView{}, err
	}
	stock, err := inventory.Levels(ctx, tx, tenantID, ids)
	if err != nil {
		return CartView{}, err
	}
	for _, item := range items {
		variant, sellable := variants[item.VariantID]
		line := CartLine{VariantID: item.VariantID, Quantity: item.Quantity}
		if sellable {
			line.ProductName, line.ProductSlug, line.Label, line.SKU = variant.ProductName, variant.ProductSlug, variant.Label, variant.SKU
			line.UnitPrice, line.Total = variant.Price, variant.Price*int64(item.Quantity)
			line.weightGrams = variant.WeightGrams * item.Quantity
			line.Available = stock[item.VariantID].Available >= item.Quantity
		} else {
			line.ProductName = "Produto indisponível"
		}
		if line.Available {
			view.Subtotal += line.Total
			view.ItemCount += line.Quantity
		} else {
			view.HasUnavailable = true
		}
		view.Lines = append(view.Lines, line)
	}
	return view, nil
}

// Cart returns the cart of the request.
func (s *Service) Cart(ctx context.Context, ref CartRef) (CartView, error) {
	var view CartView
	err := s.in(ctx, func(tx pgx.Tx, q *sqlcgen.Queries, tenantID uuid.UUID) error {
		cart, err := s.findCart(ctx, q, tenantID, ref)
		if err != nil {
			return err
		}
		view, err = s.buildView(ctx, tx, q, tenantID, cart)
		return err
	})
	return view, err
}

func validQuantity(quantity int32, allowZero bool) error {
	if quantity > maxLineQuantity || quantity < 0 || (quantity == 0 && !allowZero) {
		return apperr.Validation(apperr.FieldError{Field: "quantity", Code: "invalid", Message: "Choose from 1 to 99 units."})
	}
	return nil
}

// ChangeItem adds units of a variant, or sets its quantity when replace is true.
// It returns the token of a new visitor cart, if one was created.
func (s *Service) ChangeItem(ctx context.Context, ref CartRef, variantID uuid.UUID, quantity int32, replace bool) (CartView, string, error) {
	if err := validQuantity(quantity, replace); err != nil {
		return CartView{}, "", err
	}
	var view CartView
	var token string
	err := s.in(ctx, func(tx pgx.Tx, q *sqlcgen.Queries, tenantID uuid.UUID) error {
		var cart sqlcgen.CartCart
		var err error
		cart, token, err = s.ensureCart(ctx, q, tenantID, ref)
		if err != nil {
			return err
		}
		switch {
		case replace && quantity == 0:
			err = q.DeleteCartItem(ctx, sqlcgen.DeleteCartItemParams{TenantID: tenantID, CartID: cart.ID, VariantID: variantID})
		case replace:
			_, err = q.SetCartItem(ctx, sqlcgen.SetCartItemParams{TenantID: tenantID, CartID: cart.ID, VariantID: variantID, Quantity: quantity})
		default:
			variants, lookupErr := catalog.VariantsForSale(ctx, tx, tenantID, []uuid.UUID{variantID})
			if lookupErr != nil {
				return lookupErr
			}
			if _, ok := variants[variantID]; !ok {
				return apperr.New(apperr.KindNotFound, "VARIANT_NOT_AVAILABLE", "This product is not available.")
			}
			lines, countErr := q.CountCartLines(ctx, sqlcgen.CountCartLinesParams{TenantID: tenantID, CartID: cart.ID})
			if countErr != nil {
				return countErr
			}
			if lines >= maxCartLines {
				return apperr.New(apperr.KindUnprocessable, "CART_FULL", "The cart can hold up to 50 different products.")
			}
			err = q.AddCartItem(ctx, sqlcgen.AddCartItemParams{TenantID: tenantID, CartID: cart.ID, VariantID: variantID, Quantity: quantity})
		}
		if err != nil {
			return fmt.Errorf("change cart item: %w", err)
		}
		if err := q.TouchCart(ctx, sqlcgen.TouchCartParams{TenantID: tenantID, ID: cart.ID}); err != nil {
			return err
		}
		view, err = s.buildView(ctx, tx, q, tenantID, &cart)
		return err
	})
	return view, token, err
}

// MergeVisitorCart moves the items of a visitor cart into the cart of the
// customer who just signed in.
func (s *Service) MergeVisitorCart(ctx context.Context, userID uuid.UUID, token string) (CartView, error) {
	var view CartView
	err := s.in(ctx, func(tx pgx.Tx, q *sqlcgen.Queries, tenantID uuid.UUID) error {
		target, _, err := s.ensureCart(ctx, q, tenantID, CartRef{UserID: &userID})
		if err != nil {
			return err
		}
		visitor, err := s.findCart(ctx, q, tenantID, CartRef{Token: token})
		if err != nil {
			return err
		}
		if visitor != nil && visitor.ID != target.ID {
			items, err := q.ListCartItems(ctx, sqlcgen.ListCartItemsParams{TenantID: tenantID, CartID: visitor.ID})
			if err != nil {
				return err
			}
			for _, item := range items {
				if err := q.AddCartItem(ctx, sqlcgen.AddCartItemParams{
					TenantID: tenantID, CartID: target.ID, VariantID: item.VariantID, Quantity: item.Quantity,
				}); err != nil {
					return fmt.Errorf("merge cart item: %w", err)
				}
			}
			if err := q.SetCartStatus(ctx, sqlcgen.SetCartStatusParams{TenantID: tenantID, ID: visitor.ID, Status: "merged"}); err != nil {
				return err
			}
		}
		view, err = s.buildView(ctx, tx, q, tenantID, &target)
		return err
	})
	return view, err
}
