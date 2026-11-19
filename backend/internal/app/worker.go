package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/config"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/database"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/health"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/mail"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/messaging"
)

// purgeInterval is how often old outbox and inbox rows are removed.
const purgeInterval = time.Hour

// RunWorker publishes events, consumes them, sends e-mail and runs scheduled
// jobs until ctx is canceled. Several workers can run at once: every task
// either locks its rows or holds an advisory lock.
func RunWorker(ctx context.Context, cfg config.Config, workerCfg config.Worker, version string, logger *slog.Logger) error {
	pool, err := database.Connect(ctx, cfg.Database)
	if err != nil {
		return fmt.Errorf("connect to database: %w", err)
	}
	defer pool.Close()

	broker, err := messaging.Dial(workerCfg.RabbitMQ.URL(), logger)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := broker.Close(); closeErr != nil {
			logger.Warn("rabbitmq.close_failed", "error", closeErr)
		}
	}()

	modules, err := BuildModules(ModuleDeps{Pool: pool, Logger: logger, Auth: cfg.Auth, Commerce: cfg.Commerce})
	if err != nil {
		return fmt.Errorf("register modules: %w", err)
	}
	sealer, err := mail.NewSealer(cfg.Auth.MFAKey)
	if err != nil {
		return err
	}

	healthService := health.NewService(logger, version, healthCheckTimeout,
		health.Check{Name: "postgres", Critical: true, Probe: pool.Ping},
		health.Check{Name: "rabbitmq", Critical: true, Probe: broker.Ping},
	)
	router := chi.NewRouter()
	router.Get("/health/live", healthService.Live)
	router.Get("/health/ready", healthService.Ready)
	server := &http.Server{
		Addr: workerCfg.HealthAddr, Handler: router, ReadHeaderTimeout: 5 * time.Second,
		BaseContext: func(net.Listener) context.Context { return context.WithoutCancel(ctx) },
	}

	ctx, stop := context.WithCancel(ctx)
	defer stop()
	var wg sync.WaitGroup
	wg.Go(func() { messaging.NewRelay(pool, broker, logger, workerCfg.RelayInterval).Run(ctx) })
	for _, subscription := range modules.Subscriptions {
		consumer := messaging.NewConsumer(pool, broker, logger, subscription, nil)
		wg.Go(func() { consumer.Run(ctx) })
	}
	wg.Go(func() {
		mail.NewDispatcher(pool, mail.NewSMTPSender(cfg.Auth.SMTP), sealer, logger).Run(ctx, workerCfg.MailInterval)
	})
	wg.Go(func() { modules.Sales.Service().RunExpiry(ctx, cfg.Commerce.ExpiryInterval) })
	wg.Go(func() { runPurge(ctx, pool, workerCfg.MessagingRetention, logger) })

	serveErr := make(chan error, 1)
	go func() {
		logger.Info("worker.started", "health_addr", workerCfg.HealthAddr, "consumers", len(modules.Subscriptions))
		serveErr <- server.ListenAndServe()
	}()

	var runErr error
	select {
	case err := <-serveErr:
		runErr = fmt.Errorf("health server stopped: %w", err)
	case <-ctx.Done():
	}
	stop()
	healthService.Drain()
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cfg.HTTP.ShutdownTimeout)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		runErr = errors.Join(runErr, err)
	}
	wg.Wait()
	logger.Info("worker.stopped")
	return runErr
}

// runPurge removes old outbox and inbox rows every hour. The deletion is
// idempotent, so concurrent workers are harmless.
func runPurge(ctx context.Context, pool *pgxpool.Pool, retention time.Duration, logger *slog.Logger) {
	ticker := time.NewTicker(purgeInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			removed, err := messaging.Purge(ctx, pool, retention)
			if err != nil {
				logger.ErrorContext(ctx, "messaging.purge_failed", "error", err)
			} else if removed > 0 {
				logger.InfoContext(ctx, "messaging.purged", "rows", removed)
			}
		}
	}
}
