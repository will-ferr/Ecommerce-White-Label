package observability

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/trace"

	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/security"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/shared/reqctx"
)

func newTestLogger(out *bytes.Buffer) *slog.Logger {
	return NewLogger(out, slog.LevelInfo, "api", "testing", "1.0.0", nil)
}

func decode(t *testing.T, out *bytes.Buffer) map[string]any {
	t.Helper()
	var entry map[string]any
	if err := json.Unmarshal(out.Bytes(), &entry); err != nil {
		t.Fatalf("invalid log line %q: %v", out.String(), err)
	}
	return entry
}

func TestLoggerWritesStructuredFields(t *testing.T) {
	var out bytes.Buffer
	newTestLogger(&out).Info("order.created", "order_id", "42")

	entry := decode(t, &out)
	for key, want := range map[string]string{
		"message": "order.created", "level": "INFO", "service": "api",
		"environment": "testing", "version": "1.0.0", "order_id": "42",
	} {
		if entry[key] != want {
			t.Errorf("%s = %v, want %s", key, entry[key], want)
		}
	}
	if _, ok := entry["timestamp"]; !ok {
		t.Error("timestamp is missing")
	}
}

func TestLoggerRedactsSensitiveValues(t *testing.T) {
	const marker = "SENSITIVE-VALUE"
	keys := []string{
		"password", "user_password", "Authorization", "refresh_token", "api-key", "client_secret",
		"cookie", "card_number", "cvv", "cpf", "customer_email", "phone", "shipping_address",
	}
	for _, key := range keys {
		t.Run(key, func(t *testing.T) {
			var out bytes.Buffer
			newTestLogger(&out).Info("event", key, marker)
			if strings.Contains(out.String(), marker) {
				t.Errorf("value leaked: %s", out.String())
			}
		})
	}
}

func TestLoggerRedactsGroupsAndBoundAttributes(t *testing.T) {
	const marker = "SENSITIVE-VALUE"
	var out bytes.Buffer
	logger := newTestLogger(&out).With("token", marker).WithGroup("request")
	logger.Info("event", slog.Group("user", "email", marker, "id", "7"))

	if strings.Contains(out.String(), marker) {
		t.Errorf("value leaked: %s", out.String())
	}
	if !strings.Contains(out.String(), `"id":"7"`) {
		t.Errorf("non-sensitive attribute was lost: %s", out.String())
	}
}

func TestLoggerNeverPrintsSecretType(t *testing.T) {
	const marker = "SENSITIVE-VALUE"
	var out bytes.Buffer
	newTestLogger(&out).Info("event", "value", security.NewSecret(marker))
	if strings.Contains(out.String(), marker) {
		t.Errorf("value leaked: %s", out.String())
	}
}

func TestLoggerAddsCorrelationAndTraceIDs(t *testing.T) {
	traceID := trace.TraceID{1, 2, 3}
	spanID := trace.SpanID{4, 5, 6}
	ctx := trace.ContextWithSpanContext(context.Background(), trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: traceID, SpanID: spanID, TraceFlags: trace.FlagsSampled,
	}))
	ctx = reqctx.WithCorrelationID(ctx, "corr-1")
	reqctx.SetTenantID(ctx, "tenant-1")

	var out bytes.Buffer
	newTestLogger(&out).InfoContext(ctx, "event")

	entry := decode(t, &out)
	if entry["tenant_id"] != "tenant-1" {
		t.Errorf("tenant_id missing: %v", entry)
	}
	if entry["correlation_id"] != "corr-1" || entry["trace_id"] != traceID.String() || entry["span_id"] != spanID.String() {
		t.Errorf("identifiers missing: %v", entry)
	}
}

func TestFanoutSendsRedactedRecordsToEveryHandler(t *testing.T) {
	var primary, exported bytes.Buffer
	exporter := slog.NewJSONHandler(&exported, nil)
	logger := NewLogger(&primary, slog.LevelInfo, "api", "testing", "1.0.0", exporter)

	logger.Info("event", "password", "SENSITIVE-VALUE", "order_id", "42")

	for name, out := range map[string]*bytes.Buffer{"primary": &primary, "exported": &exported} {
		if strings.Contains(out.String(), "SENSITIVE-VALUE") {
			t.Errorf("%s handler received the secret: %s", name, out.String())
		}
		if !strings.Contains(out.String(), `"order_id":"42"`) {
			t.Errorf("%s handler lost the record: %s", name, out.String())
		}
	}
}
