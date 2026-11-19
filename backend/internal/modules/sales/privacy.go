package sales

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/modules/sales/internal/sqlcgen"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/authz"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/tenancy"
)

// CustomerData is what the sales module keeps about a customer account.
type CustomerData struct {
	Addresses []SavedAddress `json:"addresses"`
	Orders    []OrderView    `json:"orders"`
}

// ExportCustomer returns the addresses and orders of the signed-in customer.
func (s *Service) ExportCustomer(ctx context.Context, principal authz.Principal) (CustomerData, error) {
	data := CustomerData{Orders: []OrderView{}}
	addresses, err := s.Addresses(ctx, principal)
	if err != nil {
		return data, err
	}
	data.Addresses = addresses
	cursor := ""
	for range 50 {
		page, err := s.listOrders(ctx, OrderFilter{CustomerUserID: &principal.UserID, Cursor: cursor, Limit: 100})
		if err != nil {
			return data, err
		}
		data.Orders = append(data.Orders, page.Items...)
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
	}
	return data, nil
}

// EraseCustomer removes the addresses and cart of a customer and unlinks
// their orders from the account, inside the caller's transaction. Orders keep
// the name and e-mail they were placed with, as tax records.
func EraseCustomer(ctx context.Context, tx pgx.Tx, principal authz.Principal) error {
	tenantID, ok := tenancy.FromContext(ctx)
	if !ok || tenantID != principal.TenantID {
		return tenancy.ErrMissingTenant
	}
	q := sqlcgen.New(tx)
	if err := q.DeleteCustomerAddresses(ctx, sqlcgen.DeleteCustomerAddressesParams{TenantID: tenantID, UserID: principal.UserID}); err != nil {
		return fmt.Errorf("delete addresses: %w", err)
	}
	userID := principal.UserID
	if err := q.DeleteCustomerCartItems(ctx, sqlcgen.DeleteCustomerCartItemsParams{TenantID: tenantID, UserID: &userID}); err != nil {
		return fmt.Errorf("delete cart: %w", err)
	}
	if err := q.DetachCustomerCarts(ctx, sqlcgen.DetachCustomerCartsParams{TenantID: tenantID, UserID: &userID}); err != nil {
		return fmt.Errorf("detach carts: %w", err)
	}
	if err := q.DetachCustomerOrders(ctx, sqlcgen.DetachCustomerOrdersParams{TenantID: tenantID, CustomerUserID: &userID}); err != nil {
		return fmt.Errorf("detach orders: %w", err)
	}
	return nil
}
