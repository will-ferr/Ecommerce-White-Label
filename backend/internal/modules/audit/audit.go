// Package audit records administrative operations in an append-only trail.
package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel/trace"

	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/modules/audit/internal/sqlcgen"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/shared/reqctx"
)

// Actor types.
const (
	ActorUser     = "user"
	ActorOperator = "operator"
	ActorSystem   = "system"
)

// Entry describes one operation. Metadata must not contain secrets.
type Entry struct {
	TenantID   uuid.UUID
	ActorType  string
	ActorID    string
	Action     string
	TargetType string
	TargetID   string
	Metadata   map[string]string
}

// Record writes the entry in the transaction of the change it describes, so an
// operation and its trail are committed or rolled back together.
func Record(ctx context.Context, tx pgx.Tx, entry Entry) error {
	id, err := uuid.NewV7()
	if err != nil {
		return fmt.Errorf("generate audit id: %w", err)
	}
	metadata := entry.Metadata
	if metadata == nil {
		metadata = map[string]string{}
	}
	encoded, err := json.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("encode audit metadata: %w", err)
	}

	traceID := ""
	if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
		traceID = sc.TraceID().String()
	}
	err = sqlcgen.New(tx).InsertEntry(ctx, sqlcgen.InsertEntryParams{
		TenantID: entry.TenantID, ID: id, ActorType: entry.ActorType, ActorID: entry.ActorID,
		Action: entry.Action, TargetType: entry.TargetType, TargetID: entry.TargetID,
		Ip: reqctx.ClientIP(ctx), TraceID: traceID, Metadata: encoded,
	})
	if err != nil {
		return fmt.Errorf("record audit entry: %w", err)
	}
	return nil
}

// View is an entry returned to authorized readers.
type View struct {
	ID         uuid.UUID         `json:"id"`
	ActorType  string            `json:"actorType"`
	ActorID    string            `json:"actorId"`
	Action     string            `json:"action"`
	TargetType string            `json:"targetType"`
	TargetID   string            `json:"targetId"`
	IP         string            `json:"ip"`
	Metadata   map[string]string `json:"metadata"`
	OccurredAt time.Time         `json:"occurredAt"`
}

// List returns the most recent entries of a tenant.
func List(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, limit int32) ([]View, error) {
	rows, err := sqlcgen.New(tx).ListEntries(ctx, sqlcgen.ListEntriesParams{TenantID: tenantID, Limit: limit})
	if err != nil {
		return nil, fmt.Errorf("list audit entries: %w", err)
	}
	views := make([]View, 0, len(rows))
	for _, row := range rows {
		metadata := map[string]string{}
		if err := json.Unmarshal(row.Metadata, &metadata); err != nil {
			return nil, fmt.Errorf("decode audit metadata: %w", err)
		}
		views = append(views, View{
			ID: row.ID, ActorType: row.ActorType, ActorID: row.ActorID, Action: row.Action,
			TargetType: row.TargetType, TargetID: row.TargetID, IP: row.Ip, Metadata: metadata,
			OccurredAt: row.OccurredAt,
		})
	}
	return views, nil
}

type operatorKey struct{}

// WithOperator records which platform operator is acting, so operator
// changes are attributed to a person instead of the command line tool.
func WithOperator(ctx context.Context, operatorID string) context.Context {
	return context.WithValue(ctx, operatorKey{}, operatorID)
}

// OperatorFrom returns the operator acting in ctx, or "tenantctl" for the
// command line tool.
func OperatorFrom(ctx context.Context) string {
	if id, ok := ctx.Value(operatorKey{}).(string); ok && id != "" {
		return id
	}
	return "tenantctl"
}
