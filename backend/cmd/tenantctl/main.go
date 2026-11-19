// Command tenantctl lets platform operators manage stores.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	identitycli "github.com/will-ferr/Ecommerce-White-Label/backend/internal/modules/identity/cli"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/modules/tenants/cli"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/config"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/database"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/shared/apperr"
)

func main() {
	if err := run(); err != nil {
		if !errors.Is(err, cli.ErrNotFound) {
			fmt.Fprintln(os.Stderr, describe(err))
		}
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg, err := config.LoadPlatformTool(os.LookupEnv)
	if err != nil {
		return err
	}
	pool, err := database.Connect(ctx, cfg.Database)
	if err != nil {
		return err
	}
	defer pool.Close()

	args := os.Args[1:]
	if identitycli.Handles(args) {
		return identitycli.NewRunner(pool, cfg.AdminURL, cfg.ConsoleURL, os.Stdout).Run(ctx, args)
	}
	return cli.NewRunner(pool, cfg.BaseDomain, os.Stdout).Run(ctx, args)
}

func describe(err error) string {
	appErr, ok := apperr.As(err)
	if !ok {
		return err.Error()
	}
	message := appErr.Code + ": " + appErr.Message
	for _, field := range appErr.Fields {
		message += fmt.Sprintf("\n  %s: %s", field.Field, field.Message)
	}
	return message
}
