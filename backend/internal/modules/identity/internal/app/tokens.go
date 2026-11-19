package app

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

const (
	accessTokenTTL = 10 * time.Minute
	tokenIssuer    = "ecommerce-platform"
)

// AccessClaims are the contents of an access token.
type AccessClaims struct {
	TenantID  uuid.UUID `json:"tid"`
	Realm     string    `json:"realm"`
	SessionID uuid.UUID `json:"sid"`
	jwt.RegisteredClaims
}

// TokenIssuer signs and verifies access tokens with Ed25519.
type TokenIssuer struct {
	private ed25519.PrivateKey
	public  ed25519.PublicKey
	keyID   string
	now     func() time.Time
}

// NewTokenIssuer creates an issuer from a 32-byte seed.
func NewTokenIssuer(seed []byte, now func() time.Time) (*TokenIssuer, error) {
	if len(seed) != ed25519.SeedSize {
		return nil, errors.New("signing seed must have 32 bytes")
	}
	private := ed25519.NewKeyFromSeed(seed)
	public, ok := private.Public().(ed25519.PublicKey)
	if !ok {
		return nil, errors.New("unexpected public key type")
	}
	digest := sha256.Sum256(public)
	return &TokenIssuer{private: private, public: public, keyID: hex.EncodeToString(digest[:8]), now: now}, nil
}

// Issue signs an access token for a portal.
func (i *TokenIssuer) Issue(audience string, tenantID, userID, sessionID uuid.UUID, realm string) (string, error) {
	now := i.now()
	token := jwt.NewWithClaims(jwt.SigningMethodEdDSA, AccessClaims{
		TenantID: tenantID, Realm: realm, SessionID: sessionID,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer: tokenIssuer, Subject: userID.String(), Audience: jwt.ClaimStrings{audience},
			IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(now.Add(accessTokenTTL)),
		},
	})
	token.Header["kid"] = i.keyID
	signed, err := token.SignedString(i.private)
	if err != nil {
		return "", fmt.Errorf("sign access token: %w", err)
	}
	return signed, nil
}

// Verify validates signature, algorithm, issuer, audience and expiry.
func (i *TokenIssuer) Verify(raw, audience string) (AccessClaims, error) {
	var claims AccessClaims
	_, err := jwt.ParseWithClaims(raw, &claims,
		func(*jwt.Token) (any, error) { return i.public, nil },
		jwt.WithValidMethods([]string{jwt.SigningMethodEdDSA.Alg()}),
		jwt.WithIssuer(tokenIssuer),
		jwt.WithAudience(audience),
		jwt.WithExpirationRequired(),
		jwt.WithTimeFunc(i.now),
	)
	if err != nil {
		return AccessClaims{}, fmt.Errorf("verify access token: %w", err)
	}
	return claims, nil
}

// composeToken prefixes a secret with its tenant, so a token sent by e-mail or
// cookie can be looked up under the Row Level Security of that tenant.
func composeToken(tenantID uuid.UUID, secret string) string {
	return tenantID.String() + "." + secret
}

func parseToken(token string) (uuid.UUID, string, bool) {
	prefix, secret, ok := strings.Cut(token, ".")
	if !ok || secret == "" || len(token) > 256 {
		return uuid.Nil, "", false
	}
	tenantID, err := uuid.Parse(prefix)
	if err != nil || tenantID == uuid.Nil {
		return uuid.Nil, "", false
	}
	return tenantID, secret, true
}
