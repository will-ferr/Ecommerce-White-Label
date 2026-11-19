package tenants

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/modules/tenants/internal/adapters/postgres/sqlcgen"
)

// StoreCurrency returns the currency of a store, inside the transaction of the caller.
func StoreCurrency(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID) (string, error) {
	settings, err := sqlcgen.New(tx).GetSettings(ctx, tenantID)
	if err != nil {
		return "", fmt.Errorf("load store currency: %w", err)
	}
	return settings.Currency, nil
}
