// Command platform serves the platform console API. It is the only process
// that holds the platform database role (ADR-019).
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/app"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/config"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/observability"
)

// version is set at build time.
var version = "dev"

const telemetryFlushTimeout = 5 * time.Second

func main() {
	healthcheck := flag.Bool("healthcheck", false, "probe the local server and exit")
	flag.Parse()

	if *healthcheck {
		if err := probe(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if err := run(); err != nil {
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg, platform, err := config.LoadPlatformAPI(os.LookupEnv)
	if err != nil {
		slog.New(slog.NewJSONHandler(os.Stderr, nil)).Error("config.invalid", "error", err)
		return err
	}

	telemetry, err := observability.Setup(ctx, cfg, version)
	if err != nil {
		slog.New(slog.NewJSONHandler(os.Stderr, nil)).Error("telemetry.setup_failed", "error", err)
		return err
	}
	logger := observability.NewLogger(os.Stdout, cfg.LogLevel, cfg.ServiceName, string(cfg.Environment), version, telemetry.LogHandler())
	slog.SetDefault(logger)

	runErr := app.RunPlatform(ctx, cfg, platform, version, logger)
	if runErr != nil {
		logger.Error("platform.stopped", "error", runErr)
	}

	flushCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), telemetryFlushTimeout)
	defer cancel()
	return errors.Join(runErr, telemetry.Shutdown(flushCtx))
}

// probe supports container health checks in images that have no shell or curl.
func probe() error {
	addr, ok := os.LookupEnv("HTTP_ADDR")
	if !ok || addr == "" {
		addr = ":8080"
	}
	if strings.HasPrefix(addr, ":") {
		addr = "127.0.0.1" + addr
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+addr+"/health/live", http.NoBody)
	if err != nil {
		return fmt.Errorf("build health request: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("probe health endpoint: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health endpoint returned %d", resp.StatusCode)
	}
	return nil
}
