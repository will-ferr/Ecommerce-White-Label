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
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/ratelimit"
)

const healthCheckTimeout = 2 * time.Second

// Run starts the store API (storefront and store admin) and blocks until ctx
// is canceled and shutdown finishes.
func Run(ctx context.Context, cfg config.Config, version string, logger *slog.Logger) error {
	return serve(ctx, cfg, nil, version, logger)
}

// RunPlatform starts the platform API, which serves only the console routes
// and is the only process with the platform database role (ADR-019).
func RunPlatform(ctx context.Context, cfg config.Config, platform config.PlatformAPI, version string, logger *slog.Logger) error {
	return serve(ctx, cfg, &platform, version, logger)
}

func serve(ctx context.Context, cfg config.Config, platform *config.PlatformAPI, version string, logger *slog.Logger) error {
	pool, err := database.Connect(ctx, cfg.Database)
	if err != nil {
		return fmt.Errorf("connect to database: %w", err)
	}
	defer pool.Close()
	checks := []health.Check{{Name: "postgres", Critical: true, Probe: pool.Ping}}

	deps := ModuleDeps{Pool: pool, Logger: logger, Auth: cfg.Auth, Commerce: cfg.Commerce}
	if platform != nil {
		platformPool, err := database.Connect(ctx, platform.Database)
		if err != nil {
			return fmt.Errorf("connect to database as platform: %w", err)
		}
		defer platformPool.Close()
		deps.PlatformPool, deps.BaseDomain = platformPool, platform.BaseDomain
		checks = append(checks, health.Check{Name: "postgres-platform", Critical: true, Probe: platformPool.Ping})
	}

	redisClient, err := cache.Connect(cfg.Redis)
	if err != nil {
		return fmt.Errorf("connect to redis: %w", err)
	}
	defer func() {
		if closeErr := redisClient.Close(); closeErr != nil {
			logger.Warn("redis.close_failed", "error", closeErr)
		}
	}()

	var limiter *ratelimit.Limiter
	if cfg.Auth.RateLimitEnabled {
		limiter = ratelimit.New(redisClient, logger)
	}
	// E-mail, events and scheduled jobs run in the worker process.
	deps.Limiter = limiter
	modules, err := BuildModules(deps)
	if err != nil {
		return fmt.Errorf("register modules: %w", err)
	}
	register := modules.Register
	if platform != nil {
		register = modules.RegisterPlatform
	}

	checks = append(checks, health.Check{Name: "redis", Probe: func(ctx context.Context) error { return cache.Ping(ctx, redisClient) }})
	healthService := health.NewService(logger, version, healthCheckTimeout, checks...)

	server := &http.Server{
		Addr: cfg.HTTP.Addr,
		Handler: NewRouter(RouterDeps{
			Logger:         logger,
			Health:         healthService,
			RequestTimeout: cfg.HTTP.RequestTimeout,
			MaxBodyBytes:   cfg.HTTP.MaxBodyBytes,
			HSTS:           cfg.Environment.IsHardened(),
			TrustedProxies: cfg.Auth.TrustedProxies,
			RegisterAPI:    register,
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
