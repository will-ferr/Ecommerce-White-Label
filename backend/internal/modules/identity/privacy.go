package identity

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/modules/identity/internal/app"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/authz"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/shared/pagination"
)

// AccountData is the personal data kept about a customer account.
type AccountData = app.AccountData

// CustomerSummary is a customer account as the store team sees it.
type CustomerSummary = app.CustomerSummary

// ExportAccount returns the personal data of the signed-in customer.
func (m *Module) ExportAccount(ctx context.Context, principal authz.Principal) (AccountData, error) {
	return m.service.ExportAccount(ctx, principal)
}

// EraseAccount removes the personal data of the signed-in customer. The hook
// erases the data of other modules in the same transaction.
func (m *Module) EraseAccount(ctx context.Context, principal authz.Principal, password string, hook func(context.Context, pgx.Tx) error) error {
	return m.service.EraseAccount(ctx, principal, password, hook)
}

// Customers lists the customer accounts of the store of the principal.
func (m *Module) Customers(ctx context.Context, principal authz.Principal, search, cursor string) (pagination.Page[CustomerSummary], error) {
	return m.service.Customers(ctx, principal, search, cursor)
}
