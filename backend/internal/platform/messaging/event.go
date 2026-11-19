// Package messaging publishes domain events through a transactional outbox and
// delivers them to consumers through RabbitMQ (ADR-009, ADR-018).
//
// Delivery is at least once. Consumers record each event in an inbox table in
// the same transaction as its effect, so a repeated delivery changes nothing.
package messaging

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel/propagation"
)

// Exchange receives every domain event. Consumers bind their queues to it by
// event type.
const Exchange = "domain.events"

var typePattern = regexp.MustCompile(`^[a-z_]+(\.[a-z_]+)+\.v[0-9]+$`)

// Event is the CloudEvents 1.0 envelope of a domain event. Events carry
// identifiers, never personal data: consumers read what they need.
type Event struct {
	SpecVersion     string          `json:"specversion"`
	ID              uuid.UUID       `json:"id"`
	Source          string          `json:"source"`
	Type            string          `json:"type"`
	Time            time.Time       `json:"time"`
	TenantID        uuid.UUID       `json:"tenantid"`
	TraceParent     string          `json:"traceparent,omitempty"`
	AggregateID     uuid.UUID       `json:"aggregateid"`
	DataContentType string          `json:"datacontenttype"`
	Data            json.RawMessage `json:"data"`
}

// Decode reads the event data into v.
func (e Event) Decode(v any) error {
	if err := json.Unmarshal(e.Data, v); err != nil {
		return Permanent(fmt.Errorf("decode %s data: %w", e.Type, err))
	}
	return nil
}

func (e Event) valid() bool {
	return e.SpecVersion == "1.0" && e.ID != uuid.Nil && e.TenantID != uuid.Nil && typePattern.MatchString(e.Type) && len(e.Data) > 0
}

// Record adds an event to the outbox inside tx, so it is published only if the
// business change commits.
func Record(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, source, eventType string, aggregateID uuid.UUID, data any) error {
	if !typePattern.MatchString(eventType) {
		return fmt.Errorf("invalid event type %q", eventType)
	}
	payload, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("encode %s: %w", eventType, err)
	}
	id, err := uuid.NewV7()
	if err != nil {
		return fmt.Errorf("event id: %w", err)
	}
	carrier := propagation.MapCarrier{}
	propagation.TraceContext{}.Inject(ctx, carrier)
	_, err = tx.Exec(ctx, `
		INSERT INTO messaging.outbox (tenant_id, id, type, source, aggregate_id, payload, traceparent)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		tenantID, id, eventType, source, aggregateID, payload, carrier.Get("traceparent"))
	if err != nil {
		return fmt.Errorf("record %s: %w", eventType, err)
	}
	return nil
}

// permanentError marks a failure that a retry cannot fix.
type permanentError struct{ err error }

func (e permanentError) Error() string { return e.err.Error() }
func (e permanentError) Unwrap() error { return e.err }

// Permanent wraps an error so the event goes straight to the dead letter queue.
func Permanent(err error) error {
	if err == nil {
		return nil
	}
	return permanentError{err: err}
}
