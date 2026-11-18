// Command migrate applies versioned database migrations.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/config"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/database"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	command := database.MigrateUp
	if len(os.Args) > 1 {
		command = os.Args[1]
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg, err := config.LoadMigration(os.LookupEnv)
	if err != nil {
		logger.Error("migrate.config_invalid", "error", err)
		stop()
		os.Exit(1) //nolint:gocritic // stop is called explicitly above
	}
	if err := database.Migrate(ctx, cfg, command); err != nil {
		logger.Error("migrate.failed", "command", command, "error", err)
		stop()
		os.Exit(1)
	}
	logger.Info("migrate.completed", "command", command)
}
