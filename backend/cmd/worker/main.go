// Command worker publishes domain events, consumes them, sends e-mail and runs
// scheduled jobs. It also inspects and replays dead letter queues:
//
//	worker dlq list --consumer sales-notifications
//	worker dlq replay --consumer sales-notifications --limit 10 --reason "SMTP corrigido"
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/app"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/modules/audit"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/config"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/database"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/messaging"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/observability"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/tenancy"
)

// version is set at build time.
var version = "dev"

const telemetryFlushTimeout = 5 * time.Second

const usage = `Uso:
  worker                         executa o worker
  worker -healthcheck            verifica o worker local
  worker dlq list --consumer NOME [--limit N]
  worker dlq replay --consumer NOME --reason TEXTO [--limit N]`

var errUsage = errors.New("invalid command\n" + usage)

func main() {
	args := os.Args[1:]
	var err error
	switch {
	case len(args) == 1 && args[0] == "-healthcheck":
		err = probe()
	case len(args) > 0 && args[0] == "dlq":
		err = deadLetters(args[1:], os.Stdout)
	case len(args) == 0:
		err = run()
	default:
		err = errUsage
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg, workerCfg, err := config.LoadWorker(os.LookupEnv)
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

	runErr := app.RunWorker(ctx, cfg, workerCfg, version, logger)
	if runErr != nil {
		logger.Error("worker.failed", "error", runErr)
	}
	flushCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), telemetryFlushTimeout)
	defer cancel()
	return errors.Join(runErr, telemetry.Shutdown(flushCtx))
}

// deadLetters lists or replays the dead letter queue of a consumer. A replay
// is recorded in the audit trail of the platform.
func deadLetters(args []string, out io.Writer) error {
	if len(args) == 0 || (args[0] != "list" && args[0] != "replay") {
		return errUsage
	}
	action := args[0]
	flags := flag.NewFlagSet("dlq "+action, flag.ContinueOnError)
	consumer := flags.String("consumer", "", "consumer name")
	limit := flags.Int("limit", 20, "maximum number of messages")
	reason := flags.String("reason", "", "why the messages are replayed")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if *limit < 1 || *limit > 1000 {
		return errors.New("--limit must be between 1 and 1000")
	}
	if action == "replay" && strings.TrimSpace(*reason) == "" {
		return errors.New("--reason is required to replay messages")
	}

	ctx := context.Background()
	cfg, workerCfg, err := config.LoadWorker(os.LookupEnv)
	if err != nil {
		return err
	}
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	pool, err := database.Connect(ctx, cfg.Database)
	if err != nil {
		return err
	}
	defer pool.Close()
	modules, err := app.BuildModules(app.ModuleDeps{Pool: pool, Logger: logger, Auth: cfg.Auth, Commerce: cfg.Commerce})
	if err != nil {
		return err
	}
	names := []string{}
	for _, subscription := range modules.Subscriptions {
		names = append(names, subscription.Name)
	}
	if !slices.Contains(names, *consumer) {
		return fmt.Errorf("unknown consumer %q; use one of: %s", *consumer, strings.Join(names, ", "))
	}

	broker, err := messaging.Dial(workerCfg.RabbitMQ.URL(), logger)
	if err != nil {
		return err
	}
	defer func() { _ = broker.Close() }()
	topology := messaging.Topology{Queue: *consumer}

	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	if action == "list" {
		letters, total, err := broker.DeadLetters(topology, *limit)
		if err != nil {
			return err
		}
		return encoder.Encode(map[string]any{"consumer": *consumer, "total": total, "messages": letters})
	}

	replayed, replayErr := broker.Replay(ctx, topology, *limit)
	if len(replayed) > 0 {
		err := database.NewTenantDB(pool).Run(tenancy.WithTenant(ctx, tenancy.PlatformTenantID), func(tx pgx.Tx) error {
			return audit.Record(ctx, tx, audit.Entry{
				TenantID: tenancy.PlatformTenantID, ActorType: audit.ActorOperator, ActorID: "worker-cli",
				Action: "messaging.dead_letters.replayed", TargetType: "queue", TargetID: topology.DeadLetterQueue(),
				Metadata: map[string]string{"count": strconv.Itoa(len(replayed)), "reason": strings.TrimSpace(*reason)},
			})
		})
		replayErr = errors.Join(replayErr, err)
	}
	if err := encoder.Encode(map[string]any{"consumer": *consumer, "replayed": replayed}); err != nil {
		return errors.Join(replayErr, err)
	}
	return replayErr
}

// probe supports container health checks in images without a shell.
func probe() error {
	addr, ok := os.LookupEnv("WORKER_HEALTH_ADDR")
	if !ok || addr == "" {
		addr = ":8081"
	}
	if strings.HasPrefix(addr, ":") {
		addr = "127.0.0.1" + addr
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+addr+"/health/live", http.NoBody)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("probe worker health: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("worker health returned %d", resp.StatusCode)
	}
	return nil
}
