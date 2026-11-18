//go:build integration

// Package integration exercises the platform against real dependencies.
package integration

import (
	"context"
	"errors"
	"net/url"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/config"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/database"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/security"
)

const (
	testDatabase      = "ecommerce_test"
	superuserPassword = "test-superuser"
	ownerPassword     = "test-owner"
	runtimePassword   = "test-runtime"
	postgresImage     = "postgres:18.7-alpine"
)

type environment struct {
	host string
	port int
}

func (e environment) database(user, password string) config.Database {
	return config.Database{
		Host: e.host, Port: e.port, Name: testDatabase, User: user,
		Password: security.NewSecret(password), SSLMode: "disable", MaxConns: 5, MinConns: 0,
	}
}

func startPostgres(ctx context.Context, t *testing.T) environment {
	t.Helper()
	container, err := postgres.Run(ctx, postgresImage,
		postgres.WithDatabase(testDatabase),
		postgres.WithUsername("postgres"),
		postgres.WithPassword(superuserPassword),
		postgres.WithInitScripts(filepath.Join("..", "..", "..", "deployments", "compose", "postgres", "init", "01-roles.sh")),
		testcontainers.WithEnv(map[string]string{
			"MIGRATION_DB_PASSWORD": ownerPassword,
			"DB_PASSWORD":           runtimePassword,
		}),
		postgres.BasicWaitStrategies(),
	)
	testcontainers.CleanupContainer(t, container)
	if err != nil {
		t.Fatalf("start postgres: %v", err)
	}

	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("connection string: %v", err)
	}
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse connection string: %v", err)
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil {
		t.Fatalf("parse port: %v", err)
	}
	return environment{host: parsed.Hostname(), port: port}
}

func TestDatabaseFoundation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	env := startPostgres(ctx, t)
	owner := env.database("app_owner", ownerPassword)
	runtime := env.database("app_runtime", runtimePassword)

	if err := database.Migrate(ctx, owner, database.MigrateUp); err != nil {
		t.Fatalf("migrate up: %v", err)
	}

	t.Run("migrations are idempotent", func(t *testing.T) {
		if err := database.Migrate(ctx, owner, database.MigrateUp); err != nil {
			t.Errorf("second migrate up: %v", err)
		}
	})

	t.Run("runtime role is accepted", func(t *testing.T) {
		pool, err := database.Connect(ctx, runtime)
		if err != nil {
			t.Fatalf("Connect() error = %v", err)
		}
		pool.Close()
	})

	t.Run("superuser is refused", func(t *testing.T) {
		_, err := database.Connect(ctx, env.database("postgres", superuserPassword))
		if !errors.Is(err, database.ErrPrivilegedRole) {
			t.Errorf("Connect() error = %v, want ErrPrivilegedRole", err)
		}
	})

	t.Run("runtime role has safety timeouts", func(t *testing.T) {
		pool := connect(ctx, t, runtime)
		for setting, want := range map[string]string{
			"statement_timeout":                   "10s",
			"lock_timeout":                        "3s",
			"idle_in_transaction_session_timeout": "15s",
		} {
			var got string
			if err := pool.QueryRow(ctx, "SELECT current_setting($1)", setting).Scan(&got); err != nil {
				t.Fatalf("read %s: %v", setting, err)
			}
			if got != want {
				t.Errorf("%s = %s, want %s", setting, got, want)
			}
		}
	})

	t.Run("runtime role cannot change the schema", func(t *testing.T) {
		pool := connect(ctx, t, runtime)
		for _, statement := range []string{
			"CREATE TABLE public.intruder (id int)",
			"CREATE TABLE app.intruder (id int)",
			"CREATE SCHEMA intruder",
			"DROP FUNCTION app.current_tenant_id()",
		} {
			if _, err := pool.Exec(ctx, statement); err == nil {
				t.Errorf("statement was allowed: %s", statement)
			}
		}
	})

	t.Run("tenant function fails closed", func(t *testing.T) {
		pool := connect(ctx, t, runtime)
		var tenant *string
		if err := pool.QueryRow(ctx, "SELECT app.current_tenant_id()::text").Scan(&tenant); err != nil {
			t.Fatalf("query: %v", err)
		}
		if tenant != nil {
			t.Errorf("tenant = %s, want NULL", *tenant)
		}
	})

	t.Run("migrations can be reverted and reapplied", func(t *testing.T) {
		if err := database.Migrate(ctx, owner, database.MigrateDown); err != nil {
			t.Fatalf("migrate down: %v", err)
		}
		if err := database.Migrate(ctx, owner, database.MigrateUp); err != nil {
			t.Errorf("migrate up after down: %v", err)
		}
	})
}

func connect(ctx context.Context, t *testing.T, cfg config.Database) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(ctx, cfg.DSN())
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}
