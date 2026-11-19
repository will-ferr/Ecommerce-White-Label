//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/rabbitmq"

	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/database"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/messaging"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/tenancy"
)

const (
	rabbitImage = "rabbitmq:4.4.1-management-alpine"
	testEvent   = "tests.thing.happened.v1"
)

var testRetryDelays = []time.Duration{200 * time.Millisecond, 200 * time.Millisecond, 200 * time.Millisecond}

func startRabbit(ctx context.Context, t *testing.T) *messaging.Broker {
	t.Helper()
	container, err := rabbitmq.Run(ctx, rabbitImage)
	testcontainers.CleanupContainer(t, container)
	if err != nil {
		t.Fatalf("start rabbitmq: %v", err)
	}
	url, err := container.AmqpURL(ctx)
	if err != nil {
		t.Fatalf("rabbitmq url: %v", err)
	}
	broker, err := messaging.Dial(url, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("dial rabbitmq: %v", err)
	}
	t.Cleanup(func() { _ = broker.Close() })
	return broker
}

func quietLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// record writes one event to the outbox of a store, as a business change would.
func (s *suite) record(t *testing.T, slug, eventType string, data any) uuid.UUID {
	t.Helper()
	aggregate := uuid.New()
	err := database.NewTenantDB(s.runtime).Run(tenancy.WithTenant(s.ctx, s.tenants[slug]), func(tx pgx.Tx) error {
		return messaging.Record(s.ctx, tx, s.tenants[slug], "/tests", eventType, aggregate, data)
	})
	if err != nil {
		t.Fatalf("record event: %v", err)
	}
	return aggregate
}

func (s *suite) unpublished(t *testing.T) int {
	t.Helper()
	var count int
	if err := s.admin.QueryRow(s.ctx, `SELECT count(*) FROM messaging.outbox WHERE published_at IS NULL`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func eventually(t *testing.T, what string, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting until %s", what)
}

// tap counts every message of an event type that reaches the broker,
// including duplicates that consumers would ignore.
func tap(t *testing.T, broker *messaging.Broker, eventType string) *atomic.Int64 {
	t.Helper()
	ch, err := broker.Channel()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ch.Close() })
	if err := (messaging.Topology{Queue: "test-tap-" + eventType}).Declare(ch); err != nil {
		t.Fatal(err)
	}
	if err := ch.QueueBind("test-tap-"+eventType, eventType, messaging.Exchange, false, nil); err != nil {
		t.Fatal(err)
	}
	deliveries, err := ch.Consume("test-tap-"+eventType, "", true, false, false, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	var count atomic.Int64
	go func() {
		for range deliveries {
			count.Add(1)
		}
	}()
	return &count
}

type recordingHandler struct {
	mu       sync.Mutex
	calls    map[uuid.UUID]int
	tenants  map[uuid.UUID]uuid.UUID
	failures map[uuid.UUID]int // remaining failures per aggregate; -1 fails forever
}

func newRecordingHandler() *recordingHandler {
	return &recordingHandler{calls: map[uuid.UUID]int{}, tenants: map[uuid.UUID]uuid.UUID{}, failures: map[uuid.UUID]int{}}
}

func (h *recordingHandler) handle(ctx context.Context, tx pgx.Tx, event messaging.Event) error {
	var current string
	if err := tx.QueryRow(ctx, `SELECT current_setting('app.tenant_id')`).Scan(&current); err != nil {
		return err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.calls[event.AggregateID]++
	h.tenants[event.AggregateID] = uuid.MustParse(current)
	switch remaining := h.failures[event.AggregateID]; {
	case remaining < 0:
		return errors.New("downstream unavailable")
	case remaining > 0:
		h.failures[event.AggregateID]--
		return errors.New("temporary failure")
	}
	return nil
}

func (h *recordingHandler) count(aggregate uuid.UUID) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.calls[aggregate]
}

func (s *suite) processed(t *testing.T, consumer string, aggregate uuid.UUID) bool {
	t.Helper()
	var exists bool
	err := s.admin.QueryRow(s.ctx, `
		SELECT EXISTS (SELECT 1 FROM messaging.inbox i JOIN messaging.outbox o ON o.id = i.event_id
		               WHERE i.consumer = $1 AND o.aggregate_id = $2)`, consumer, aggregate).Scan(&exists)
	if err != nil {
		t.Fatal(err)
	}
	return exists
}

func TestEventDelivery(t *testing.T) {
	s := newSuite(t)
	broker := startRabbit(s.ctx, t)
	ctx, stop := context.WithCancel(s.ctx)
	defer stop()

	handler := newRecordingHandler()
	consumer := messaging.NewConsumer(s.runtime, broker, quietLogger(), messaging.Subscription{
		Name: "test-consumer", Events: []string{testEvent}, Handle: handler.handle,
	}, testRetryDelays)
	go consumer.Run(ctx)
	relay := messaging.NewRelay(s.runtime, broker, quietLogger(), 50*time.Millisecond)
	tapped := tap(t, broker, testEvent)
	// The consumer declares its queue when it starts.
	eventually(t, "the consumer queue exists", func() bool {
		ch, err := broker.Channel()
		if err != nil {
			return false
		}
		defer func() { _ = ch.Close() }()
		_, err = ch.QueueDeclarePassive("test-consumer", true, false, false, false, amqp.Table{amqp.QueueTypeArg: amqp.QueueTypeQuorum})
		return err == nil
	})

	t.Run("an event reaches its consumer in the context of its store", func(t *testing.T) {
		aggregate := s.record(t, "beta", testEvent, map[string]string{"note": "olá"})
		if published, err := relay.PublishBatch(s.ctx); err != nil || published != 1 {
			t.Fatalf("PublishBatch() = %d, %v", published, err)
		}
		eventually(t, "the event is handled", func() bool { return s.processed(t, "test-consumer", aggregate) })
		if got := handler.tenants[aggregate]; got != s.tenants["beta"] {
			t.Errorf("handler ran for tenant %s, want beta", got)
		}
		if s.unpublished(t) != 0 {
			t.Error("the event is still pending in the outbox")
		}
	})

	t.Run("a duplicate delivery has no effect", func(t *testing.T) {
		aggregate := s.record(t, "alfa", testEvent, map[string]string{})
		var body []byte
		err := s.admin.QueryRow(s.ctx, `
			SELECT json_build_object('specversion', '1.0', 'id', id, 'source', source, 'type', type, 'time', created_at,
			       'tenantid', tenant_id, 'aggregateid', aggregate_id, 'datacontenttype', 'application/json', 'data', payload)::text
			FROM messaging.outbox WHERE aggregate_id = $1`, aggregate).Scan(&body)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := relay.PublishBatch(s.ctx); err != nil {
			t.Fatal(err)
		}
		eventually(t, "the event is handled", func() bool { return s.processed(t, "test-consumer", aggregate) })

		// The broker delivers the same event again, as after a lost acknowledgement.
		duplicate := messaging.Outgoing{RoutingKey: testEvent, Publishing: amqp.Publishing{Body: body}}
		if err := broker.Publish(s.ctx, messaging.Exchange, duplicate); err != nil {
			t.Fatal(err)
		}
		outcome, err := consumer.Process(s.ctx, body, 1)
		if err != nil || outcome.Status != "duplicate" {
			t.Errorf("Process(duplicate) = %+v, %v", outcome, err)
		}
		time.Sleep(500 * time.Millisecond)
		if calls := handler.count(aggregate); calls != 1 {
			t.Errorf("handler ran %d times, want 1", calls)
		}
	})

	t.Run("a temporary failure is retried", func(t *testing.T) {
		aggregate := uuid.New()
		handler.mu.Lock()
		handler.failures[aggregate] = 2
		handler.mu.Unlock()
		err := database.NewTenantDB(s.runtime).Run(tenancy.WithTenant(s.ctx, s.tenants["alfa"]), func(tx pgx.Tx) error {
			return messaging.Record(s.ctx, tx, s.tenants["alfa"], "/tests", testEvent, aggregate, map[string]string{})
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := relay.PublishBatch(s.ctx); err != nil {
			t.Fatal(err)
		}
		eventually(t, "the event is handled after retries", func() bool { return s.processed(t, "test-consumer", aggregate) })
		if calls := handler.count(aggregate); calls != 3 {
			t.Errorf("handler ran %d times, want 3", calls)
		}
	})

	t.Run("exhausted attempts go to the dead letter queue and can be replayed", func(t *testing.T) {
		aggregate := uuid.New()
		handler.mu.Lock()
		handler.failures[aggregate] = -1
		handler.mu.Unlock()
		err := database.NewTenantDB(s.runtime).Run(tenancy.WithTenant(s.ctx, s.tenants["gama"]), func(tx pgx.Tx) error {
			return messaging.Record(s.ctx, tx, s.tenants["gama"], "/tests", testEvent, aggregate, map[string]string{})
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := relay.PublishBatch(s.ctx); err != nil {
			t.Fatal(err)
		}

		var letters []messaging.DeadLetter
		eventually(t, "the event reaches the dead letter queue", func() bool {
			letters, _, err = broker.DeadLetters(consumer.Topology(), 10)
			return err == nil && len(letters) == 1
		})
		attempts := len(testRetryDelays) + 1
		if calls := handler.count(aggregate); calls != attempts {
			t.Errorf("handler ran %d times, want %d", calls, attempts)
		}
		letter := letters[0]
		if letter.Type != testEvent || letter.TenantID != s.tenants["gama"].String() ||
			letter.Error != "downstream unavailable" || letter.Attempts != attempts {
			t.Errorf("dead letter = %+v", letter)
		}
		if s.processed(t, "test-consumer", aggregate) {
			t.Fatal("a failed event was marked as processed")
		}
		// Listing does not remove messages.
		if again, total, _ := broker.DeadLetters(consumer.Topology(), 10); len(again) != 1 || total != 1 {
			t.Fatalf("second listing = %v (%d)", again, total)
		}

		handler.mu.Lock()
		handler.failures[aggregate] = 0
		handler.mu.Unlock()
		replayed, err := broker.Replay(s.ctx, consumer.Topology(), 10)
		if err != nil || len(replayed) != 1 {
			t.Fatalf("Replay() = %v, %v", replayed, err)
		}
		eventually(t, "the replayed event is handled", func() bool { return s.processed(t, "test-consumer", aggregate) })
		if left, total, _ := broker.DeadLetters(consumer.Topology(), 10); len(left) != 0 || total != 0 {
			t.Errorf("dead letter queue still has %v", left)
		}
	})

	t.Run("an invalid message goes straight to the dead letter queue", func(t *testing.T) {
		outcome, err := consumer.Process(s.ctx, []byte(`{"not":"an event"}`), 1)
		if err != nil || outcome.Status != "dead" {
			t.Errorf("Process(invalid) = %+v, %v", outcome, err)
		}
		forged, _ := json.Marshal(messaging.Event{SpecVersion: "1.0", ID: uuid.New(), Type: testEvent, Data: []byte(`{}`)})
		if outcome, _ := consumer.Process(s.ctx, forged, 1); outcome.Status != "dead" {
			t.Errorf("an event without a store was accepted: %+v", outcome)
		}
	})

	t.Run("concurrent relays publish each event once", func(t *testing.T) {
		before := tapped.Load()
		const total = 60
		for range total {
			s.record(t, "alfa", testEvent, map[string]string{})
		}
		var wg sync.WaitGroup
		for range 3 {
			wg.Go(func() {
				other := messaging.NewRelay(s.runtime, broker, quietLogger(), time.Millisecond)
				for s.unpublished(t) > 0 {
					if _, err := other.PublishBatch(s.ctx); err != nil {
						t.Error(err)
						return
					}
				}
			})
		}
		wg.Wait()
		eventually(t, "every event reaches the broker", func() bool { return tapped.Load()-before >= total })
		time.Sleep(300 * time.Millisecond)
		if got := tapped.Load() - before; got != total {
			t.Errorf("broker received %d messages, want %d", got, total)
		}
	})

	t.Run("the runtime role cannot read or change the outbox directly", func(t *testing.T) {
		for _, statement := range []string{
			`SELECT count(*) FROM messaging.outbox`,
			`UPDATE messaging.outbox SET published_at = now()`,
			`DELETE FROM messaging.inbox`,
		} {
			if _, err := s.runtime.Exec(s.ctx, statement); err == nil {
				t.Errorf("runtime role ran: %s", statement)
			}
		}
	})
}
