package observability

import (
	"context"
	"io"
	"log/slog"
	"strings"

	"go.opentelemetry.io/otel/trace"

	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/shared/reqctx"
)

const redactedValue = "[REDACTED]"

// sensitiveKeyParts lists attribute name fragments that are never logged.
var sensitiveKeyParts = []string{
	"password", "passwd", "secret", "token", "authorization", "cookie",
	"api_key", "apikey", "credential", "private_key",
	"cvv", "cvc", "card_number", "pan",
	"cpf", "cnpj", "document", "email", "phone", "address",
}

// NewLogger builds the process logger. Records go to out as JSON and, when
// exporter is not nil, to the OpenTelemetry pipeline.
func NewLogger(out io.Writer, level slog.Leveler, service, environment, version string, exporter slog.Handler) *slog.Logger {
	jsonHandler := slog.NewJSONHandler(out, &slog.HandlerOptions{
		Level: level,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if len(groups) > 0 {
				return a
			}
			switch a.Key {
			case slog.TimeKey:
				a.Key = "timestamp"
			case slog.MessageKey:
				a.Key = "message"
			}
			return a
		},
	})

	var sink slog.Handler = jsonHandler
	if exporter != nil {
		sink = slog.NewMultiHandler(jsonHandler, exporter)
	}

	handler := redactingHandler{next: contextHandler{next: sink}}
	return slog.New(handler).With("service", service, "environment", environment, "version", version)
}

// redactingHandler removes sensitive attributes before any sink sees them.
type redactingHandler struct {
	next slog.Handler
}

func (h redactingHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h redactingHandler) Handle(ctx context.Context, record slog.Record) error {
	clean := slog.NewRecord(record.Time, record.Level, record.Message, record.PC)
	record.Attrs(func(a slog.Attr) bool {
		clean.AddAttrs(redactAttr(a))
		return true
	})
	return h.next.Handle(ctx, clean)
}

func (h redactingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	clean := make([]slog.Attr, 0, len(attrs))
	for _, a := range attrs {
		clean = append(clean, redactAttr(a))
	}
	return redactingHandler{next: h.next.WithAttrs(clean)}
}

func (h redactingHandler) WithGroup(name string) slog.Handler {
	return redactingHandler{next: h.next.WithGroup(name)}
}

func redactAttr(a slog.Attr) slog.Attr {
	if isSensitiveKey(a.Key) {
		return slog.String(a.Key, redactedValue)
	}
	a.Value = a.Value.Resolve()
	if a.Value.Kind() != slog.KindGroup {
		return a
	}
	group := a.Value.Group()
	clean := make([]slog.Attr, 0, len(group))
	for _, member := range group {
		clean = append(clean, redactAttr(member))
	}
	return slog.Attr{Key: a.Key, Value: slog.GroupValue(clean...)}
}

func isSensitiveKey(key string) bool {
	normalized := strings.ToLower(strings.ReplaceAll(key, "-", "_"))
	for _, part := range sensitiveKeyParts {
		if strings.Contains(normalized, part) {
			return true
		}
	}
	return false
}

// contextHandler adds the correlation and trace identifiers to each record.
type contextHandler struct {
	next slog.Handler
}

func (h contextHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h contextHandler) Handle(ctx context.Context, record slog.Record) error {
	if id := reqctx.CorrelationID(ctx); id != "" {
		record.AddAttrs(slog.String("correlation_id", id))
	}
	if id := reqctx.TenantID(ctx); id != "" {
		record.AddAttrs(slog.String("tenant_id", id))
	}
	if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
		record.AddAttrs(
			slog.String("trace_id", sc.TraceID().String()),
			slog.String("span_id", sc.SpanID().String()),
		)
	}
	return h.next.Handle(ctx, record)
}

func (h contextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return contextHandler{next: h.next.WithAttrs(attrs)}
}

func (h contextHandler) WithGroup(name string) slog.Handler {
	return contextHandler{next: h.next.WithGroup(name)}
}
