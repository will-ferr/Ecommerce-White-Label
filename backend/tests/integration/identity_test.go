//go:build integration

package integration

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base32"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/app"
	identitycli "github.com/will-ferr/Ecommerce-White-Label/backend/internal/modules/identity/cli"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/authz"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/config"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/health"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/mail"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/ratelimit"
)

const (
	strongPassword = "cavalo correto bateria grampo"
	newPassword    = "outra frase longa e dificil"
	adminHost      = "admin." + baseDomain
	consoleHost    = "console." + baseDomain
)

type mailbox struct {
	mu       sync.Mutex
	messages []mail.Message
}

func (m *mailbox) Enqueue(_ context.Context, message mail.Message) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages = append(m.messages, message)
}

var tokenInLink = regexp.MustCompile(`token=([^\s]+)`)

// lastToken returns the token of the most recent e-mail sent to an address.
func (m *mailbox) lastToken(t *testing.T, to string) string {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := len(m.messages) - 1; i >= 0; i-- {
		if m.messages[i].To == to {
			if match := tokenInLink.FindStringSubmatch(m.messages[i].Body); match != nil {
				return match[1]
			}
		}
	}
	t.Fatalf("no e-mail with a token was sent to %s", to)
	return ""
}

func (m *mailbox) count(to string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	total := 0
	for _, message := range m.messages {
		if message.To == to {
			total++
		}
	}
	return total
}

func testAuthConfig() config.Auth {
	return config.Auth{
		SigningSeed: []byte("test-signing-key-with-32-bytes-0"),
		MFAKey:      []byte("test-mfa-key-with-exact-32-bytes"),
		Links: config.PublicLinks{
			StoreScheme: "https", AdminURL: "https://" + adminHost, ConsoleURL: "https://" + consoleHost,
		},
	}
}

func (s *suite) modules(mailer *mailbox, limiter *ratelimit.Limiter) *app.Modules {
	modules, err := app.BuildModules(app.ModuleDeps{
		Pool: s.runtime, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Limiter: limiter, Mailer: mailer,
		Auth: testAuthConfig(), Commerce: config.Commerce{PaymentTTL: 30 * time.Minute, TestPayments: true},
		PlatformPool: s.platform, BaseDomain: baseDomain,
	})
	if err != nil {
		panic(err)
	}
	return modules
}

func (s *suite) deps(mailer *mailbox, limiter *ratelimit.Limiter) app.RouterDeps {
	return routerDeps(s.modules(mailer, limiter))
}

func routerDeps(modules *app.Modules) app.RouterDeps {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return app.RouterDeps{
		Logger: logger, Health: health.NewService(logger, "test", time.Second),
		RequestTimeout: 10 * time.Second, MaxBodyBytes: 1 << 16, RegisterAPI: func(api chi.Router) {
			// The store API and the platform API are separate processes; tests serve both.
			modules.Register(api)
			if modules.RegisterPlatform != nil {
				modules.RegisterPlatform(api)
			}
		},
	}
}

type apiClient struct {
	t       *testing.T
	handler http.Handler
	host    string
	base    string
	token   string
	cookie  string
	cart    string
	headers map[string]string
}

type apiResponse struct {
	code   int
	body   map[string]any
	header http.Header
}

func (r apiResponse) str(key string) string {
	value, _ := r.body[key].(string)
	return value
}

func (c *apiClient) call(method, path string, body any) apiResponse {
	c.t.Helper()
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			c.t.Fatal(err)
		}
		reader = bytes.NewReader(encoded)
	}
	req := httptest.NewRequest(method, "/api/v1"+c.base+path, reader)
	req.Host = c.host
	req.Header.Set("Origin", "https://"+c.host)
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	if c.cookie != "" {
		req.AddCookie(&http.Cookie{Name: "wl_refresh", Value: c.cookie})
	}
	if c.cart != "" {
		req.AddCookie(&http.Cookie{Name: "wl_cart", Value: c.cart})
	}
	for name, value := range c.headers {
		req.Header.Set(name, value)
	}
	rec := httptest.NewRecorder()
	c.handler.ServeHTTP(rec, req)

	response := apiResponse{code: rec.Code, body: map[string]any{}, header: rec.Header()}
	if rec.Body.Len() > 0 {
		if err := json.Unmarshal(rec.Body.Bytes(), &response.body); err != nil {
			c.t.Fatalf("invalid response %q: %v", rec.Body.String(), err)
		}
	}
	for _, cookie := range rec.Result().Cookies() {
		switch cookie.Name {
		case "wl_refresh":
			c.cookie = cookie.Value
		case "wl_cart":
			c.cart = cookie.Value
		}
	}
	if token := response.str("accessToken"); token != "" {
		c.token = token
	}
	return response
}

func (c *apiClient) expect(want int, wantCode string, response apiResponse) {
	c.t.Helper()
	if response.code != want || (wantCode != "" && response.str("code") != wantCode) {
		c.t.Fatalf("got %d %v, want %d %s", response.code, response.body, want, wantCode)
	}
}

func customerClient(t *testing.T, handler http.Handler, slug string) *apiClient {
	return &apiClient{t: t, handler: handler, host: slug + "." + baseDomain, base: "/auth"}
}

func staffClient(t *testing.T, handler http.Handler) *apiClient {
	return &apiClient{t: t, handler: handler, host: adminHost, base: "/staff"}
}

func platformClient(t *testing.T, handler http.Handler) *apiClient {
	return &apiClient{t: t, handler: handler, host: consoleHost, base: "/platform"}
}

func (c *apiClient) signUp(mailer *mailbox, email string) {
	c.t.Helper()
	c.expect(http.StatusAccepted, "", c.call(http.MethodPost, "/register", map[string]string{
		"name": "Maria", "email": email, "password": strongPassword,
	}))
	c.expect(http.StatusOK, "", c.call(http.MethodPost, "/login", map[string]string{"email": email, "password": strongPassword}))
}

// totp computes a code independently of the production implementation.
func totp(t *testing.T, secret string, offset int64) string {
	t.Helper()
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secret)
	if err != nil {
		t.Fatal(err)
	}
	var counter [8]byte
	binary.BigEndian.PutUint64(counter[:], uint64(time.Now().Unix()/30+offset))
	mac := hmac.New(sha1.New, key)
	mac.Write(counter[:])
	sum := mac.Sum(nil)
	position := sum[len(sum)-1] & 0x0f
	return fmt.Sprintf("%06d", (binary.BigEndian.Uint32(sum[position:position+4])&0x7fffffff)%1_000_000)
}

// inviteStaff creates an active staff account through the operator tool.
func (s *suite) inviteStaff(t *testing.T, handler http.Handler, slug, email, role string) *apiClient {
	t.Helper()
	var out bytes.Buffer
	runner := identitycli.NewRunner(s.platform, "https://"+adminHost, "https://"+consoleHost, &out)
	if err := runner.Run(s.ctx, []string{"staff", "invite", "--slug", slug, "--email", email, "--name", "Equipe", "--role", role}); err != nil {
		t.Fatalf("invite: %v", err)
	}
	match := tokenInLink.FindStringSubmatch(out.String())
	if match == nil {
		t.Fatalf("no token in %q", out.String())
	}
	client := staffClient(t, handler)
	client.expect(http.StatusNoContent, "", client.call(http.MethodPost, "/invitation/accept", map[string]string{
		"token": match[1], "name": "Equipe", "password": strongPassword,
	}))
	client.expect(http.StatusOK, "", client.call(http.MethodPost, "/login", map[string]string{
		"store": slug, "email": email, "password": strongPassword,
	}))
	return client
}

func TestCustomerAccountLifecycle(t *testing.T) {
	s := newSuite(t)
	mailer := &mailbox{}
	handler := app.NewRouter(s.deps(mailer, nil))
	const email = "maria@exemplo.com"

	client := customerClient(t, handler, "alfa")
	client.signUp(mailer, email)

	t.Run("profile belongs to the caller", func(t *testing.T) {
		me := client.call(http.MethodGet, "/me", nil)
		client.expect(http.StatusOK, "", me)
		if me.str("email") != email || me.body["emailVerified"] != false {
			t.Errorf("me = %v", me.body)
		}
	})

	t.Run("e-mail verification works once", func(t *testing.T) {
		token := mailer.lastToken(t, email)
		client.expect(http.StatusNoContent, "", client.call(http.MethodPost, "/email/verify", map[string]string{"token": token}))
		client.expect(http.StatusBadRequest, "INVALID_TOKEN", client.call(http.MethodPost, "/email/verify", map[string]string{"token": token}))
		if me := client.call(http.MethodGet, "/me", nil); me.body["emailVerified"] != true {
			t.Errorf("me = %v", me.body)
		}
	})

	t.Run("duplicate registration reveals nothing", func(t *testing.T) {
		before := mailer.count(email)
		anonymous := customerClient(t, handler, "alfa")
		anonymous.expect(http.StatusAccepted, "", anonymous.call(http.MethodPost, "/register", map[string]string{
			"name": "Outra", "email": email, "password": strongPassword,
		}))
		if mailer.count(email) != before+1 {
			t.Error("the owner of the address must be warned")
		}
		login := customerClient(t, handler, "alfa")
		login.expect(http.StatusOK, "", login.call(http.MethodPost, "/login", map[string]string{"email": email, "password": strongPassword}))
	})

	t.Run("wrong password and unknown account are indistinguishable", func(t *testing.T) {
		anonymous := customerClient(t, handler, "alfa")
		wrong := anonymous.call(http.MethodPost, "/login", map[string]string{"email": email, "password": "senha errada aqui"})
		unknown := anonymous.call(http.MethodPost, "/login", map[string]string{"email": "ninguem@exemplo.com", "password": "senha errada aqui"})
		anonymous.expect(http.StatusUnauthorized, "INVALID_CREDENTIALS", wrong)
		anonymous.expect(http.StatusUnauthorized, "INVALID_CREDENTIALS", unknown)
		if wrong.str("detail") != unknown.str("detail") {
			t.Errorf("messages differ: %v / %v", wrong.body, unknown.body)
		}
	})

	t.Run("weak passwords are refused", func(t *testing.T) {
		anonymous := customerClient(t, handler, "alfa")
		for _, password := range []string{"curta", "senha123456", "1234567890"} {
			anonymous.expect(http.StatusBadRequest, "VALIDATION_FAILED", anonymous.call(http.MethodPost, "/register", map[string]string{
				"name": "Fraca", "email": "fraca@exemplo.com", "password": password,
			}))
		}
	})

	t.Run("unknown request fields are refused", func(t *testing.T) {
		anonymous := customerClient(t, handler, "alfa")
		anonymous.expect(http.StatusBadRequest, "INVALID_BODY", anonymous.call(http.MethodPost, "/register", map[string]any{
			"name": "Maria", "email": "x@exemplo.com", "password": strongPassword, "role": "owner", "tenant_id": "x",
		}))
	})

	t.Run("password reset ends every session and works once", func(t *testing.T) {
		session := customerClient(t, handler, "alfa")
		session.expect(http.StatusOK, "", session.call(http.MethodPost, "/login", map[string]string{"email": email, "password": strongPassword}))

		anonymous := customerClient(t, handler, "alfa")
		anonymous.expect(http.StatusAccepted, "", anonymous.call(http.MethodPost, "/password/forgot", map[string]string{"email": email}))
		before := len(mailer.messages)
		anonymous.expect(http.StatusAccepted, "", anonymous.call(http.MethodPost, "/password/forgot", map[string]string{"email": "ninguem@exemplo.com"}))
		if len(mailer.messages) != before {
			t.Error("unknown accounts must not receive e-mail")
		}

		token := mailer.lastToken(t, email)
		anonymous.expect(http.StatusNoContent, "", anonymous.call(http.MethodPost, "/password/reset", map[string]string{"token": token, "password": newPassword}))
		anonymous.expect(http.StatusBadRequest, "INVALID_TOKEN", anonymous.call(http.MethodPost, "/password/reset", map[string]string{"token": token, "password": strongPassword}))

		session.expect(http.StatusUnauthorized, "SESSION_EXPIRED", session.call(http.MethodGet, "/me", nil))
		anonymous.expect(http.StatusUnauthorized, "INVALID_CREDENTIALS", anonymous.call(http.MethodPost, "/login", map[string]string{"email": email, "password": strongPassword}))
		anonymous.expect(http.StatusOK, "", anonymous.call(http.MethodPost, "/login", map[string]string{"email": email, "password": newPassword}))
	})

	t.Run("reset link of one store does not work on another", func(t *testing.T) {
		alfa := customerClient(t, handler, "alfa")
		alfa.expect(http.StatusAccepted, "", alfa.call(http.MethodPost, "/password/forgot", map[string]string{"email": email}))
		beta := customerClient(t, handler, "beta")
		beta.expect(http.StatusBadRequest, "INVALID_TOKEN", beta.call(http.MethodPost, "/password/reset", map[string]string{
			"token": mailer.lastToken(t, email), "password": strongPassword,
		}))
	})
}

func TestSessions(t *testing.T) {
	s := newSuite(t)
	mailer := &mailbox{}
	handler := app.NewRouter(s.deps(mailer, nil))

	t.Run("refresh rotates the token", func(t *testing.T) {
		client := customerClient(t, handler, "alfa")
		client.signUp(mailer, "rotacao@exemplo.com")
		first := client.cookie
		client.expect(http.StatusOK, "", client.call(http.MethodPost, "/refresh", nil))
		if client.cookie == first || client.cookie == "" {
			t.Error("the refresh token must change on every use")
		}
		client.expect(http.StatusOK, "", client.call(http.MethodGet, "/me", nil))
	})

	t.Run("simultaneous refreshes never revoke the session", func(t *testing.T) {
		client := customerClient(t, handler, "alfa")
		client.signUp(mailer, "abas@exemplo.com")
		cookie := client.cookie

		var wg sync.WaitGroup
		codes := make(chan int, 8)
		for range 8 {
			wg.Go(func() {
				parallel := customerClient(t, handler, "alfa")
				parallel.cookie = cookie
				codes <- parallel.call(http.MethodPost, "/refresh", nil).code
			})
		}
		wg.Wait()
		close(codes)
		succeeded := 0
		for code := range codes {
			switch code {
			case http.StatusOK:
				succeeded++
			case http.StatusConflict:
			default:
				t.Errorf("unexpected status %d", code)
			}
		}
		if succeeded != 1 {
			t.Errorf("%d refreshes succeeded, want 1", succeeded)
		}
		client.expect(http.StatusOK, "", client.call(http.MethodGet, "/me", nil))
	})

	t.Run("reusing a stolen refresh token revokes the session", func(t *testing.T) {
		client := customerClient(t, handler, "alfa")
		client.signUp(mailer, "roubo@exemplo.com")
		stolen := client.cookie
		client.expect(http.StatusOK, "", client.call(http.MethodPost, "/refresh", nil))

		_, err := s.admin.Exec(s.ctx, `UPDATE identity.refresh_tokens SET used_at = now() - interval '1 minute' WHERE used_at IS NOT NULL`)
		if err != nil {
			t.Fatal(err)
		}
		attacker := customerClient(t, handler, "alfa")
		attacker.cookie = stolen
		attacker.expect(http.StatusUnauthorized, "SESSION_EXPIRED", attacker.call(http.MethodPost, "/refresh", nil))

		client.expect(http.StatusUnauthorized, "SESSION_EXPIRED", client.call(http.MethodGet, "/me", nil))
		client.token = ""
		client.expect(http.StatusUnauthorized, "SESSION_EXPIRED", client.call(http.MethodPost, "/refresh", nil))
	})

	t.Run("refresh refuses requests from another site", func(t *testing.T) {
		client := customerClient(t, handler, "alfa")
		client.signUp(mailer, "csrf@exemplo.com")
		for name, origin := range map[string]string{"other site": "https://evil.test", "missing": ""} {
			t.Run(name, func(t *testing.T) {
				req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", nil)
				req.Host = client.host
				req.AddCookie(&http.Cookie{Name: "wl_refresh", Value: client.cookie})
				if origin != "" {
					req.Header.Set("Origin", origin)
				}
				rec := httptest.NewRecorder()
				handler.ServeHTTP(rec, req)
				if rec.Code != http.StatusForbidden {
					t.Errorf("status = %d", rec.Code)
				}
			})
		}
	})

	t.Run("logout ends the session", func(t *testing.T) {
		client := customerClient(t, handler, "alfa")
		client.signUp(mailer, "saida@exemplo.com")
		cookie := client.cookie
		client.expect(http.StatusNoContent, "", client.call(http.MethodPost, "/logout", nil))
		client.expect(http.StatusUnauthorized, "SESSION_EXPIRED", client.call(http.MethodGet, "/me", nil))
		client.token, client.cookie = "", cookie
		client.expect(http.StatusUnauthorized, "SESSION_EXPIRED", client.call(http.MethodPost, "/refresh", nil))
	})

	t.Run("devices can be listed and revoked", func(t *testing.T) {
		phone := customerClient(t, handler, "alfa")
		phone.signUp(mailer, "aparelhos@exemplo.com")
		laptop := customerClient(t, handler, "alfa")
		laptop.expect(http.StatusOK, "", laptop.call(http.MethodPost, "/login", map[string]string{"email": "aparelhos@exemplo.com", "password": strongPassword}))

		listed := laptop.call(http.MethodGet, "/sessions", nil)
		items, _ := listed.body["items"].([]any)
		if len(items) != 2 {
			t.Fatalf("sessions = %v", listed.body)
		}
		var other string
		for _, item := range items {
			session, _ := item.(map[string]any)
			if session["current"] == false {
				other, _ = session["id"].(string)
			}
		}
		laptop.expect(http.StatusNoContent, "", laptop.call(http.MethodDelete, "/sessions/"+other, nil))
		phone.expect(http.StatusUnauthorized, "SESSION_EXPIRED", phone.call(http.MethodGet, "/me", nil))
		laptop.expect(http.StatusOK, "", laptop.call(http.MethodGet, "/me", nil))

		history := laptop.call(http.MethodGet, "/access-history", nil)
		if events, _ := history.body["items"].([]any); len(events) < 2 {
			t.Errorf("history = %v", history.body)
		}
	})

	t.Run("session of another account looks nonexistent", func(t *testing.T) {
		victim := customerClient(t, handler, "alfa")
		victim.signUp(mailer, "vitima@exemplo.com")
		items, _ := victim.call(http.MethodGet, "/sessions", nil).body["items"].([]any)
		session, _ := items[0].(map[string]any)
		id, _ := session["id"].(string)

		attacker := customerClient(t, handler, "alfa")
		attacker.signUp(mailer, "atacante@exemplo.com")
		attacker.expect(http.StatusNotFound, "SESSION_NOT_FOUND", attacker.call(http.MethodDelete, "/sessions/"+id, nil))
		victim.expect(http.StatusOK, "", victim.call(http.MethodGet, "/me", nil))
	})
}

func TestTwoStepVerification(t *testing.T) {
	s := newSuite(t)
	mailer := &mailbox{}
	handler := app.NewRouter(s.deps(mailer, nil))
	const email = "mfa@exemplo.com"
	credentials := map[string]string{"email": email, "password": strongPassword}

	client := customerClient(t, handler, "alfa")
	client.signUp(mailer, email)

	client.expect(http.StatusBadRequest, "INVALID_PASSWORD", client.call(http.MethodPost, "/mfa/enroll", map[string]string{"password": "senha incorreta longa"}))
	enrollment := client.call(http.MethodPost, "/mfa/enroll", map[string]string{"password": strongPassword})
	client.expect(http.StatusOK, "", enrollment)
	secret := enrollment.str("secret")

	client.expect(http.StatusUnauthorized, "INVALID_MFA_CODE", client.call(http.MethodPost, "/mfa/enroll/confirm", map[string]string{"code": "000000"}))
	confirmed := client.call(http.MethodPost, "/mfa/enroll/confirm", map[string]string{"code": totp(t, secret, -1)})
	client.expect(http.StatusOK, "", confirmed)
	codes, _ := confirmed.body["recoveryCodes"].([]any)
	if len(codes) != 10 {
		t.Fatalf("recovery codes = %v", confirmed.body)
	}

	t.Run("secret is encrypted at rest", func(t *testing.T) {
		var stored []byte
		if err := s.admin.QueryRow(s.ctx, `SELECT mfa_secret_encrypted FROM identity.users WHERE email = $1`, email).Scan(&stored); err != nil {
			t.Fatal(err)
		}
		raw, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secret)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(stored, raw) || bytes.Contains(stored, []byte(secret)) {
			t.Error("the secret is stored in clear text")
		}
	})

	t.Run("password alone no longer signs in", func(t *testing.T) {
		login := customerClient(t, handler, "alfa")
		first := login.call(http.MethodPost, "/login", credentials)
		if first.str("status") != "mfa_required" || first.str("accessToken") != "" || login.cookie != "" {
			t.Fatalf("login = %v", first.body)
		}
	})

	t.Run("valid code signs in and cannot be replayed", func(t *testing.T) {
		code := totp(t, secret, 0)
		login := customerClient(t, handler, "alfa")
		challenge := login.call(http.MethodPost, "/login", credentials).str("challenge")
		login.expect(http.StatusOK, "", login.call(http.MethodPost, "/mfa/challenge", map[string]string{"challenge": challenge, "code": code}))

		replay := customerClient(t, handler, "alfa")
		challenge = replay.call(http.MethodPost, "/login", credentials).str("challenge")
		replay.expect(http.StatusUnauthorized, "INVALID_MFA_CODE", replay.call(http.MethodPost, "/mfa/challenge", map[string]string{"challenge": challenge, "code": code}))
	})

	t.Run("challenge is discarded after too many wrong codes", func(t *testing.T) {
		login := customerClient(t, handler, "alfa")
		challenge := login.call(http.MethodPost, "/login", credentials).str("challenge")
		for range 4 {
			login.expect(http.StatusUnauthorized, "INVALID_MFA_CODE", login.call(http.MethodPost, "/mfa/challenge", map[string]string{"challenge": challenge, "code": "000000"}))
		}
		login.expect(http.StatusUnauthorized, "CHALLENGE_EXPIRED", login.call(http.MethodPost, "/mfa/challenge", map[string]string{"challenge": challenge, "code": "000000"}))
		login.expect(http.StatusUnauthorized, "CHALLENGE_EXPIRED", login.call(http.MethodPost, "/mfa/challenge", map[string]string{"challenge": challenge, "code": totp(t, secret, 1)}))
	})

	t.Run("recovery code works once", func(t *testing.T) {
		code, _ := codes[0].(string)
		login := customerClient(t, handler, "alfa")
		challenge := login.call(http.MethodPost, "/login", credentials).str("challenge")
		login.expect(http.StatusOK, "", login.call(http.MethodPost, "/mfa/challenge", map[string]string{"challenge": challenge, "code": strings.ToLower(code)}))

		again := customerClient(t, handler, "alfa")
		challenge = again.call(http.MethodPost, "/login", credentials).str("challenge")
		again.expect(http.StatusUnauthorized, "INVALID_MFA_CODE", again.call(http.MethodPost, "/mfa/challenge", map[string]string{"challenge": challenge, "code": code}))
	})

	t.Run("challenge of one store is refused on another", func(t *testing.T) {
		login := customerClient(t, handler, "alfa")
		challenge := login.call(http.MethodPost, "/login", credentials).str("challenge")
		other := customerClient(t, handler, "beta")
		other.expect(http.StatusUnauthorized, "CHALLENGE_EXPIRED", other.call(http.MethodPost, "/mfa/challenge", map[string]string{"challenge": challenge, "code": totp(t, secret, 1)}))
	})
}

func TestPlatformOperatorsMustUseTwoStepVerification(t *testing.T) {
	s := newSuite(t)
	handler := app.NewRouter(s.deps(&mailbox{}, nil))
	const email = "operador@plataforma.test"

	var out bytes.Buffer
	runner := identitycli.NewRunner(s.platform, "https://"+adminHost, "https://"+consoleHost, &out)
	if err := runner.Run(s.ctx, []string{"operator", "invite", "--email", email, "--name", "Operador"}); err != nil {
		t.Fatal(err)
	}
	client := platformClient(t, handler)
	client.expect(http.StatusNoContent, "", client.call(http.MethodPost, "/invitation/accept", map[string]string{
		"token": tokenInLink.FindStringSubmatch(out.String())[1], "name": "Operador", "password": strongPassword,
	}))

	login := client.call(http.MethodPost, "/login", map[string]string{"email": email, "password": strongPassword})
	if login.str("status") != "mfa_enrollment_required" || login.str("accessToken") != "" {
		t.Fatalf("login = %v", login.body)
	}
	challenge := login.str("challenge")
	setup := client.call(http.MethodPost, "/mfa/setup/start", map[string]string{"challenge": challenge})
	client.expect(http.StatusOK, "", setup)
	done := client.call(http.MethodPost, "/mfa/setup/confirm", map[string]string{"challenge": challenge, "code": totp(t, setup.str("secret"), 0)})
	client.expect(http.StatusOK, "", done)
	if codes, _ := done.body["recoveryCodes"].([]any); len(codes) != 10 {
		t.Errorf("recovery codes = %v", done.body)
	}

	client.expect(http.StatusForbidden, "MFA_REQUIRED", client.call(http.MethodPost, "/mfa/disable", map[string]string{
		"password": strongPassword, "code": totp(t, setup.str("secret"), 1),
	}))
	client.expect(http.StatusOK, "", client.call(http.MethodGet, "/team", nil))
}

func TestPortalAndTenantSeparation(t *testing.T) {
	s := newSuite(t)
	mailer := &mailbox{}
	handler := app.NewRouter(s.deps(mailer, nil))
	const email = "mesma@exemplo.com"

	alfa := customerClient(t, handler, "alfa")
	alfa.signUp(mailer, email)
	owner := s.inviteStaff(t, handler, "alfa", "dono@alfa.test", "owner")

	t.Run("same e-mail is independent in each store", func(t *testing.T) {
		beta := customerClient(t, handler, "beta")
		beta.expect(http.StatusUnauthorized, "INVALID_CREDENTIALS", beta.call(http.MethodPost, "/login", map[string]string{"email": email, "password": strongPassword}))
		beta.signUp(mailer, email)
		if beta.call(http.MethodGet, "/me", nil).str("id") == alfa.call(http.MethodGet, "/me", nil).str("id") {
			t.Error("accounts of different stores must be separate")
		}
	})

	t.Run("customer token is refused on another store", func(t *testing.T) {
		stolen := customerClient(t, handler, "beta")
		stolen.token = alfa.token
		stolen.expect(http.StatusUnauthorized, "SESSION_EXPIRED", stolen.call(http.MethodGet, "/me", nil))
	})

	t.Run("refresh token is refused on another store", func(t *testing.T) {
		stolen := customerClient(t, handler, "beta")
		stolen.cookie = alfa.cookie
		stolen.expect(http.StatusUnauthorized, "SESSION_EXPIRED", stolen.call(http.MethodPost, "/refresh", nil))
	})

	t.Run("tokens never cross portals", func(t *testing.T) {
		attempts := map[string]*apiClient{
			"customer token on staff":    {t: t, handler: handler, host: adminHost, base: "/staff", token: alfa.token},
			"customer token on platform": {t: t, handler: handler, host: consoleHost, base: "/platform", token: alfa.token},
			"staff token on platform":    {t: t, handler: handler, host: consoleHost, base: "/platform", token: owner.token},
			"staff token on customer":    {t: t, handler: handler, host: "alfa." + baseDomain, base: "/auth", token: owner.token},
		}
		for name, client := range attempts {
			t.Run(name, func(t *testing.T) {
				client.t = t
				client.expect(http.StatusUnauthorized, "SESSION_EXPIRED", client.call(http.MethodGet, "/me", nil))
			})
		}
	})

	t.Run("staff password does not open the customer portal", func(t *testing.T) {
		client := customerClient(t, handler, "alfa")
		client.expect(http.StatusUnauthorized, "INVALID_CREDENTIALS", client.call(http.MethodPost, "/login", map[string]string{"email": "dono@alfa.test", "password": strongPassword}))
	})

	t.Run("staff of one store cannot sign in to another", func(t *testing.T) {
		client := staffClient(t, handler)
		client.expect(http.StatusUnauthorized, "INVALID_CREDENTIALS", client.call(http.MethodPost, "/login", map[string]string{
			"store": "beta", "email": "dono@alfa.test", "password": strongPassword,
		}))
	})

	t.Run("reserved platform tenant is not a store", func(t *testing.T) {
		client := staffClient(t, handler)
		client.expect(http.StatusUnauthorized, "INVALID_CREDENTIALS", client.call(http.MethodPost, "/login", map[string]string{
			"store": "platform", "email": "dono@alfa.test", "password": strongPassword,
		}))
	})

	t.Run("forged token is refused", func(t *testing.T) {
		parts := strings.Split(owner.token, ".")
		payload, err := base64.RawURLEncoding.DecodeString(parts[1])
		if err != nil {
			t.Fatal(err)
		}
		parts[1] = base64.RawURLEncoding.EncodeToString(bytes.ReplaceAll(payload, []byte(`"staff"`), []byte(`"platform"`)))
		forged := platformClient(t, handler)
		forged.token = strings.Join(parts, ".")
		forged.expect(http.StatusUnauthorized, "SESSION_EXPIRED", forged.call(http.MethodGet, "/team", nil))
	})

	t.Run("suspending a store ends staff access", func(t *testing.T) {
		member := s.inviteStaff(t, handler, "gama", "dono@gama.test", "owner")
		member.expect(http.StatusOK, "", member.call(http.MethodGet, "/team", nil))
		s.run(t, "tenant", "suspend", "--slug", "gama")
		member.expect(http.StatusUnauthorized, "SESSION_EXPIRED", member.call(http.MethodGet, "/team", nil))
	})
}

func TestRoleBasedAccess(t *testing.T) {
	s := newSuite(t)
	mailer := &mailbox{}
	handler := app.NewRouter(s.deps(mailer, nil))

	owner := s.inviteStaff(t, handler, "alfa", "dono@alfa.test", "owner")
	manager := s.inviteStaff(t, handler, "alfa", "gerente@alfa.test", "manager")
	viewer := s.inviteStaff(t, handler, "alfa", "leitor@alfa.test", "viewer")
	otherStore := s.inviteStaff(t, handler, "beta", "dono@beta.test", "owner")

	memberID := func(t *testing.T, email string) string {
		t.Helper()
		items, _ := owner.call(http.MethodGet, "/team", nil).body["items"].([]any)
		for _, item := range items {
			member, _ := item.(map[string]any)
			if member["email"] == email {
				id, _ := member["id"].(string)
				return id
			}
		}
		t.Fatalf("member %s not found", email)
		return ""
	}

	t.Run("permissions follow the role", func(t *testing.T) {
		invite := map[string]string{"name": "Novo", "email": "novo@alfa.test", "role": "viewer"}
		viewer.expect(http.StatusForbidden, "PERMISSION_DENIED", viewer.call(http.MethodGet, "/team", nil))
		viewer.expect(http.StatusForbidden, "PERMISSION_DENIED", viewer.call(http.MethodGet, "/audit", nil))
		viewer.expect(http.StatusForbidden, "PERMISSION_DENIED", viewer.call(http.MethodPost, "/team/invitations", invite))
		manager.expect(http.StatusOK, "", manager.call(http.MethodGet, "/team", nil))
		manager.expect(http.StatusOK, "", manager.call(http.MethodGet, "/audit", nil))
		manager.expect(http.StatusForbidden, "PERMISSION_DENIED", manager.call(http.MethodPost, "/team/invitations", invite))
		owner.expect(http.StatusAccepted, "", owner.call(http.MethodPost, "/team/invitations", invite))
		owner.expect(http.StatusConflict, "MEMBER_ALREADY_EXISTS", owner.call(http.MethodPost, "/team/invitations", invite))
	})

	t.Run("role of another realm cannot be assigned", func(t *testing.T) {
		owner.expect(http.StatusBadRequest, "VALIDATION_FAILED", owner.call(http.MethodPost, "/team/invitations", map[string]string{
			"name": "Intruso", "email": "intruso@alfa.test", "role": "platform_admin",
		}))
	})

	t.Run("anonymous callers are refused", func(t *testing.T) {
		anonymous := staffClient(t, handler)
		anonymous.expect(http.StatusUnauthorized, "AUTHENTICATION_REQUIRED", anonymous.call(http.MethodGet, "/team", nil))
	})

	t.Run("role change applies immediately", func(t *testing.T) {
		id := memberID(t, "leitor@alfa.test")
		owner.expect(http.StatusNoContent, "", owner.call(http.MethodPut, "/team/"+id+"/role", map[string]string{"role": "manager"}))
		viewer.expect(http.StatusOK, "", viewer.call(http.MethodGet, "/team", nil))
		owner.expect(http.StatusNoContent, "", owner.call(http.MethodPut, "/team/"+id+"/role", map[string]string{"role": "viewer"}))
		viewer.expect(http.StatusForbidden, "PERMISSION_DENIED", viewer.call(http.MethodGet, "/team", nil))
	})

	t.Run("member of another store looks nonexistent", func(t *testing.T) {
		id := memberID(t, "gerente@alfa.test")
		otherStore.expect(http.StatusNotFound, "MEMBER_NOT_FOUND", otherStore.call(http.MethodPut, "/team/"+id+"/role", map[string]string{"role": "viewer"}))
		otherStore.expect(http.StatusNotFound, "MEMBER_NOT_FOUND", otherStore.call(http.MethodPost, "/team/"+id+"/deactivate", nil))
		team := otherStore.call(http.MethodGet, "/team", nil)
		if strings.Contains(fmt.Sprint(team.body), "alfa.test") {
			t.Errorf("team of another store exposed: %v", team.body)
		}
	})

	t.Run("administrators cannot change themselves", func(t *testing.T) {
		id := memberID(t, "dono@alfa.test")
		owner.expect(http.StatusConflict, "SELF_CHANGE_NOT_ALLOWED", owner.call(http.MethodPut, "/team/"+id+"/role", map[string]string{"role": "viewer"}))
		owner.expect(http.StatusConflict, "SELF_CHANGE_NOT_ALLOWED", owner.call(http.MethodPost, "/team/"+id+"/deactivate", nil))
	})

	t.Run("deactivated member loses access immediately", func(t *testing.T) {
		id := memberID(t, "gerente@alfa.test")
		cookie := manager.cookie
		owner.expect(http.StatusNoContent, "", owner.call(http.MethodPost, "/team/"+id+"/deactivate", nil))
		manager.expect(http.StatusUnauthorized, "SESSION_EXPIRED", manager.call(http.MethodGet, "/me", nil))
		manager.token, manager.cookie = "", cookie
		manager.expect(http.StatusUnauthorized, "SESSION_EXPIRED", manager.call(http.MethodPost, "/refresh", nil))
		manager.expect(http.StatusUnauthorized, "INVALID_CREDENTIALS", manager.call(http.MethodPost, "/login", map[string]string{
			"store": "alfa", "email": "gerente@alfa.test", "password": strongPassword,
		}))
	})

	t.Run("two owners cannot remove each other at the same time", func(t *testing.T) {
		first := s.inviteStaff(t, handler, "gama", "primeiro@gama.test", "owner")
		second := s.inviteStaff(t, handler, "gama", "segundo@gama.test", "owner")
		idOf := func(client *apiClient) string { return client.call(http.MethodGet, "/me", nil).str("id") }
		firstID, secondID := idOf(first), idOf(second)

		var wg sync.WaitGroup
		results := make([]int, 2)
		wg.Go(func() { results[0] = first.call(http.MethodPost, "/team/"+secondID+"/deactivate", nil).code })
		wg.Go(func() { results[1] = second.call(http.MethodPost, "/team/"+firstID+"/deactivate", nil).code })
		wg.Wait()

		var active int
		err := s.admin.QueryRow(s.ctx, `
			SELECT count(*) FROM identity.users u JOIN core.tenants t ON t.id = u.tenant_id
			WHERE t.slug = 'gama' AND u.realm = 'staff' AND u.status = 'active'`).Scan(&active)
		if err != nil {
			t.Fatal(err)
		}
		if active == 0 {
			t.Errorf("the store lost every administrator, results = %v", results)
		}
	})

	t.Run("administrative actions are audited", func(t *testing.T) {
		entries := owner.call(http.MethodGet, "/audit", nil)
		text := fmt.Sprint(entries.body)
		for _, action := range []string{"identity.member.invited", "identity.member.role_changed", "identity.member.deactivated"} {
			if !strings.Contains(text, action) {
				t.Errorf("%s is missing from the audit trail", action)
			}
		}
		if other := fmt.Sprint(otherStore.call(http.MethodGet, "/audit", nil).body); strings.Contains(other, "identity.member.deactivated") {
			t.Errorf("audit trail of another store exposed: %s", other)
		}
	})

	t.Run("audit trail is append-only", func(t *testing.T) {
		for _, role := range []struct {
			name string
			exec func(string) error
		}{
			{"runtime", func(sql string) error { _, err := s.runtime.Exec(s.ctx, sql); return err }},
			{"platform", func(sql string) error { _, err := s.platform.Exec(s.ctx, sql); return err }},
		} {
			for _, statement := range []string{`UPDATE audit.entries SET action = 'apagado'`, `DELETE FROM audit.entries`} {
				if err := role.exec(statement); err == nil {
					t.Errorf("%s was allowed to run: %s", role.name, statement)
				}
			}
		}
	})

	t.Run("operator tool actions are audited", func(t *testing.T) {
		s.run(t, "tenant", "suspend", "--slug", "gama")
		var count int
		err := s.admin.QueryRow(s.ctx, `
			SELECT count(*) FROM audit.entries
			WHERE actor_type = 'operator' AND action = 'tenants.tenant.status_changed' AND target_id = 'gama'`).Scan(&count)
		if err != nil || count == 0 {
			t.Errorf("count=%d err=%v", count, err)
		}
	})
}

func TestRateLimits(t *testing.T) {
	s := newSuite(t)
	mailer := &mailbox{}
	limiter := ratelimit.New(nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	handler := app.NewRouter(s.deps(mailer, limiter))

	t.Run("repeated failures lock the account for a while", func(t *testing.T) {
		client := customerClient(t, handler, "alfa")
		credentials := map[string]string{"email": "alvo@exemplo.com", "password": "tentativa de senha"}
		for range 10 {
			client.expect(http.StatusUnauthorized, "INVALID_CREDENTIALS", client.call(http.MethodPost, "/login", credentials))
		}
		limited := client.call(http.MethodPost, "/login", credentials)
		client.expect(http.StatusTooManyRequests, "RATE_LIMITED", limited)
		if limited.header.Get("Retry-After") == "" {
			t.Error("Retry-After is missing")
		}
	})

	t.Run("limit of one store does not affect another", func(t *testing.T) {
		client := customerClient(t, handler, "beta")
		client.expect(http.StatusUnauthorized, "INVALID_CREDENTIALS", client.call(http.MethodPost, "/login", map[string]string{
			"email": "alvo@exemplo.com", "password": "tentativa de senha",
		}))
	})

	t.Run("reset requests stop sending e-mail", func(t *testing.T) {
		client := customerClient(t, handler, "gama")
		client.signUp(mailer, "limite@exemplo.com")
		before := mailer.count("limite@exemplo.com")
		for range 8 {
			client.expect(http.StatusAccepted, "", client.call(http.MethodPost, "/password/forgot", map[string]string{"email": "limite@exemplo.com"}))
		}
		if sent := mailer.count("limite@exemplo.com") - before; sent != 3 {
			t.Errorf("%d e-mails were sent, want 3", sent)
		}
	})
}

func TestEveryRouteDeclaresAnAccessPolicy(t *testing.T) {
	s := newSuite(t)
	routes := app.NewRoutes(s.deps(&mailbox{}, nil))
	handler := app.NewRouter(s.deps(&mailbox{}, nil))

	total, protected := 0, 0
	err := chi.Walk(routes, func(method, route string, h http.Handler, _ ...func(http.Handler) http.Handler) error {
		if !strings.HasPrefix(route, "/api/v1/") {
			return nil
		}
		total++
		endpoint, ok := h.(*authz.Endpoint)
		if !ok {
			t.Errorf("%s %s has no access policy", method, route)
			return nil
		}
		if endpoint.Policy.IsPublic() {
			return nil
		}
		protected++

		path := strings.ReplaceAll(route, "{id}", "01960000-0000-7000-8000-000000000999")
		for _, host := range []string{"alfa." + baseDomain, adminHost, consoleHost} {
			req := httptest.NewRequest(method, path, strings.NewReader("{}"))
			req.Host = host
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code == http.StatusOK || rec.Code == http.StatusNoContent || rec.Code == http.StatusAccepted {
				t.Errorf("%s %s answered %d to an anonymous caller on %s", method, route, rec.Code, host)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if total < 30 || protected < 15 {
		t.Errorf("routes=%d protected=%d", total, protected)
	}
}
