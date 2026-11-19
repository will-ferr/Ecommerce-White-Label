// Package cli implements the operator commands that create the first accounts.
package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/modules/identity/internal/app"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/shared/apperr"
)

// Runner executes the account commands of the operator tool.
type Runner struct {
	pool       *pgxpool.Pool
	adminURL   string
	consoleURL string
	out        io.Writer
}

// NewRunner creates a Runner. The pool must use the platform database role.
func NewRunner(pool *pgxpool.Pool, adminURL, consoleURL string, out io.Writer) *Runner {
	return &Runner{pool: pool, adminURL: adminURL, consoleURL: consoleURL, out: out}
}

// Handles reports whether a command line belongs to this package.
func Handles(args []string) bool {
	return len(args) >= 2 && args[1] == "invite" && (args[0] == "staff" || args[0] == "operator")
}

// Run executes "staff invite" or "operator invite".
func (r *Runner) Run(ctx context.Context, args []string) error {
	var slug, email, name, role string
	flags := flag.NewFlagSet(args[0]+" invite", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&slug, "slug", "", "")
	flags.StringVar(&email, "email", "", "")
	flags.StringVar(&name, "name", "", "")
	flags.StringVar(&role, "role", "", "")
	if err := flags.Parse(args[2:]); err != nil {
		return fmt.Errorf("invalid command: %w", err)
	}

	portal, tenantID, base := app.Platform, app.PlatformTenantID, r.consoleURL
	if args[0] == "staff" {
		portal, base = app.Staff, r.adminURL
		var err error
		if tenantID, err = r.storeID(ctx, slug); err != nil {
			return err
		}
	}
	if role == "" {
		role = portal.TopRole
	}

	token, err := app.OperatorInvitation(ctx, r.pool, portal, tenantID, name, email, role)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(r.out, "Convite criado. Acesse em até 7 dias:\n%s/convite?token=%s\n", base, token)
	return err
}

func (r *Runner) storeID(ctx context.Context, slug string) (uuid.UUID, error) {
	var tenantID uuid.UUID
	err := r.pool.QueryRow(ctx, `SELECT tenant_id FROM core.resolve_tenant_by_slug($1)`, slug).Scan(&tenantID)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, apperr.New(apperr.KindNotFound, "TENANT_NOT_FOUND", "The store does not exist.")
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("find store: %w", err)
	}
	return tenantID, nil
}
