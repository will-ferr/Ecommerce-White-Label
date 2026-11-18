package database

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/tenancy"
)

// TenantDB is the only way repositories reach tenant data. Every transaction
// is bound to the tenant in the context, and Row Level Security enforces it.
type TenantDB struct {
	pool *pgxpool.Pool
}

// NewTenantDB wraps a pool that connects with an unprivileged role.
func NewTenantDB(pool *pgxpool.Pool) *TenantDB {
	return &TenantDB{pool: pool}
}

// Run executes fn in a transaction scoped to the tenant in ctx. The setting is
// local to the transaction, so it never leaks to the next use of the connection.
func (db *TenantDB) Run(ctx context.Context, fn func(pgx.Tx) error) error {
	tenantID, ok := tenancy.FromContext(ctx)
	if !ok {
		return tenancy.ErrMissingTenant
	}
	return pgx.BeginFunc(ctx, db.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, tenantID.String()); err != nil {
			return fmt.Errorf("set tenant context: %w", err)
		}
		return fn(tx)
	})
}
