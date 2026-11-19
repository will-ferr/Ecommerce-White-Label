package mail

import (
	"context"
	"crypto/hkdf"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/database"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/security"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/tenancy"
)

var (
	kindPattern    = regexp.MustCompile(`^[a-z_.]{3,60}$`)
	addressPattern = regexp.MustCompile(`[\p{L}\p{N}._%+!#$&'*/?^{|}~-]+@[\p{L}\p{N}.-]+`)
)

// redact removes e-mail addresses that mail servers repeat in their errors,
// so failures can be logged and stored.
func redact(text string) string {
	return addressPattern.ReplaceAllString(text, "[e-mail]")
}

// NewSealer derives the key that protects stored messages from the
// encryption key of the platform.
func NewSealer(encryptionKey []byte) (*security.Sealer, error) {
	key, err := hkdf.Key(sha256.New, encryptionKey, nil, "mail-outbox-v1", 32)
	if err != nil {
		return nil, fmt.Errorf("derive mail key: %w", err)
	}
	return security.NewSealer(key)
}

func sealContext(tenantID, id uuid.UUID) []byte {
	return []byte(tenantID.String() + "|" + id.String())
}

// Outbox stores messages for the dispatcher. A message survives restarts and
// is retried until delivered. Storing is fast and the same for every request,
// so the response time does not reveal whether a message was sent.
type Outbox struct {
	db     *database.TenantDB
	sealer *security.Sealer
	logger *slog.Logger
}

// NewOutbox creates an Outbox.
func NewOutbox(pool *pgxpool.Pool, sealer *security.Sealer, logger *slog.Logger) *Outbox {
	return &Outbox{db: database.NewTenantDB(pool), sealer: sealer, logger: logger}
}

// Write stores a message inside tx, so it is sent only if tx commits.
func (o *Outbox) Write(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, message Message) error {
	if !kindPattern.MatchString(message.Kind) {
		return fmt.Errorf("invalid message kind %q", message.Kind)
	}
	id, err := uuid.NewV7()
	if err != nil {
		return err
	}
	plain, err := json.Marshal(message)
	if err != nil {
		return err
	}
	sealed, err := o.sealer.Seal(plain, sealContext(tenantID, id))
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO notifications.emails (tenant_id, id, kind, sealed) VALUES ($1, $2, $3, $4)`,
		tenantID, id, message.Kind, sealed); err != nil {
		return fmt.Errorf("store e-mail: %w", err)
	}
	return nil
}

// Enqueue stores a message for the store in ctx in its own transaction. A
// failure is logged: the operation that asked for the message already happened.
func (o *Outbox) Enqueue(ctx context.Context, message Message) {
	tenantID, ok := tenancy.FromContext(ctx)
	err := tenancy.ErrMissingTenant
	if ok {
		err = o.db.Run(context.WithoutCancel(ctx), func(tx pgx.Tx) error { return o.Write(ctx, tx, tenantID, message) })
	}
	if err != nil {
		o.logger.ErrorContext(ctx, "mail.enqueue_failed", "kind", message.Kind, "error", err)
	}
}

// Delivery attempts and the waits between them.
var retryDelays = []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute, time.Hour, 6 * time.Hour}

const leaseTime = 5 * time.Minute

// Dispatcher sends stored messages. Several dispatchers can run at once: each
// message is leased to one of them.
type Dispatcher struct {
	pool   *pgxpool.Pool
	sender Sender
	sealer *security.Sealer
	logger *slog.Logger
	sent   metric.Int64Counter
}

// NewDispatcher creates a Dispatcher.
func NewDispatcher(pool *pgxpool.Pool, sender Sender, sealer *security.Sealer, logger *slog.Logger) *Dispatcher {
	sent, err := otel.Meter("mail").Int64Counter("mail.messages", metric.WithDescription("Delivery attempts, by kind and outcome"))
	if err != nil {
		logger.Warn("mail.metric_failed", "error", err)
	}
	return &Dispatcher{pool: pool, sender: sender, sealer: sealer, logger: logger, sent: sent}
}

// Run sends due messages until ctx is canceled.
func (d *Dispatcher) Run(ctx context.Context, interval time.Duration) {
	for {
		count, err := d.DispatchBatch(ctx)
		if err != nil && ctx.Err() == nil {
			d.logger.ErrorContext(ctx, "mail.dispatch_failed", "error", err)
		}
		wait := interval
		if count > 0 && err == nil {
			wait = 0
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
	}
}

type claimed struct {
	TenantID uuid.UUID
	ID       uuid.UUID
	Kind     string
	Sealed   []byte
	Attempts int32
}

// DispatchBatch sends one batch of due messages and returns how many it tried.
func (d *Dispatcher) DispatchBatch(ctx context.Context) (int, error) {
	rows, err := d.pool.Query(ctx, `SELECT tenant_id, id, kind, sealed, attempts FROM notifications.claim_emails($1, $2)`, 10, leaseTime)
	if err != nil {
		return 0, fmt.Errorf("claim e-mails: %w", err)
	}
	batch, err := pgx.CollectRows(rows, pgx.RowToStructByPos[claimed])
	if err != nil {
		return 0, fmt.Errorf("read claimed e-mails: %w", err)
	}
	for _, item := range batch {
		final, sendErr := d.send(ctx, item)
		var errText *string
		var retryIn *time.Duration
		outcome := "sent"
		if sendErr != nil {
			text := redact(sendErr.Error())
			errText, outcome = &text, "failed"
			if !final && int(item.Attempts) <= len(retryDelays) {
				retryIn, outcome = &retryDelays[item.Attempts-1], "retry"
			}
			d.logger.WarnContext(ctx, "mail.send_failed", "tenant_id", item.TenantID, "email_id", item.ID, "kind", item.Kind,
				"attempt", item.Attempts, "outcome", outcome, "error", text)
		}
		if d.sent != nil {
			d.sent.Add(ctx, 1, metric.WithAttributes(attribute.String("kind", item.Kind), attribute.String("outcome", outcome)))
		}
		if _, err := d.pool.Exec(ctx, `SELECT notifications.finish_email($1, $2, $3, $4)`, item.TenantID, item.ID, errText, retryIn); err != nil {
			return len(batch), fmt.Errorf("record delivery: %w", err)
		}
	}
	return len(batch), nil
}

// send delivers one message. final reports a failure that retrying cannot fix.
func (d *Dispatcher) send(ctx context.Context, item claimed) (final bool, err error) {
	if item.Sealed == nil {
		return true, errors.New("message content is missing")
	}
	plain, err := d.sealer.Open(item.Sealed, sealContext(item.TenantID, item.ID))
	if err != nil {
		return true, err
	}
	var message Message
	if err := json.Unmarshal(plain, &message); err != nil {
		return true, fmt.Errorf("decode message: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, sendTimeout)
	defer cancel()
	return false, d.sender.Send(ctx, message)
}
