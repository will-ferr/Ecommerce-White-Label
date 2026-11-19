package messaging

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	amqp "github.com/rabbitmq/amqp091-go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

// Relay publishes outbox events to RabbitMQ. Several relays can run at once:
// each batch is locked with SKIP LOCKED, and rows are marked as published only
// after the broker confirms them.
type Relay struct {
	pool      *pgxpool.Pool
	broker    *Broker
	logger    *slog.Logger
	batch     int
	interval  time.Duration
	published metric.Int64Counter
}

// NewRelay creates a Relay that polls the outbox at the given interval.
func NewRelay(pool *pgxpool.Pool, broker *Broker, logger *slog.Logger, interval time.Duration) *Relay {
	meter := otel.Meter("messaging")
	published, err := meter.Int64Counter("messaging.events.published", metric.WithDescription("Events published to the broker"))
	if err != nil {
		logger.Warn("messaging.metric_failed", "error", err)
	}
	_, err = meter.Float64ObservableGauge("messaging.outbox.lag",
		metric.WithUnit("s"), metric.WithDescription("Age of the oldest unpublished event"),
		metric.WithFloat64Callback(func(ctx context.Context, observer metric.Float64Observer) error {
			var lag float64
			if err := pool.QueryRow(ctx, `SELECT messaging.outbox_lag()`).Scan(&lag); err != nil {
				return err
			}
			observer.Observe(lag)
			return nil
		}))
	if err != nil {
		logger.Warn("messaging.metric_failed", "error", err)
	}
	return &Relay{pool: pool, broker: broker, logger: logger, batch: 100, interval: interval, published: published}
}

// Run publishes until ctx is canceled. A full batch is followed immediately by
// the next one; failures back off up to 30 seconds.
func (r *Relay) Run(ctx context.Context) {
	backoff := r.interval
	for {
		count, err := r.PublishBatch(ctx)
		wait := r.interval
		switch {
		case err != nil:
			if ctx.Err() != nil {
				return
			}
			r.logger.ErrorContext(ctx, "messaging.relay_failed", "error", err)
			backoff = min(backoff*2, 30*time.Second)
			wait = backoff
		case count == r.batch:
			backoff, wait = r.interval, 0
		default:
			backoff = r.interval
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
	}
}

type pendingEvent struct {
	TenantID    uuid.UUID
	ID          uuid.UUID
	Type        string
	Source      string
	AggregateID uuid.UUID
	Payload     json.RawMessage
	TraceParent string
	CreatedAt   time.Time
}

// PublishBatch publishes one batch and returns how many events it sent.
func (r *Relay) PublishBatch(ctx context.Context) (int, error) {
	count := 0
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT tenant_id, id, type, source, aggregate_id, payload, traceparent, created_at FROM messaging.claim_outbox($1)`, r.batch)
		if err != nil {
			return fmt.Errorf("claim outbox: %w", err)
		}
		pending, err := pgx.CollectRows(rows, pgx.RowToStructByPos[pendingEvent])
		if err != nil || len(pending) == 0 {
			return err
		}

		messages := make([]Outgoing, 0, len(pending))
		ids := make([]uuid.UUID, 0, len(pending))
		for _, event := range pending {
			body, err := json.Marshal(Event{
				SpecVersion: "1.0", ID: event.ID, Source: event.Source, Type: event.Type, Time: event.CreatedAt.UTC(),
				TenantID: event.TenantID, TraceParent: event.TraceParent, AggregateID: event.AggregateID,
				DataContentType: "application/json", Data: event.Payload,
			})
			if err != nil {
				return fmt.Errorf("encode event %s: %w", event.ID, err)
			}
			messages = append(messages, Outgoing{RoutingKey: event.Type, Publishing: amqp.Publishing{
				ContentType: "application/cloudevents+json", DeliveryMode: amqp.Persistent, MessageId: event.ID.String(),
				Timestamp: event.CreatedAt, Type: event.Type, Body: body,
				Headers: amqp.Table{headerTenant: event.TenantID.String()},
			}})
			ids = append(ids, event.ID)
		}
		if err := r.broker.Publish(ctx, Exchange, messages...); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `SELECT messaging.mark_published($1)`, ids); err != nil {
			return fmt.Errorf("mark published: %w", err)
		}
		count = len(pending)
		return nil
	})
	if err == nil && count > 0 && r.published != nil {
		r.published.Add(ctx, int64(count))
	}
	return count, err
}

// Purge removes published events and processed inbox entries older than the retention.
func Purge(ctx context.Context, pool *pgxpool.Pool, retention time.Duration) (int64, error) {
	var removed int64
	err := pool.QueryRow(ctx, `SELECT messaging.purge($1)`, retention).Scan(&removed)
	if err != nil {
		return 0, fmt.Errorf("purge messaging tables: %w", err)
	}
	return removed, nil
}
