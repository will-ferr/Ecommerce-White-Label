package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/cache"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/config"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/database"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/health"
)

const healthCheckTimeout = 2 * time.Second

// Run starts the API and blocks until ctx is canceled and shutdown finishes.
func Run(ctx context.Context, cfg config.Config, version string, logger *slog.Logger) error {
	pool, err := database.Connect(ctx, cfg.Database)
	if err != nil {
		return fmt.Errorf("connect to database: %w", err)
	}
	defer pool.Close()

	redisClient, err := cache.Connect(cfg.Redis)
	if err != nil {
		return fmt.Errorf("connect to redis: %w", err)
	}
	defer func() {
		if closeErr := redisClient.Close(); closeErr != nil {
			logger.Warn("redis.close_failed", "error", closeErr)
		}
	}()

	healthService := health.NewService(logger, version, healthCheckTimeout,
		health.Check{Name: "postgres", Critical: true, Probe: pool.Ping},
		health.Check{Name: "redis", Probe: func(ctx context.Context) error { return cache.Ping(ctx, redisClient) }},
	)

	server := &http.Server{
		Addr: cfg.HTTP.Addr,
		Handler: NewRouter(RouterDeps{
			Logger:         logger,
			Health:         healthService,
			RequestTimeout: cfg.HTTP.RequestTimeout,
			MaxBodyBytes:   cfg.HTTP.MaxBodyBytes,
			HSTS:           cfg.Environment.IsHardened(),
		}),
		ReadHeaderTimeout: cfg.HTTP.ReadHeaderTimeout,
		ReadTimeout:       cfg.HTTP.ReadTimeout,
		WriteTimeout:      cfg.HTTP.WriteTimeout,
		IdleTimeout:       cfg.HTTP.IdleTimeout,
		MaxHeaderBytes:    1 << 16,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelWarn),
		BaseContext:       func(net.Listener) context.Context { return context.WithoutCancel(ctx) },
	}

	serveErr := make(chan error, 1)
	go func() {
		logger.Info("http.server.started", "addr", cfg.HTTP.Addr)
		serveErr <- server.ListenAndServe()
	}()

	select {
	case err := <-serveErr:
		return fmt.Errorf("http server stopped: %w", err)
	case <-ctx.Done():
	}

	logger.Info("http.server.draining")
	healthService.Drain()
	time.Sleep(cfg.HTTP.DrainDelay)

	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cfg.HTTP.ShutdownTimeout)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shut down http server: %w", err)
	}
	if err := <-serveErr; !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("http server stopped: %w", err)
	}
	logger.Info("http.server.stopped")
	return nil
}
