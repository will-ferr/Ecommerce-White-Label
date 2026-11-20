package observability

import (
	"context"
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

var counters sync.Map

// Count adds one to a counter. Counters are created on first use, after the
// telemetry setup. Attributes must have few values: never IDs, e-mails or hosts.
func Count(ctx context.Context, name string, attributes ...attribute.KeyValue) {
	value, ok := counters.Load(name)
	if !ok {
		counter, err := otel.Meter("ecommerce").Int64Counter(name)
		if err != nil {
			return
		}
		value, _ = counters.LoadOrStore(name, counter)
	}
	value.(metric.Int64Counter).Add(ctx, 1, metric.WithAttributes(attributes...))
}

// SecurityEvent counts an event that may indicate an attack, for alerts.
func SecurityEvent(ctx context.Context, event string) {
	Count(ctx, "security.events", attribute.String("event", event))
}
