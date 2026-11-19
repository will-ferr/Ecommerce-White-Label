//go:build integration

package integration

import (
	"bytes"
	"net/http"
	"strings"
	"testing"

	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/app"
	identitycli "github.com/will-ferr/Ecommerce-White-Label/backend/internal/modules/identity/cli"
)

// operator signs in a platform operator with two-step verification.
func (s *suite) operator(t *testing.T, handler http.Handler, email, role string) *apiClient {
	t.Helper()
	var out bytes.Buffer
	runner := identitycli.NewRunner(s.platform, "https://"+adminHost, "https://"+consoleHost, &out)
	if err := runner.Run(s.ctx, []string{"operator", "invite", "--email", email, "--name", "Operador", "--role", role}); err != nil {
		t.Fatal(err)
	}
	client := platformClient(t, handler)
	client.expect(http.StatusNoContent, "", client.call(http.MethodPost, "/invitation/accept", map[string]string{
		"token": tokenInLink.FindStringSubmatch(out.String())[1], "name": "Operador", "password": strongPassword,
	}))
	challenge := client.call(http.MethodPost, "/login", map[string]string{"email": email, "password": strongPassword}).str("challenge")
	setup := client.call(http.MethodPost, "/mfa/setup/start", map[string]string{"challenge": challenge})
	client.expect(http.StatusOK, "", client.call(http.MethodPost, "/mfa/setup/confirm", map[string]string{"challenge": challenge, "code": totp(t, setup.str("secret"), 0)}))
	return client
}

func TestConsoleManagesStores(t *testing.T) {
	sh := newShop(t, 1)
	s := sh.s
	admin := s.operator(t, sh.handler, "admin@plataforma.test", "platform_admin")

	t.Run("an operator creates a store and invites its owner by e-mail", func(t *testing.T) {
		created := admin.call(http.MethodPost, "/stores", map[string]string{"slug": "delta", "name": "Loja Delta", "plan": "growth"})
		admin.expect(http.StatusCreated, "", created)
		if created.str("host") != "delta."+baseDomain {
			t.Errorf("created = %v", created.body)
		}
		admin.expect(http.StatusConflict, "TENANT_ALREADY_EXISTS", admin.call(http.MethodPost, "/stores", map[string]string{"slug": "delta", "name": "Outra", "plan": "growth"}))
		admin.expect(http.StatusBadRequest, "VALIDATION_FAILED", admin.call(http.MethodPost, "/stores", map[string]string{"slug": "admin", "name": "Reservada", "plan": "growth"}))

		admin.expect(http.StatusAccepted, "", admin.call(http.MethodPost, "/stores/delta/invitations", map[string]string{"name": "Dona Delta", "email": "dona@delta.test"}))
		var tenantID string
		if err := s.admin.QueryRow(s.ctx, `SELECT id::text FROM core.tenants WHERE slug = 'delta'`).Scan(&tenantID); err != nil {
			t.Fatal(err)
		}
		messages := (&shop{s: s, t: t}).storedMailByID(tenantID)
		if len(messages) != 1 || messages[0].To != "dona@delta.test" || !strings.Contains(messages[0].Body, "https://"+adminHost+"/convite?token=") ||
			!strings.Contains(messages[0].Subject, "Loja Delta") {
			t.Fatalf("invitation e-mail = %+v", messages)
		}

		// The owner accepts and runs the store without database access.
		token := tokenInLink.FindStringSubmatch(messages[0].Body)[1]
		owner := staffClient(t, sh.handler)
		owner.expect(http.StatusNoContent, "", owner.call(http.MethodPost, "/invitation/accept", map[string]string{"token": token, "name": "Dona Delta", "password": strongPassword}))
		owner.expect(http.StatusOK, "", owner.call(http.MethodPost, "/login", map[string]string{"store": "delta", "email": "dona@delta.test", "password": strongPassword}))
		owner.expect(http.StatusOK, "", owner.call(http.MethodGet, "/store/settings", nil))

		var actor string
		if err := s.admin.QueryRow(s.ctx, `SELECT actor_id FROM audit.entries WHERE action = 'tenants.tenant.created' ORDER BY occurred_at DESC LIMIT 1`).Scan(&actor); err != nil {
			t.Fatal(err)
		}
		if actor == "tenantctl" || actor == "" {
			t.Errorf("store creation was attributed to %q, not to the operator", actor)
		}
	})

	t.Run("an operator suspends and reactivates a store", func(t *testing.T) {
		admin.expect(http.StatusNoContent, "", admin.call(http.MethodPost, "/stores/alfa/status", map[string]string{"status": "suspended"}))
		detail := admin.call(http.MethodGet, "/stores/alfa", nil)
		if detail.str("status") != "suspended" {
			t.Errorf("detail = %v", detail.body)
		}
		admin.expect(http.StatusNoContent, "", admin.call(http.MethodPost, "/stores/alfa/status", map[string]string{"status": "active"}))
		admin.expect(http.StatusBadRequest, "INVALID_STATUS", admin.call(http.MethodPost, "/stores/alfa/status", map[string]string{"status": "deleted"}))
	})

	t.Run("an operator adds a custom domain that waits for verification", func(t *testing.T) {
		added := admin.call(http.MethodPost, "/stores/alfa/domains", map[string]string{"host": "www.lojaalfa.com.br"})
		admin.expect(http.StatusCreated, "", added)
		detail := admin.call(http.MethodGet, "/stores/alfa", nil)
		domains := items(detail, "domains")
		var pending map[string]any
		for _, d := range domains {
			if d["host"] == "www.lojaalfa.com.br" {
				pending = d
			}
		}
		if pending == nil || pending["verifiedAt"] != nil || pending["verificationValue"] != added.str("verificationValue") {
			t.Fatalf("domains = %v", domains)
		}
		// The domain of one store cannot be managed through another.
		admin.expect(http.StatusNotFound, "DOMAIN_NOT_FOUND", admin.call(http.MethodDelete, "/stores/beta/domains/www.lojaalfa.com.br", nil))
		admin.expect(http.StatusNoContent, "", admin.call(http.MethodDelete, "/stores/alfa/domains/www.lojaalfa.com.br", nil))
	})

	t.Run("an operator turns features on and off within the plan", func(t *testing.T) {
		detail := admin.call(http.MethodGet, "/stores/gama", nil)
		var outside string
		for _, feature := range items(detail, "features") {
			if feature["inPlan"] == false {
				outside, _ = feature["key"].(string)
			}
		}
		if outside == "" {
			t.Skip("every feature is in the starter plan")
		}
		admin.expect(http.StatusConflict, "FEATURE_NOT_IN_PLAN", admin.call(http.MethodPut, "/stores/gama/features/"+outside, map[string]bool{"enabled": true}))
	})

	t.Run("the reserved platform tenant is not a store", func(t *testing.T) {
		admin.expect(http.StatusNotFound, "TENANT_NOT_FOUND", admin.call(http.MethodGet, "/stores/platform", nil))
		admin.expect(http.StatusNotFound, "TENANT_NOT_FOUND", admin.call(http.MethodPost, "/stores/platform/status", map[string]string{"status": "suspended"}))
		admin.expect(http.StatusNotFound, "TENANT_NOT_FOUND", admin.call(http.MethodPost, "/stores/platform/invitations", map[string]string{"name": "X", "email": "x@x.test"}))
		for _, store := range items(admin.call(http.MethodGet, "/stores", nil), "items") {
			if store["slug"] == "platform" {
				t.Error("the platform tenant is listed as a store")
			}
		}
	})

	t.Run("support operators only read", func(t *testing.T) {
		support := s.operator(t, sh.handler, "suporte@plataforma.test", "platform_support")
		support.expect(http.StatusOK, "", support.call(http.MethodGet, "/stores", nil))
		support.expect(http.StatusForbidden, "PERMISSION_DENIED", support.call(http.MethodPost, "/stores", map[string]string{"slug": "epsilon", "name": "E", "plan": "growth"}))
		support.expect(http.StatusForbidden, "PERMISSION_DENIED", support.call(http.MethodPost, "/stores/alfa/status", map[string]string{"status": "suspended"}))
	})

	t.Run("store teams and buyers cannot reach the console", func(t *testing.T) {
		sh.owner.base = "/platform"
		sh.owner.expect(http.StatusUnauthorized, "", sh.owner.call(http.MethodGet, "/stores", nil))
		sh.owner.base = "/staff"
		buyer := sh.buyer("alfa")
		buyer.base = "/platform"
		buyer.expect(http.StatusUnauthorized, "", buyer.call(http.MethodGet, "/stores", nil))
	})
}

func TestStoreAPIDoesNotServeTheConsole(t *testing.T) {
	s := newSuite(t)
	modules := s.modules(&mailbox{}, nil)
	deps := routerDeps(modules)
	deps.RegisterAPI = modules.Register
	client := platformClient(t, app.NewRouter(deps))
	client.expect(http.StatusNotFound, "ROUTE_NOT_FOUND", client.call(http.MethodGet, "/stores", nil))
	client.expect(http.StatusNotFound, "ROUTE_NOT_FOUND", client.call(http.MethodPost, "/login", map[string]string{}))
}
