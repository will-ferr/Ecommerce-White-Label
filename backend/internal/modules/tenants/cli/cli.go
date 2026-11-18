// Package cli implements the operator commands that manage stores. Until the
// authenticated console exists, this is the only way to change tenant data.
package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"text/tabwriter"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/modules/tenants/internal/adapters/postgres"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/modules/tenants/internal/app"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/modules/tenants/internal/domain"
)

// ErrUsage means the command line was not understood.
var ErrUsage = errors.New("invalid command")

// ErrNotFound makes "tenant exists" exit with failure for scripts.
var ErrNotFound = errors.New("tenant does not exist")

const usage = `Uso: tenantctl <recurso> <ação> [opções]

  tenant create   --slug S --name N [--plan starter|growth|enterprise]
  tenant list
  tenant exists   --slug S
  tenant suspend  --slug S
  tenant activate --slug S
  domain add      --slug S --host H
  domain verify   --host H
  domain remove   --host H
  settings set    --slug S [--name N] [--currency C] [--locale L] [--timezone T]
                  [--seo-title T] [--seo-description D] [--indexable true|false]
  feature set     --slug S --key K --enabled true|false
  theme publish   --slug S --file arquivo.json
  theme rollback  --slug S --version N
`

// Runner executes operator commands.
type Runner struct {
	admin *app.Admin
	out   io.Writer
}

// NewRunner wires the commands. The pool must use the platform database role.
func NewRunner(pool *pgxpool.Pool, baseDomain string, out io.Writer) *Runner {
	return &Runner{
		admin: app.NewAdmin(postgres.NewPlatformStore(pool), net.DefaultResolver, baseDomain),
		out:   out,
	}
}

// Run executes one command line.
func (r *Runner) Run(ctx context.Context, args []string) error {
	if len(args) < 2 {
		_, _ = fmt.Fprint(r.out, usage)
		return ErrUsage
	}
	command, rest := args[0]+" "+args[1], args[2:]
	handlers := map[string]func(context.Context, []string) error{
		"tenant create":   r.tenantCreate,
		"tenant list":     r.tenantList,
		"tenant exists":   r.tenantExists,
		"tenant suspend":  r.tenantStatus(domain.StatusSuspended),
		"tenant activate": r.tenantStatus(domain.StatusActive),
		"domain add":      r.domainAdd,
		"domain verify":   r.domainVerify,
		"domain remove":   r.domainRemove,
		"settings set":    r.settingsSet,
		"feature set":     r.featureSet,
		"theme publish":   r.themePublish,
		"theme rollback":  r.themeRollback,
	}
	handler, ok := handlers[command]
	if !ok {
		_, _ = fmt.Fprint(r.out, usage)
		return ErrUsage
	}
	return handler(ctx, rest)
}

func parse(name string, args []string, define func(*flag.FlagSet)) error {
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	define(flags)
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("%w: %s: %w", ErrUsage, name, err)
	}
	if flags.NArg() > 0 {
		return fmt.Errorf("%w: %s: unexpected argument %q", ErrUsage, name, flags.Arg(0))
	}
	return nil
}

func (r *Runner) tenantCreate(ctx context.Context, args []string) error {
	var slug, name, plan string
	err := parse("tenant create", args, func(f *flag.FlagSet) {
		f.StringVar(&slug, "slug", "", "")
		f.StringVar(&name, "name", "", "")
		f.StringVar(&plan, "plan", "starter", "")
	})
	if err != nil {
		return err
	}
	host, err := r.admin.CreateTenant(ctx, slug, name, plan)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(r.out, "Loja criada: %s\n", host)
	return err
}

func (r *Runner) tenantList(ctx context.Context, args []string) error {
	if err := parse("tenant list", args, func(*flag.FlagSet) {}); err != nil {
		return err
	}
	tenants, err := r.admin.ListTenants(ctx)
	if err != nil {
		return err
	}
	table := tabwriter.NewWriter(r.out, 0, 2, 2, ' ', 0)
	_, _ = fmt.Fprintln(table, "SLUG\tSTATUS\tPLANO\tNOME")
	for _, tenant := range tenants {
		_, _ = fmt.Fprintf(table, "%s\t%s\t%s\t%s\n", tenant.Slug, tenant.Status, tenant.PlanCode, tenant.StoreName)
	}
	return table.Flush()
}

func (r *Runner) tenantExists(ctx context.Context, args []string) error {
	var slug string
	if err := parse("tenant exists", args, func(f *flag.FlagSet) { f.StringVar(&slug, "slug", "", "") }); err != nil {
		return err
	}
	exists, err := r.admin.TenantExists(ctx, slug)
	if err != nil {
		return err
	}
	if !exists {
		return ErrNotFound
	}
	return nil
}

func (r *Runner) tenantStatus(status string) func(context.Context, []string) error {
	return func(ctx context.Context, args []string) error {
		var slug string
		if err := parse("tenant status", args, func(f *flag.FlagSet) { f.StringVar(&slug, "slug", "", "") }); err != nil {
			return err
		}
		if err := r.admin.SetStatus(ctx, slug, status); err != nil {
			return err
		}
		_, err := fmt.Fprintf(r.out, "Loja %s: %s\n", slug, status)
		return err
	}
}

func (r *Runner) domainAdd(ctx context.Context, args []string) error {
	var slug, host string
	err := parse("domain add", args, func(f *flag.FlagSet) {
		f.StringVar(&slug, "slug", "", "")
		f.StringVar(&host, "host", "", "")
	})
	if err != nil {
		return err
	}
	record, token, err := r.admin.AddDomain(ctx, slug, host)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(r.out, "Crie o registro DNS TXT:\n  nome:  %s\n  valor: %s\n", record, token)
	return err
}

func (r *Runner) domainVerify(ctx context.Context, args []string) error {
	var host string
	if err := parse("domain verify", args, func(f *flag.FlagSet) { f.StringVar(&host, "host", "", "") }); err != nil {
		return err
	}
	if err := r.admin.VerifyDomain(ctx, host); err != nil {
		return err
	}
	_, err := fmt.Fprintf(r.out, "Domínio verificado: %s\n", host)
	return err
}

func (r *Runner) domainRemove(ctx context.Context, args []string) error {
	var host string
	if err := parse("domain remove", args, func(f *flag.FlagSet) { f.StringVar(&host, "host", "", "") }); err != nil {
		return err
	}
	if err := r.admin.RemoveDomain(ctx, host); err != nil {
		return err
	}
	_, err := fmt.Fprintf(r.out, "Domínio removido: %s\n", host)
	return err
}

func (r *Runner) settingsSet(ctx context.Context, args []string) error {
	var slug string
	var changes []app.SettingsChange
	text := func(f *flag.FlagSet, name string, apply func(*domain.Settings, string)) {
		f.Func(name, "", func(value string) error {
			changes = append(changes, func(s *domain.Settings) { apply(s, value) })
			return nil
		})
	}
	err := parse("settings set", args, func(f *flag.FlagSet) {
		f.StringVar(&slug, "slug", "", "")
		text(f, "name", func(s *domain.Settings, v string) { s.StoreName = v })
		text(f, "currency", func(s *domain.Settings, v string) { s.Currency = v })
		text(f, "locale", func(s *domain.Settings, v string) { s.Locale = v })
		text(f, "timezone", func(s *domain.Settings, v string) { s.Timezone = v })
		text(f, "seo-title", func(s *domain.Settings, v string) { s.SEOTitle = v })
		text(f, "seo-description", func(s *domain.Settings, v string) { s.SEODescription = v })
		f.BoolFunc("indexable", "", func(value string) error {
			indexable := value == "true"
			if value != "true" && value != "false" {
				return errors.New("use true or false")
			}
			changes = append(changes, func(s *domain.Settings) { s.SEOIndexable = indexable })
			return nil
		})
	})
	if err != nil {
		return err
	}
	if err := r.admin.UpdateSettings(ctx, slug, changes...); err != nil {
		return err
	}
	_, err = fmt.Fprintf(r.out, "Configurações atualizadas: %s\n", slug)
	return err
}

func (r *Runner) featureSet(ctx context.Context, args []string) error {
	var slug, key string
	var enabled bool
	err := parse("feature set", args, func(f *flag.FlagSet) {
		f.StringVar(&slug, "slug", "", "")
		f.StringVar(&key, "key", "", "")
		f.BoolVar(&enabled, "enabled", false, "")
	})
	if err != nil {
		return err
	}
	if err := r.admin.SetFeature(ctx, slug, key, enabled); err != nil {
		return err
	}
	_, err = fmt.Fprintf(r.out, "Funcionalidade %s da loja %s: %t\n", key, slug, enabled)
	return err
}

func (r *Runner) themePublish(ctx context.Context, args []string) error {
	var slug, file string
	err := parse("theme publish", args, func(f *flag.FlagSet) {
		f.StringVar(&slug, "slug", "", "")
		f.StringVar(&file, "file", "", "")
	})
	if err != nil {
		return err
	}
	document, err := os.ReadFile(file) //nolint:gosec // The path is chosen by the operator running the tool.
	if err != nil {
		return fmt.Errorf("read theme file: %w", err)
	}
	version, err := r.admin.PublishTheme(ctx, slug, document)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(r.out, "Tema publicado: versão %d\n", version)
	return err
}

func (r *Runner) themeRollback(ctx context.Context, args []string) error {
	var slug string
	var version int
	err := parse("theme rollback", args, func(f *flag.FlagSet) {
		f.StringVar(&slug, "slug", "", "")
		f.IntVar(&version, "version", 0, "")
	})
	if err != nil {
		return err
	}
	if version > 1<<30 {
		return fmt.Errorf("%w: version is too large", ErrUsage)
	}
	if err := r.admin.RollbackTheme(ctx, slug, int32(version)); err != nil { //nolint:gosec // bounded above
		return err
	}
	_, err = fmt.Fprintf(r.out, "Tema restaurado: versão %d\n", version)
	return err
}
