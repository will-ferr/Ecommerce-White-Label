package messaging

import (
	"context"
	"encoding/json"
	"fmt"

	amqp "github.com/rabbitmq/amqp091-go"
)

// DeadLetter summarizes a message kept in a dead letter queue. It shows the
// envelope metadata and the failure, never the event data.
type DeadLetter struct {
	MessageID string `json:"messageId"`
	Type      string `json:"type"`
	TenantID  string `json:"tenantId"`
	Attempts  int    `json:"attempts"`
	Error     string `json:"error"`
}

func deadLetter(delivery amqp.Delivery) DeadLetter {
	letter := DeadLetter{MessageID: delivery.MessageId, Type: delivery.Type, Attempts: attemptOf(delivery.Headers)}
	letter.TenantID, _ = delivery.Headers[headerTenant].(string)
	letter.Error, _ = delivery.Headers[headerError].(string)
	if letter.Type == "" {
		var event Event
		if json.Unmarshal(delivery.Body, &event) == nil {
			letter.Type = event.Type
		}
	}
	return letter
}

// DeadLetters lists up to limit messages of a dead letter queue without
// removing them: they return to the queue when the channel closes.
func (b *Broker) DeadLetters(topology Topology, limit int) ([]DeadLetter, int, error) {
	ch, err := b.Channel()
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = ch.Close() }()
	queue, err := ch.QueueDeclarePassive(topology.DeadLetterQueue(), true, false, false, false, amqp.Table{amqp.QueueTypeArg: amqp.QueueTypeClassic})
	if err != nil {
		return nil, 0, fmt.Errorf("inspect %s: %w", topology.DeadLetterQueue(), err)
	}
	letters := []DeadLetter{}
	for range min(limit, queue.Messages) {
		delivery, ok, err := ch.Get(topology.DeadLetterQueue(), false)
		if err != nil {
			return nil, 0, fmt.Errorf("read %s: %w", topology.DeadLetterQueue(), err)
		}
		if !ok {
			break
		}
		letters = append(letters, deadLetter(delivery))
	}
	return letters, queue.Messages, nil
}

// Replay moves up to limit messages from the dead letter queue back to the
// consumer queue, as first attempts. A message is removed from the dead
// letter queue only after the broker confirms the copy.
func (b *Broker) Replay(ctx context.Context, topology Topology, limit int) ([]DeadLetter, error) {
	ch, err := b.Channel()
	if err != nil {
		return nil, err
	}
	defer func() { _ = ch.Close() }()
	replayed := []DeadLetter{}
	for range limit {
		delivery, ok, err := ch.Get(topology.DeadLetterQueue(), false)
		if err != nil {
			return replayed, fmt.Errorf("read %s: %w", topology.DeadLetterQueue(), err)
		}
		if !ok {
			break
		}
		headers := amqp.Table{}
		if tenant, ok := delivery.Headers[headerTenant]; ok {
			headers[headerTenant] = tenant
		}
		err = b.Publish(ctx, "", Outgoing{RoutingKey: topology.Queue, Publishing: amqp.Publishing{
			ContentType: delivery.ContentType, DeliveryMode: amqp.Persistent, MessageId: delivery.MessageId,
			Timestamp: delivery.Timestamp, Type: delivery.Type, Body: delivery.Body, Headers: headers,
		}})
		if err != nil {
			_ = delivery.Nack(false, true)
			return replayed, err
		}
		if err := delivery.Ack(false); err != nil {
			return replayed, fmt.Errorf("remove replayed message: %w", err)
		}
		replayed = append(replayed, deadLetter(delivery))
	}
	return replayed, nil
}
