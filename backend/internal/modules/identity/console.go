package identity

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/modules/audit"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/modules/identity/internal/app"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/modules/tenants"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/authz"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/httpx"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/mail"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/tenancy"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/shared/apperr"
)

// StoreInvitations lets platform operators invite the team of a store from
// the console. The invitation goes by e-mail and is never shown to the operator.
type StoreInvitations struct {
	pool     *pgxpool.Pool
	mail     *mail.Outbox
	adminURL string
	logger   *slog.Logger
}

// NewStoreInvitations creates StoreInvitations. The pool must use the platform role.
func NewStoreInvitations(pool *pgxpool.Pool, outbox *mail.Outbox, adminURL string, logger *slog.Logger) *StoreInvitations {
	return &StoreInvitations{pool: pool, mail: outbox, adminURL: adminURL, logger: logger}
}

func (s *StoreInvitations) storeID(ctx context.Context, slug string) (uuid.UUID, error) {
	var tenantID uuid.UUID
	err := s.pool.QueryRow(ctx, `SELECT tenant_id FROM core.resolve_tenant_by_slug($1)`, slug).Scan(&tenantID)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && tenantID == tenancy.PlatformTenantID) {
		return uuid.Nil, apperr.New(apperr.KindNotFound, "TENANT_NOT_FOUND", "The store does not exist.")
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("find store: %w", err)
	}
	return tenantID, nil
}

// Invite creates a store team account and e-mails the invitation.
func (s *StoreInvitations) Invite(ctx context.Context, slug, name, email, role string) error {
	tenantID, err := s.storeID(ctx, slug)
	if err != nil {
		return err
	}
	if role == "" {
		role = app.Staff.TopRole
	}
	_, err = app.OperatorInvitation(ctx, s.pool, app.Staff, tenantID, name, email, role,
		func(ctx context.Context, tx pgx.Tx, address, token string) error {
			store, err := tenants.StoreName(ctx, tx, tenantID)
			if err != nil {
				return err
			}
			return s.mail.Write(ctx, tx, tenantID, mail.Message{
				Kind: "identity.invitation", To: address, Subject: "Convite para a equipe da loja " + store,
				Body: "Você foi convidado para a equipe da loja " + store + ". Crie sua senha no endereço abaixo, válido por 7 dias.\n\n" +
					s.adminURL + "/convite?token=" + token + "\n\nAo entrar, informe o identificador da loja: " + slug,
			})
		})
	return err
}

// RegisterPlatformRoutes adds the invitation route to the platform portal.
func (s *StoreInvitations) RegisterPlatformRoutes(router chi.Router) {
	router.Method(http.MethodPost, "/stores/{slug}/invitations", authz.Protect(s.logger, authz.Permission("stores:manage"),
		httpx.Handle(s.logger, func(w http.ResponseWriter, r *http.Request) error {
			var body struct {
				Name  string `json:"name"`
				Email string `json:"email"`
				Role  string `json:"role"`
			}
			if err := httpx.DecodeJSON(r, &body); err != nil {
				return err
			}
			principal, _ := authz.FromContext(r.Context())
			ctx := audit.WithOperator(r.Context(), principal.UserID.String())
			if err := s.Invite(ctx, chi.URLParam(r, "slug"), body.Name, body.Email, body.Role); err != nil {
				return err
			}
			w.WriteHeader(http.StatusAccepted)
			return nil
		})))
}
