package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/modules/identity/internal/domain"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/modules/identity/internal/sqlcgen"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/authz"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/shared/apperr"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/shared/pagination"
)

// AccountData is what the platform keeps about a customer account, for the
// data portability right of the LGPD (art. 18, V).
type AccountData struct {
	Name            string            `json:"name"`
	Email           string            `json:"email"`
	CreatedAt       time.Time         `json:"createdAt"`
	EmailVerifiedAt *time.Time        `json:"emailVerifiedAt"`
	TwoStepEnabled  bool              `json:"twoStepEnabled"`
	AccessHistory   []AccessEventView `json:"accessHistory"`
}

// ExportAccount returns the personal data of the signed-in customer.
func (s *Service) ExportAccount(ctx context.Context, principal authz.Principal) (AccountData, error) {
	var data AccountData
	err := s.in(ctx, func(tx pgx.Tx, q *sqlcgen.Queries) error {
		user, err := q.GetUserByID(ctx, sqlcgen.GetUserByIDParams{TenantID: principal.TenantID, ID: principal.UserID})
		if err != nil {
			return fmt.Errorf("load user: %w", err)
		}
		if user.Realm != domain.RealmCustomer {
			return apperr.New(apperr.KindForbidden, "PERMISSION_DENIED", "This action is not allowed.")
		}
		data = AccountData{
			Name: user.Name, Email: user.Email, CreatedAt: user.CreatedAt, EmailVerifiedAt: user.EmailVerifiedAt,
			TwoStepEnabled: user.MfaEnabledAt != nil, AccessHistory: []AccessEventView{},
		}
		events, err := q.ListAccessEvents(ctx, sqlcgen.ListAccessEventsParams{TenantID: user.TenantID, UserID: &user.ID, Limit: 200})
		if err != nil {
			return fmt.Errorf("list access events: %w", err)
		}
		for _, event := range events {
			data.AccessHistory = append(data.AccessHistory, AccessEventView{Event: event.Event, Device: event.Device, IP: event.Ip, OccurredAt: event.OccurredAt})
		}
		return selfAudit(ctx, tx, user.TenantID, user.ID, "privacy.data_exported")
	})
	return data, err
}

// EraseHook removes the data other modules keep about the customer, inside
// the transaction that erases the account.
type EraseHook func(ctx context.Context, tx pgx.Tx) error

// EraseAccount removes the personal data of the signed-in customer after
// confirming the password (LGPD art. 18, VI). Orders stay, with the name and
// e-mail they were placed with, because tax law requires keeping them.
func (s *Service) EraseAccount(ctx context.Context, principal authz.Principal, password string, hook EraseHook) error {
	user, err := s.loadPrincipalUser(ctx, principal)
	if err != nil {
		return err
	}
	if user.Realm != domain.RealmCustomer {
		return apperr.New(apperr.KindForbidden, "PERMISSION_DENIED", "This action is not allowed.")
	}
	if err := s.confirmPassword(ctx, user, password); err != nil {
		return err
	}
	return s.in(ctx, func(tx pgx.Tx, q *sqlcgen.Queries) error {
		reason := "account_erased"
		if _, err := q.RevokeAllSessions(ctx, sqlcgen.RevokeAllSessionsParams{TenantID: user.TenantID, UserID: user.ID, RevokedReason: &reason}); err != nil {
			return fmt.Errorf("revoke sessions: %w", err)
		}
		if err := q.DeleteRecoveryCodes(ctx, sqlcgen.DeleteRecoveryCodesParams{TenantID: user.TenantID, UserID: user.ID}); err != nil {
			return fmt.Errorf("delete recovery codes: %w", err)
		}
		if err := q.InvalidateAllUserTokens(ctx, sqlcgen.InvalidateAllUserTokensParams{TenantID: user.TenantID, UserID: user.ID}); err != nil {
			return fmt.Errorf("invalidate tokens: %w", err)
		}
		if hook != nil {
			if err := hook(ctx, tx); err != nil {
				return err
			}
		}
		affected, err := q.EraseUser(ctx, sqlcgen.EraseUserParams{TenantID: user.TenantID, ID: user.ID})
		if err != nil {
			return fmt.Errorf("erase user: %w", err)
		}
		if affected == 0 {
			return apperr.New(apperr.KindConflict, "ACCOUNT_ALREADY_ERASED", "This account was already erased.")
		}
		return selfAudit(ctx, tx, user.TenantID, user.ID, "privacy.account_erased")
	})
}

// CustomerSummary is a customer account as the store team sees it.
type CustomerSummary struct {
	ID            uuid.UUID `json:"id"`
	Name          string    `json:"name"`
	Email         string    `json:"email"`
	Status        string    `json:"status"`
	EmailVerified bool      `json:"emailVerified"`
	CreatedAt     time.Time `json:"createdAt"`
}

// Customers lists the customer accounts of the store, newest first. Erased
// accounts are not listed.
func (s *Service) Customers(ctx context.Context, principal authz.Principal, search, cursor string) (pagination.Page[CustomerSummary], error) {
	page := pagination.Page[CustomerSummary]{Items: []CustomerSummary{}}
	after, err := pagination.Decode(cursor)
	if err != nil {
		return page, err
	}
	params := sqlcgen.ListCustomersParams{TenantID: principal.TenantID, RowLimit: pagination.DefaultLimit + 1}
	if text := strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(strings.TrimSpace(search)); text != "" && len(text) <= 100 {
		params.Search = &text
	}
	if after != nil {
		params.AfterAt, params.AfterID = &after.At, &after.ID
	}
	err = s.in(ctx, func(_ pgx.Tx, q *sqlcgen.Queries) error {
		rows, err := q.ListCustomers(ctx, params)
		if err != nil {
			return fmt.Errorf("list customers: %w", err)
		}
		if len(rows) > pagination.DefaultLimit {
			rows = rows[:pagination.DefaultLimit]
			last := rows[len(rows)-1]
			page.NextCursor = pagination.Cursor{At: last.CreatedAt, ID: last.ID}.Encode()
		}
		for _, row := range rows {
			page.Items = append(page.Items, CustomerSummary{
				ID: row.ID, Name: row.Name, Email: row.Email, Status: row.Status, EmailVerified: row.EmailVerifiedAt != nil, CreatedAt: row.CreatedAt,
			})
		}
		return nil
	})
	return page, err
}
