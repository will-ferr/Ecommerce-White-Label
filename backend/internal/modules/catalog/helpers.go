package catalog

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/modules/catalog/internal/sqlcgen"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/shared/apperr"
)

type sqlcgenQueries = sqlcgen.Queries

// requireVariant confirms that a variant belongs to the store, so stock is
// never created for an identifier invented by the caller.
func (s *Service) requireVariant(ctx context.Context, tx pgx.Tx, tenantID, variantID uuid.UUID) error {
	exists, err := sqlcgen.New(tx).VariantExists(ctx, sqlcgen.VariantExistsParams{TenantID: tenantID, ID: variantID})
	if err != nil {
		return err
	}
	if !exists {
		return apperr.New(apperr.KindNotFound, "VARIANT_NOT_FOUND", "The variant does not exist.")
	}
	return nil
}
