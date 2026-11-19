package messaging

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	amqp "github.com/rabbitmq/amqp091-go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/database"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/tenancy"
)

// DefaultRetryDelays wait before attempts 2 to 5 (docs/architecture/event-driven-architecture.md).
var DefaultRetryDelays = []time.Duration{10 * time.Second, time.Minute, 10 * time.Minute, time.Hour}

// Handler applies an event inside a transaction bound to the event's store.
// Returning an error rolls back the effect and schedules a retry.
type Handler func(ctx context.Context, tx pgx.Tx, event Event) error

// Subscription is a named consumer and the events it handles.
type Subscription struct {
	Name   string
	Events []string
	Handle Handler
}

// Consumer delivers the events of one subscription.
type Consumer struct {
	db       *database.TenantDB
	broker   *Broker
	logger   *slog.Logger
	sub      Subscription
	topology Topology
	outcomes metric.Int64Counter
}

// NewConsumer creates a Consumer. Empty retry delays use DefaultRetryDelays.
func NewConsumer(pool *pgxpool.Pool, broker *Broker, logger *slog.Logger, sub Subscription, retryDelays []time.Duration) *Consumer {
	if len(retryDelays) == 0 {
		retryDelays = DefaultRetryDelays
	}
	outcomes, err := otel.Meter("messaging").Int64Counter("messaging.events.consumed", metric.WithDescription("Events handled, by consumer and outcome"))
	if err != nil {
		logger.Warn("messaging.metric_failed", "error", err)
	}
	return &Consumer{
		db: database.NewTenantDB(pool), broker: broker, logger: logger.With("consumer", sub.Name), sub: sub,
		topology: Topology{Queue: sub.Name, Events: sub.Events, RetryDelays: retryDelays}, outcomes: outcomes,
	}
}

// Topology returns the queues of the consumer.
func (c *Consumer) Topology() Topology { return c.topology }

// Run consumes until ctx is canceled, reconnecting after failures.
func (c *Consumer) Run(ctx context.Context) {
	backoff := time.Second
	for {
		err := c.consume(ctx)
		if ctx.Err() != nil {
			return
		}
		c.logger.ErrorContext(ctx, "messaging.consumer_stopped", "error", err)
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, 30*time.Second)
	}
}

func (c *Consumer) consume(ctx context.Context) error {
	ch, err := c.broker.Channel()
	if err != nil {
		return err
	}
	defer func() { _ = ch.Close() }()
	if err := c.topology.Declare(ch); err != nil {
		return err
	}
	if err := ch.Qos(8, 0, false); err != nil {
		return fmt.Errorf("set prefetch: %w", err)
	}
	deliveries, err := ch.ConsumeWithContext(ctx, c.topology.Queue, "", false, false, false, false, nil)
	if err != nil {
		return fmt.Errorf("consume %s: %w", c.topology.Queue, err)
	}
	c.logger.InfoContext(ctx, "messaging.consumer_started")
	for delivery := range deliveries {
		c.deliver(ctx, delivery)
	}
	if ctx.Err() != nil {
		return nil
	}
	return errors.New("delivery channel closed")
}

// deliver handles one message and always settles it: acknowledged when handled
// or rescheduled, rejected to the dead letter queue only if rescheduling fails.
func (c *Consumer) deliver(ctx context.Context, delivery amqp.Delivery) {
	outcome, err := c.Process(ctx, delivery.Body, attemptOf(delivery.Headers))
	if err == nil {
		err = c.reschedule(ctx, delivery, outcome)
	}
	if err != nil {
		c.logger.ErrorContext(ctx, "messaging.settle_failed", "error", err)
		_ = delivery.Nack(false, false)
		return
	}
	if ackErr := delivery.Ack(false); ackErr != nil {
		c.logger.ErrorContext(ctx, "messaging.ack_failed", "error", ackErr)
	}
}

// Outcome is the result of processing one message.
type Outcome struct {
	Status string // handled, duplicate, retry or dead
	Reason string
	Body   []byte
	Next   int
}

// Process runs the handler for a message body received on its given attempt
// (1 for the first delivery) and decides what happens next. It is exported so
// tests can exercise the decision without a broker.
func (c *Consumer) Process(ctx context.Context, body []byte, attempt int) (Outcome, error) {
	var event Event
	if err := json.Unmarshal(body, &event); err != nil || !event.valid() {
		c.count(ctx, "dead", "")
		return Outcome{Status: "dead", Reason: "invalid envelope", Body: body}, nil
	}
	ctx = propagation.TraceContext{}.Extract(ctx, propagation.MapCarrier{"traceparent": event.TraceParent})
	ctx, span := otel.Tracer("messaging").Start(ctx, c.sub.Name+" "+event.Type, trace.WithSpanKind(trace.SpanKindConsumer),
		trace.WithAttributes(
			attribute.String("messaging.system", "rabbitmq"),
			attribute.String("messaging.destination.name", c.topology.Queue),
			attribute.String("messaging.message.id", event.ID.String()),
			attribute.Int("messaging.attempt", attempt),
		))
	defer span.End()

	duplicate := false
	err := c.db.Run(tenancy.WithTenant(ctx, event.TenantID), func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `INSERT INTO messaging.inbox (tenant_id, consumer, event_id) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`,
			event.TenantID, c.sub.Name, event.ID)
		if err != nil {
			return fmt.Errorf("record inbox: %w", err)
		}
		if tag.RowsAffected() == 0 {
			duplicate = true
			return nil
		}
		return c.sub.Handle(ctx, tx, event)
	})
	switch {
	case err == nil && duplicate:
		c.count(ctx, "duplicate", event.Type)
		return Outcome{Status: "duplicate"}, nil
	case err == nil:
		c.count(ctx, "handled", event.Type)
		return Outcome{Status: "handled"}, nil
	}

	span.RecordError(err)
	span.SetStatus(codes.Error, "handler failed")
	reason := err.Error()
	var permanent permanentError
	if errors.As(err, &permanent) || attempt > len(c.topology.RetryDelays) {
		c.logger.ErrorContext(ctx, "messaging.event_dead", "event_id", event.ID, "event_type", event.Type, "tenant_id", event.TenantID, "attempt", attempt, "error", err)
		c.count(ctx, "dead", event.Type)
		return Outcome{Status: "dead", Reason: reason, Body: body}, nil
	}
	c.logger.WarnContext(ctx, "messaging.event_retry", "event_id", event.ID, "event_type", event.Type, "tenant_id", event.TenantID, "attempt", attempt, "error", err)
	c.count(ctx, "retry", event.Type)
	return Outcome{Status: "retry", Reason: reason, Body: body, Next: attempt + 1}, nil
}

func (c *Consumer) reschedule(ctx context.Context, delivery amqp.Delivery, outcome Outcome) error {
	var queue string
	headers := amqp.Table{headerError: truncate(outcome.Reason, 300)}
	switch outcome.Status {
	case "retry":
		queue = c.topology.RetryQueue(outcome.Next - 1)
		headers[headerAttempt] = int32(outcome.Next) //nolint:gosec // bounded by the number of retry delays
	case "dead":
		queue = c.topology.DeadLetterQueue()
		headers[headerAttempt] = int32(attemptOf(delivery.Headers)) //nolint:gosec // small counter
	default:
		return nil
	}
	if tenant, ok := delivery.Headers[headerTenant]; ok {
		headers[headerTenant] = tenant
	}
	return c.broker.Publish(ctx, "", Outgoing{RoutingKey: queue, Publishing: amqp.Publishing{
		ContentType: delivery.ContentType, DeliveryMode: amqp.Persistent, MessageId: delivery.MessageId,
		Timestamp: delivery.Timestamp, Type: delivery.Type, Body: outcome.Body, Headers: headers,
	}})
}

func (c *Consumer) count(ctx context.Context, outcome, eventType string) {
	if c.outcomes != nil {
		c.outcomes.Add(ctx, 1, metric.WithAttributes(
			attribute.String("consumer", c.sub.Name), attribute.String("outcome", outcome), attribute.String("event_type", eventType)))
	}
}

func attemptOf(headers amqp.Table) int {
	switch value := headers[headerAttempt].(type) {
	case int32:
		return max(int(value), 1)
	case int64:
		return max(int(value), 1)
	case int:
		return max(value, 1)
	}
	return 1
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}
