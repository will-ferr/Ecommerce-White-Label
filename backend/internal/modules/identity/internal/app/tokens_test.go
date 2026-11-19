package app

import (
	"crypto/ed25519"
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

var testSeed = []byte("test-signing-key-with-32-bytes-0")

func newIssuer(t *testing.T, now time.Time) *TokenIssuer {
	t.Helper()
	issuer, err := NewTokenIssuer(testSeed, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	return issuer
}

func TestIssueAndVerify(t *testing.T) {
	issuer := newIssuer(t, time.Now())
	tenant, user, session := uuid.New(), uuid.New(), uuid.New()

	raw, err := issuer.Issue("storefront", tenant, user, session, "customer")
	if err != nil {
		t.Fatal(err)
	}
	claims, err := issuer.Verify(raw, "storefront")
	if err != nil {
		t.Fatal(err)
	}
	if claims.TenantID != tenant || claims.Subject != user.String() || claims.SessionID != session || claims.Realm != "customer" {
		t.Errorf("claims = %+v", claims)
	}
}

func TestVerifyRefusesTokenOfAnotherPortal(t *testing.T) {
	issuer := newIssuer(t, time.Now())
	raw, err := issuer.Issue("storefront", uuid.New(), uuid.New(), uuid.New(), "customer")
	if err != nil {
		t.Fatal(err)
	}
	for _, audience := range []string{"staff", "platform"} {
		if _, err := issuer.Verify(raw, audience); err == nil {
			t.Errorf("audience %s accepted a storefront token", audience)
		}
	}
}

func TestVerifyRefusesExpiredToken(t *testing.T) {
	now := time.Now()
	raw, err := newIssuer(t, now).Issue("staff", uuid.New(), uuid.New(), uuid.New(), "staff")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := newIssuer(t, now.Add(accessTokenTTL+time.Minute)).Verify(raw, "staff"); err == nil {
		t.Error("an expired token was accepted")
	}
}

func TestVerifyRefusesForgedTokens(t *testing.T) {
	issuer := newIssuer(t, time.Now())
	claims := AccessClaims{
		TenantID: uuid.New(), Realm: "platform", SessionID: uuid.New(),
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer: tokenIssuer, Subject: uuid.NewString(), Audience: jwt.ClaimStrings{"platform"},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}

	none, err := jwt.NewWithClaims(jwt.SigningMethodNone, claims).SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatal(err)
	}
	// Classic confusion attack: HMAC signed with the public key as the secret.
	confused, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(issuer.public))
	if err != nil {
		t.Fatal(err)
	}
	_, otherKey, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	wrongKey, err := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims).SignedString(otherKey)
	if err != nil {
		t.Fatal(err)
	}
	valid, err := issuer.Issue("platform", uuid.New(), uuid.New(), uuid.New(), "customer")
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(valid, ".")
	parts[1] = base64.RawURLEncoding.EncodeToString([]byte(`{"realm":"platform","aud":["platform"],"iss":"ecommerce-platform","exp":9999999999}`))
	tampered := strings.Join(parts, ".")

	for name, raw := range map[string]string{
		"alg none": none, "hmac confusion": confused, "wrong key": wrongKey, "tampered payload": tampered,
		"empty": "", "garbage": "a.b.c",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := issuer.Verify(raw, "platform"); err == nil {
				t.Error("a forged token was accepted")
			}
		})
	}
}

func TestParseToken(t *testing.T) {
	tenant := uuid.New()
	id, secret, ok := parseToken(composeToken(tenant, "segredo"))
	if !ok || id != tenant || secret != "segredo" {
		t.Fatalf("id=%s secret=%s ok=%v", id, secret, ok)
	}
	for _, bad := range []string{"", "semponto", ".segredo", tenant.String() + ".", "nao-uuid.segredo", uuid.Nil.String() + ".x", tenant.String() + "." + strings.Repeat("a", 300)} {
		if _, _, ok := parseToken(bad); ok {
			t.Errorf("parseToken(%q) accepted a malformed token", bad)
		}
	}
}
