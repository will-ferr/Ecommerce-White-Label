package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/config"
	"github.com/will-ferr/Ecommerce-White-Label/backend/migrations"
)

// Migration commands.
const (
	MigrateUp     = "up"
	MigrateDown   = "down"
	MigrateStatus = "status"
)

// Migrate applies a migration command using the privileged migration role.
func Migrate(ctx context.Context, cfg config.Database, command string) error {
	connConfig, err := pgx.ParseConfig(cfg.DSN())
	if err != nil {
		// The parser error may echo the connection string, so it is not wrapped.
		return errors.New("parse migration configuration: invalid connection settings")
	}
	db := stdlib.OpenDB(*connConfig)
	defer func() { _ = db.Close() }()

	provider, err := goose.NewProvider(goose.DialectPostgres, db, migrations.FS)
	if err != nil {
		return fmt.Errorf("create migration provider: %w", err)
	}
	return run(ctx, provider, db, command)
}

func run(ctx context.Context, provider *goose.Provider, db *sql.DB, command string) error {
	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("connect to database: %w", err)
	}
	var err error
	switch command {
	case MigrateUp:
		_, err = provider.Up(ctx)
	case MigrateDown:
		_, err = provider.Down(ctx)
	case MigrateStatus:
		_, err = provider.Status(ctx)
	default:
		return fmt.Errorf("unknown migration command %q", command)
	}
	if err != nil {
		return fmt.Errorf("run migration command %s: %w", command, err)
	}
	return nil
}
