//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/app"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/modules/tenants/cli"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/database"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/tenancy"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/shared/apperr"
)

const baseDomain = "plataforma.test"

type suite struct {
	ctx      context.Context
	env      environment
	runtime  *pgxpool.Pool
	platform *pgxpool.Pool
	admin    *pgxpool.Pool
	ctl      *cli.Runner
	tenants  map[string]uuid.UUID
}

func newSuite(t *testing.T) *suite {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	t.Cleanup(cancel)

	env := startPostgres(ctx, t)
	if err := database.Migrate(ctx, env.database("app_owner", ownerPassword), database.MigrateUp); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	s := &suite{
		ctx: ctx, env: env,
		runtime:  connect(ctx, t, env.database("app_runtime", runtimePassword)),
		platform: connect(ctx, t, env.database("app_platform", platformPassword)),
		admin:    connect(ctx, t, env.database("postgres", superuserPassword)),
		tenants:  map[string]uuid.UUID{},
	}
	s.ctl = cli.NewRunner(s.platform, baseDomain, io.Discard)

	s.run(t, "tenant", "create", "--slug", "alfa", "--name", "Loja Alfa", "--plan", "growth")
	s.run(t, "tenant", "create", "--slug", "beta", "--name", "Loja Beta", "--plan", "enterprise")
	s.run(t, "tenant", "create", "--slug", "gama", "--name", "Loja Gama", "--plan", "starter")

	for _, slug := range []string{"alfa", "beta", "gama"} {
		var id uuid.UUID
		if err := s.admin.QueryRow(ctx, `SELECT id FROM core.tenants WHERE slug = $1`, slug).Scan(&id); err != nil {
			t.Fatalf("load tenant %s: %v", slug, err)
		}
		s.tenants[slug] = id
	}
	return s
}

func (s *suite) run(t *testing.T, args ...string) {
	t.Helper()
	if err := s.ctl.Run(s.ctx, args); err != nil {
		t.Fatalf("tenantctl %s: %v", strings.Join(args, " "), err)
	}
}

// handler returns a new API instance, with an empty host cache.
func (s *suite) handler() http.Handler {
	return app.NewRouter(s.deps(&mailbox{}, nil))
}

type configResponse struct {
	Slug  string `json:"slug"`
	Code  string `json:"code"`
	Store struct {
		Name string `json:"name"`
	} `json:"store"`
	Theme struct {
		Version int `json:"version"`
		Tokens  struct {
			Colors struct {
				Primary string `json:"primary"`
			} `json:"colors"`
		} `json:"tokens"`
	} `json:"theme"`
	Features map[string]bool `json:"features"`
}

func requestConfig(t *testing.T, handler http.Handler, host, target string, headers map[string]string) (int, configResponse) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	req.Host = host
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	var body configResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid response %q: %v", rec.Body.String(), err)
	}
	return rec.Code, body
}

const configPath = "/api/v1/storefront/config"

// tenantTables lists every table that holds data of a store.
func (s *suite) tenantTables(t *testing.T) []string {
	t.Helper()
	rows, err := s.admin.Query(s.ctx, `
		SELECT format('%I.%I', n.nspname, c.relname)
		FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		JOIN pg_attribute a ON a.attrelid = c.oid AND a.attname = 'tenant_id' AND NOT a.attisdropped
		WHERE c.relkind IN ('r', 'p') AND n.nspname NOT IN ('pg_catalog', 'information_schema')
		ORDER BY 1`)
	if err != nil {
		t.Fatalf("list tables: %v", err)
	}
	tables, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatalf("read tables: %v", err)
	}
	if len(tables) < 4 {
		t.Fatalf("expected tenant tables, found %v", tables)
	}
	return tables
}

// runtimeReadable keeps the tables the runtime role is allowed to query.
func (s *suite) runtimeReadable(t *testing.T) []string {
	t.Helper()
	var readable []string
	for _, table := range s.tenantTables(t) {
		var allowed bool
		err := s.admin.QueryRow(s.ctx, `SELECT has_table_privilege('app_runtime', $1, 'SELECT')`, table).Scan(&allowed)
		if err != nil {
			t.Fatalf("check privilege: %v", err)
		}
		if allowed {
			readable = append(readable, table)
		}
	}
	return readable
}

func TestTenantIsolation(t *testing.T) {
	s := newSuite(t)
	ctx := s.ctx
	db := database.NewTenantDB(s.runtime)
	alfa, beta := s.tenants["alfa"], s.tenants["beta"]

	t.Run("every tenant table enforces row level security", func(t *testing.T) {
		for _, table := range append(s.tenantTables(t), "core.tenants") {
			var enabled, forced bool
			err := s.admin.QueryRow(ctx, `SELECT relrowsecurity, relforcerowsecurity FROM pg_class WHERE oid = $1::regclass`, table).
				Scan(&enabled, &forced)
			if err != nil {
				t.Fatalf("%s: %v", table, err)
			}
			if !enabled || !forced {
				t.Errorf("%s: enabled=%v forced=%v", table, enabled, forced)
			}
		}
	})

	t.Run("application roles cannot bypass row level security", func(t *testing.T) {
		rows, err := s.admin.Query(ctx, `SELECT rolname FROM pg_roles WHERE rolname LIKE 'app_%' AND (rolsuper OR rolbypassrls)`)
		if err != nil {
			t.Fatal(err)
		}
		privileged, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			t.Fatal(err)
		}
		if len(privileged) > 0 {
			t.Errorf("privileged roles: %v", privileged)
		}
	})

	t.Run("no tenant context returns no rows", func(t *testing.T) {
		for _, table := range append(s.runtimeReadable(t), "core.tenants") {
			var count int
			if err := s.runtime.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&count); err != nil {
				t.Fatalf("%s: %v", table, err)
			}
			if count != 0 {
				t.Errorf("%s exposed %d rows without a tenant", table, count)
			}
		}
	})

	t.Run("tenant context returns only its own rows", func(t *testing.T) {
		for _, table := range s.runtimeReadable(t) {
			err := db.Run(tenancy.WithTenant(ctx, alfa), func(tx pgx.Tx) error {
				var own, foreign int
				err := tx.QueryRow(ctx, fmt.Sprintf(
					`SELECT count(*) FILTER (WHERE tenant_id = $1), count(*) FILTER (WHERE tenant_id <> $1) FROM %s`, table), alfa).
					Scan(&own, &foreign)
				if err != nil {
					return err
				}
				if foreign != 0 {
					t.Errorf("%s: own=%d foreign=%d", table, own, foreign)
				}
				return nil
			})
			if err != nil {
				t.Fatalf("%s: %v", table, err)
			}
		}
	})

	t.Run("filtering by another tenant id returns nothing", func(t *testing.T) {
		err := db.Run(tenancy.WithTenant(ctx, alfa), func(tx pgx.Tx) error {
			var count int
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM core.tenant_settings WHERE tenant_id = $1`, beta).Scan(&count); err != nil {
				return err
			}
			if count != 0 {
				t.Errorf("read %d rows of another tenant", count)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	})

	t.Run("repository access requires a tenant", func(t *testing.T) {
		err := db.Run(ctx, func(pgx.Tx) error {
			t.Error("the transaction must not start")
			return nil
		})
		if !errors.Is(err, tenancy.ErrMissingTenant) {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("tenant context does not survive the transaction", func(t *testing.T) {
		singleConfig, err := pgxpool.ParseConfig(s.env.database("app_runtime", runtimePassword).DSN())
		if err != nil {
			t.Fatal(err)
		}
		singleConfig.MaxConns = 1
		single, err := pgxpool.NewWithConfig(ctx, singleConfig)
		if err != nil {
			t.Fatal(err)
		}
		defer single.Close()
		if err := database.NewTenantDB(single).Run(tenancy.WithTenant(ctx, alfa), func(pgx.Tx) error { return nil }); err != nil {
			t.Fatal(err)
		}
		for range 20 {
			var count int
			if err := single.QueryRow(ctx, `SELECT count(*) FROM core.tenant_settings`).Scan(&count); err != nil {
				t.Fatal(err)
			}
			if count != 0 {
				t.Fatalf("context leaked to the next query: %d rows", count)
			}
		}
	})

	t.Run("malformed tenant context fails instead of matching rows", func(t *testing.T) {
		err := pgx.BeginFunc(ctx, s.runtime, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, `SELECT set_config('app.tenant_id', 'nao-e-uuid', true)`); err != nil {
				return err
			}
			var count int
			return tx.QueryRow(ctx, `SELECT count(*) FROM core.tenant_settings`).Scan(&count)
		})
		if err == nil {
			t.Error("a malformed tenant id must fail")
		}
	})

	t.Run("runtime role cannot change or read restricted data", func(t *testing.T) {
		statements := []string{
			`SELECT * FROM core.tenant_domains`,
			`UPDATE core.tenants SET status = 'suspended'`,
			`UPDATE core.tenant_settings SET currency = 'USD'`,
			`UPDATE core.theme_versions SET tokens = '{}'`,
			`DELETE FROM core.theme_versions`,
			`INSERT INTO core.tenant_features VALUES ('` + beta.String() + `', 'reviews', false)`,
			`UPDATE core.feature_flags SET enabled = false`,
			`INSERT INTO core.plans (id, code, name) VALUES (gen_random_uuid(), 'gratis', 'Gratis')`,
		}
		for _, statement := range statements {
			err := db.Run(tenancy.WithTenant(ctx, alfa), func(tx pgx.Tx) error {
				_, err := tx.Exec(ctx, statement)
				return err
			})
			if err == nil {
				t.Errorf("statement was allowed: %s", statement)
			}
		}
	})

	t.Run("a store changes only its own name", func(t *testing.T) {
		var changed int64
		err := db.Run(tenancy.WithTenant(ctx, alfa), func(tx pgx.Tx) error {
			tag, err := tx.Exec(ctx, `UPDATE core.tenant_settings SET store_name = 'Invadida' WHERE tenant_id = $1`, beta)
			changed = tag.RowsAffected()
			return err
		})
		if err != nil || changed != 0 {
			t.Errorf("store alfa changed %d rows of store beta (%v)", changed, err)
		}
	})

	t.Run("platform role is restricted to tenant management", func(t *testing.T) {
		for _, statement := range []string{
			`CREATE TABLE core.intruder (id int)`,
			`DROP TABLE core.tenant_settings`,
			`DELETE FROM core.tenants`,
			`UPDATE core.plans SET name = 'x'`,
			`ALTER TABLE core.tenants DISABLE ROW LEVEL SECURITY`,
		} {
			if _, err := s.platform.Exec(ctx, statement); err == nil {
				t.Errorf("statement was allowed: %s", statement)
			}
		}
	})
}

func TestStoreResolutionOverHTTP(t *testing.T) {
	s := newSuite(t)
	handler := s.handler()
	alfaHost, betaHost := "alfa."+baseDomain, "beta."+baseDomain

	themePath := writeTheme(t, "#7a1f4b")
	s.run(t, "theme", "publish", "--slug", "beta", "--file", themePath)

	t.Run("each host receives its own store", func(t *testing.T) {
		code, alfa := requestConfig(t, handler, alfaHost, configPath, nil)
		if code != http.StatusOK || alfa.Slug != "alfa" || alfa.Store.Name != "Loja Alfa" || alfa.Theme.Version != 1 {
			t.Errorf("alfa: %d %+v", code, alfa)
		}
		code, beta := requestConfig(t, handler, betaHost, configPath, nil)
		if code != http.StatusOK || beta.Slug != "beta" || beta.Theme.Tokens.Colors.Primary != "#7a1f4b" || beta.Theme.Version != 2 {
			t.Errorf("beta: %d %+v", code, beta)
		}
		if alfa.Theme.Tokens.Colors.Primary == beta.Theme.Tokens.Colors.Primary {
			t.Error("stores must keep independent themes")
		}
	})

	t.Run("client supplied tenant values are ignored", func(t *testing.T) {
		attempts := map[string]map[string]string{
			"tenant header":  {"X-Tenant-ID": s.tenants["beta"].String()},
			"slug header":    {"X-Tenant": "beta"},
			"forwarded host": {"X-Forwarded-Host": betaHost},
			"original host":  {"X-Original-Host": betaHost},
			"forwarded":      {"Forwarded": "host=" + betaHost},
		}
		for name, headers := range attempts {
			t.Run(name, func(t *testing.T) {
				code, body := requestConfig(t, handler, alfaHost, configPath, headers)
				if code != http.StatusOK || body.Slug != "alfa" {
					t.Errorf("got %d %+v", code, body)
				}
			})
		}
		target := configPath + "?tenant_id=" + s.tenants["beta"].String() + "&slug=beta"
		if code, body := requestConfig(t, handler, alfaHost, target, nil); code != http.StatusOK || body.Slug != "alfa" {
			t.Errorf("query parameters changed the tenant: %d %+v", code, body)
		}
	})

	t.Run("hosts without a verified store are refused", func(t *testing.T) {
		s.run(t, "domain", "add", "--slug", "alfa", "--host", "loja-alfa.invalid")
		for _, host := range []string{"desconhecida." + baseDomain, "loja-alfa.invalid", "127.0.0.1:8080", "localhost", "api:8080", ""} {
			code, body := requestConfig(t, handler, host, configPath, nil)
			if code != http.StatusNotFound || body.Code != "STORE_NOT_FOUND" {
				t.Errorf("host %q: %d %+v", host, code, body)
			}
		}
	})

	t.Run("host matching ignores case and port", func(t *testing.T) {
		code, body := requestConfig(t, handler, "ALFA."+strings.ToUpper(baseDomain)+":8443", configPath, nil)
		if code != http.StatusOK || body.Slug != "alfa" {
			t.Errorf("got %d %+v", code, body)
		}
	})

	t.Run("concurrent requests never mix stores", func(t *testing.T) {
		var wg sync.WaitGroup
		errs := make(chan string, 2000)
		for i := range 2000 {
			wg.Go(func() {
				host, want := alfaHost, "alfa"
				if i%2 == 0 {
					host, want = betaHost, "beta"
				}
				req := httptest.NewRequest(http.MethodGet, configPath, nil)
				req.Host = host
				rec := httptest.NewRecorder()
				handler.ServeHTTP(rec, req)
				var body configResponse
				if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.Slug != want {
					errs <- fmt.Sprintf("host %s got %d %s", host, rec.Code, rec.Body.String())
				}
			})
		}
		wg.Wait()
		close(errs)
		for message := range errs {
			t.Error(message)
		}
	})

	t.Run("suspended store stops serving and can return", func(t *testing.T) {
		s.run(t, "tenant", "suspend", "--slug", "gama")
		host := "gama." + baseDomain
		code, body := requestConfig(t, s.handler(), host, configPath, nil)
		if code != http.StatusForbidden || body.Code != "STORE_UNAVAILABLE" {
			t.Errorf("suspended: %d %+v", code, body)
		}
		s.run(t, "tenant", "activate", "--slug", "gama")
		if code, _ := requestConfig(t, s.handler(), host, configPath, nil); code != http.StatusOK {
			t.Errorf("reactivated: %d", code)
		}
	})

	t.Run("status endpoint does not depend on a store", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/status", nil)
		req.Host = "desconhecida.invalid"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("status = %d", rec.Code)
		}
	})
}

func TestTenantManagement(t *testing.T) {
	s := newSuite(t)
	alfaHost := "alfa." + baseDomain

	expectCode := func(t *testing.T, code string, args ...string) {
		t.Helper()
		err := s.ctl.Run(s.ctx, args)
		appErr, ok := apperr.As(err)
		if !ok || appErr.Code != code {
			t.Errorf("tenantctl %s: err = %v, want %s", strings.Join(args, " "), err, code)
		}
	}

	t.Run("feature flags follow plan, store and platform", func(t *testing.T) {
		_, alfa := requestConfig(t, s.handler(), alfaHost, configPath, nil)
		_, beta := requestConfig(t, s.handler(), "beta."+baseDomain, configPath, nil)
		if alfa.Features["marketplace"] || !beta.Features["marketplace"] {
			t.Errorf("plan rule: alfa=%v beta=%v", alfa.Features, beta.Features)
		}

		expectCode(t, "FEATURE_NOT_IN_PLAN", "feature", "set", "--slug", "alfa", "--key", "marketplace", "--enabled=true")

		s.run(t, "feature", "set", "--slug", "alfa", "--key", "wishlist", "--enabled=false")
		_, alfa = requestConfig(t, s.handler(), alfaHost, configPath, nil)
		_, beta = requestConfig(t, s.handler(), "beta."+baseDomain, configPath, nil)
		if alfa.Features["wishlist"] || !beta.Features["wishlist"] {
			t.Errorf("a store changed another store: alfa=%v beta=%v", alfa.Features, beta.Features)
		}

		if _, err := s.admin.Exec(s.ctx, `UPDATE core.feature_flags SET enabled = false WHERE key = 'reviews'`); err != nil {
			t.Fatal(err)
		}
		_, beta = requestConfig(t, s.handler(), "beta."+baseDomain, configPath, nil)
		if beta.Features["reviews"] {
			t.Error("the platform switch must disable the feature everywhere")
		}
	})

	t.Run("invalid theme keeps the published version", func(t *testing.T) {
		bad := filepath.Join(t.TempDir(), "bad.json")
		if err := os.WriteFile(bad, []byte(`{"customCss":"body{display:none}"}`), 0o600); err != nil {
			t.Fatal(err)
		}
		expectCode(t, "VALIDATION_FAILED", "theme", "publish", "--slug", "alfa", "--file", bad)
		if _, body := requestConfig(t, s.handler(), alfaHost, configPath, nil); body.Theme.Version != 1 {
			t.Errorf("version = %d", body.Theme.Version)
		}
	})

	t.Run("theme can be rolled back", func(t *testing.T) {
		s.run(t, "theme", "publish", "--slug", "alfa", "--file", writeTheme(t, "#7a1f4b"))
		s.run(t, "theme", "rollback", "--slug", "alfa", "--version", "1")
		if _, body := requestConfig(t, s.handler(), alfaHost, configPath, nil); body.Theme.Version != 1 {
			t.Errorf("version = %d", body.Theme.Version)
		}
		expectCode(t, "THEME_VERSION_NOT_FOUND", "theme", "rollback", "--slug", "alfa", "--version", "99")
		if _, body := requestConfig(t, s.handler(), alfaHost, configPath, nil); body.Theme.Version != 1 {
			t.Errorf("a failed rollback must change nothing, version = %d", body.Theme.Version)
		}
	})

	t.Run("duplicates are refused", func(t *testing.T) {
		expectCode(t, "TENANT_ALREADY_EXISTS", "tenant", "create", "--slug", "alfa", "--name", "Outra")
		s.run(t, "domain", "add", "--slug", "alfa", "--host", "disputado.invalid")
		expectCode(t, "DOMAIN_ALREADY_EXISTS", "domain", "add", "--slug", "beta", "--host", "disputado.invalid")
	})

	t.Run("unverified domain cannot be activated without its DNS record", func(t *testing.T) {
		s.run(t, "domain", "add", "--slug", "alfa", "--host", "sem-registro.invalid")
		expectCode(t, "DOMAIN_NOT_VERIFIED", "domain", "verify", "--host", "sem-registro.invalid")
	})

	t.Run("removed domain stops resolving", func(t *testing.T) {
		const host = "removido.example.test"
		s.run(t, "domain", "add", "--slug", "alfa", "--host", host)
		if _, err := s.admin.Exec(s.ctx, `UPDATE core.tenant_domains SET verified_at = now() WHERE host = $1`, host); err != nil {
			t.Fatal(err)
		}
		if code, body := requestConfig(t, s.handler(), host, configPath, nil); code != http.StatusOK || body.Slug != "alfa" {
			t.Fatalf("verified domain: %d %+v", code, body)
		}
		s.run(t, "domain", "remove", "--host", host)
		if code, _ := requestConfig(t, s.handler(), host, configPath, nil); code != http.StatusNotFound {
			t.Errorf("removed domain: %d", code)
		}
	})

	t.Run("platform subdomain cannot be removed", func(t *testing.T) {
		expectCode(t, "DOMAIN_NOT_FOUND", "domain", "remove", "--host", alfaHost)
	})

	t.Run("settings changes are validated", func(t *testing.T) {
		expectCode(t, "VALIDATION_FAILED", "settings", "set", "--slug", "alfa", "--currency", "REAL")
		s.run(t, "settings", "set", "--slug", "alfa", "--name", "Alfa Renovada", "--indexable=true")
		if _, body := requestConfig(t, s.handler(), alfaHost, configPath, nil); body.Store.Name != "Alfa Renovada" {
			t.Errorf("name = %q", body.Store.Name)
		}
	})

	t.Run("tenant list shows every store", func(t *testing.T) {
		var out bytes.Buffer
		if err := cli.NewRunner(s.platform, baseDomain, &out).Run(s.ctx, []string{"tenant", "list"}); err != nil {
			t.Fatal(err)
		}
		for _, slug := range []string{"alfa", "beta", "gama"} {
			if !strings.Contains(out.String(), slug) {
				t.Errorf("%s missing from %s", slug, out.String())
			}
		}
	})
}

func writeTheme(t *testing.T, primary string) string {
	t.Helper()
	document := fmt.Sprintf(`{
	  "colors": {"canvas":"#fbf5f7","surface":"#ffffff","ink":"#2a1523","muted":"#6b4f5f","line":"#ecd9e2",
	             "primary":%q,"primaryStrong":"#5f1039","onPrimary":"#ffffff"},
	  "typography": {"body":"humanist","display":"serif"},
	  "shape": {"radius":"large"},
	  "spacing": {"density":"spacious"},
	  "layout": {"header":"centered"},
	  "assets": {"logoUrl":"","faviconUrl":""}
	}`, primary)
	path := filepath.Join(t.TempDir(), "theme.json")
	if err := os.WriteFile(path, []byte(document), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
