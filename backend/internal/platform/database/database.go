// Package database manages the PostgreSQL connection pool and migrations.
package database

import (
	"context"
	"errors"
	"fmt"

	"github.com/exaring/otelpgx"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/config"
)

// ErrPrivilegedRole means the API connected with a role able to bypass RLS.
var ErrPrivilegedRole = errors.New("database role must not be superuser or have BYPASSRLS")

// Connect opens an instrumented pool and confirms that it is safe to use.
func Connect(ctx context.Context, cfg config.Database) (*pgxpool.Pool, error) {
	poolConfig, err := pgxpool.ParseConfig(cfg.DSN())
	if err != nil {
		// The parser error may echo the connection string, so it is not wrapped.
		return nil, errors.New("parse database configuration: invalid connection settings")
	}
	poolConfig.MaxConns = cfg.MaxConns
	poolConfig.MinConns = cfg.MinConns
	poolConfig.ConnConfig.Tracer = otelpgx.NewTracer()

	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("create database pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	if err := AssertUnprivilegedRole(ctx, pool); err != nil {
		pool.Close()
		return nil, err
	}
	if err := otelpgx.RecordStats(pool); err != nil {
		pool.Close()
		return nil, fmt.Errorf("record database pool metrics: %w", err)
	}
	return pool, nil
}

// AssertUnprivilegedRole stops the API from running with a role that would
// silently ignore Row Level Security.
func AssertUnprivilegedRole(ctx context.Context, pool *pgxpool.Pool) error {
	var superuser, bypassRLS bool
	err := pool.QueryRow(ctx,
		`SELECT rolsuper, rolbypassrls FROM pg_catalog.pg_roles WHERE rolname = current_user`,
	).Scan(&superuser, &bypassRLS)
	if err != nil {
		return fmt.Errorf("inspect database role: %w", err)
	}
	if superuser || bypassRLS {
		return ErrPrivilegedRole
	}
	return nil
}
