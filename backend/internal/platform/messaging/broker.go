package messaging

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

// Header names used on messages.
const (
	headerAttempt = "x-attempt"
	headerError   = "x-last-error"
	headerTenant  = "x-tenant-id"
)

// publishTimeout bounds the wait for a publisher confirm.
const publishTimeout = 10 * time.Second

// ErrNotConfirmed means the broker did not accept a message.
var ErrNotConfirmed = errors.New("message not confirmed by the broker")

// Broker holds the connection to RabbitMQ and reopens it when it drops.
type Broker struct {
	url    string
	logger *slog.Logger

	mu        sync.Mutex
	conn      *amqp.Connection
	publisher *amqp.Channel
}

// Dial connects to RabbitMQ. The URL carries credentials and is never logged.
func Dial(url string, logger *slog.Logger) (*Broker, error) {
	b := &Broker{url: url, logger: logger}
	if _, err := b.connection(); err != nil {
		return nil, err
	}
	return b, nil
}

func (b *Broker) connection() (*amqp.Connection, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.connectLocked()
}

func (b *Broker) connectLocked() (*amqp.Connection, error) {
	if b.conn != nil && !b.conn.IsClosed() {
		return b.conn, nil
	}
	conn, err := amqp.DialConfig(b.url, amqp.Config{Heartbeat: 10 * time.Second, Properties: amqp.Table{"connection_name": "ecommerce"}})
	if err != nil {
		return nil, fmt.Errorf("connect to rabbitmq: %w", err)
	}
	b.conn, b.publisher = conn, nil
	return conn, nil
}

// Channel opens a new channel. The caller closes it.
func (b *Broker) Channel() (*amqp.Channel, error) {
	conn, err := b.connection()
	if err != nil {
		return nil, err
	}
	ch, err := conn.Channel()
	if err != nil {
		return nil, fmt.Errorf("open channel: %w", err)
	}
	return ch, nil
}

// Ping reports whether the broker is reachable.
func (b *Broker) Ping(context.Context) error {
	ch, err := b.Channel()
	if err != nil {
		return err
	}
	return ch.Close()
}

// Close ends the connection.
func (b *Broker) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.conn == nil || b.conn.IsClosed() {
		return nil
	}
	return b.conn.Close()
}

// Publish sends messages and waits until the broker confirms all of them.
// Publishing is serialized on one confirm channel.
func (b *Broker) Publish(ctx context.Context, exchange string, messages ...Outgoing) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, err := b.connectLocked(); err != nil {
		return err
	}
	if b.publisher == nil || b.publisher.IsClosed() {
		ch, err := b.conn.Channel()
		if err != nil {
			return fmt.Errorf("open publisher channel: %w", err)
		}
		if err := ch.Confirm(false); err != nil {
			_ = ch.Close()
			return fmt.Errorf("enable publisher confirms: %w", err)
		}
		b.publisher = ch
	}

	ctx, cancel := context.WithTimeout(ctx, publishTimeout)
	defer cancel()
	confirms := make([]*amqp.DeferredConfirmation, 0, len(messages))
	for _, message := range messages {
		confirm, err := b.publisher.PublishWithDeferredConfirmWithContext(ctx, exchange, message.RoutingKey, false, false, message.Publishing)
		if err != nil {
			b.publisher = nil
			return fmt.Errorf("publish to %s: %w", message.RoutingKey, err)
		}
		confirms = append(confirms, confirm)
	}
	for _, confirm := range confirms {
		acked, err := confirm.WaitContext(ctx)
		if err != nil {
			b.publisher = nil
			return fmt.Errorf("wait for confirm: %w", err)
		}
		if !acked {
			return ErrNotConfirmed
		}
	}
	return nil
}

// Outgoing is a message to publish.
type Outgoing struct {
	RoutingKey string
	amqp.Publishing
}

// Topology names the queues of one consumer.
type Topology struct {
	Queue       string
	Events      []string
	RetryDelays []time.Duration
}

// RetryQueue is the queue that holds messages before attempt n+1.
func (t Topology) RetryQueue(n int) string { return t.Queue + ".retry." + strconv.Itoa(n) }

// DeadLetterQueue keeps messages whose attempts ran out.
func (t Topology) DeadLetterQueue() string { return t.Queue + ".dlq" }

// Declare creates the exchange, the main queue bound to its events, one
// delayed queue per retry and the dead letter queue. It is idempotent.
func (t Topology) Declare(ch *amqp.Channel) error {
	if err := ch.ExchangeDeclare(Exchange, amqp.ExchangeTopic, true, false, false, false, nil); err != nil {
		return fmt.Errorf("declare exchange: %w", err)
	}
	// Messages rejected by the broker itself, such as after too many
	// redeliveries, also end in the dead letter queue.
	if _, err := ch.QueueDeclare(t.Queue, true, false, false, false, amqp.Table{
		amqp.QueueTypeArg:           amqp.QueueTypeQuorum,
		"x-dead-letter-exchange":    "",
		"x-dead-letter-routing-key": t.DeadLetterQueue(),
	}); err != nil {
		return fmt.Errorf("declare %s: %w", t.Queue, err)
	}
	for _, event := range t.Events {
		if err := ch.QueueBind(t.Queue, event, Exchange, false, nil); err != nil {
			return fmt.Errorf("bind %s to %s: %w", t.Queue, event, err)
		}
	}
	for n, delay := range t.RetryDelays {
		if _, err := ch.QueueDeclare(t.RetryQueue(n+1), true, false, false, false, amqp.Table{
			"x-message-ttl":             delay.Milliseconds(),
			"x-dead-letter-exchange":    "",
			"x-dead-letter-routing-key": t.Queue,
		}); err != nil {
			return fmt.Errorf("declare retry queue: %w", err)
		}
	}
	// A classic queue, so inspecting it never counts as a redelivery.
	if _, err := ch.QueueDeclare(t.DeadLetterQueue(), true, false, false, false, amqp.Table{
		amqp.QueueTypeArg: amqp.QueueTypeClassic,
	}); err != nil {
		return fmt.Errorf("declare dead letter queue: %w", err)
	}
	return nil
}
