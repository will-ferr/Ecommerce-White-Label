package domain

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1" //nolint:gosec // RFC 6238 authenticator apps use HMAC-SHA1.
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strings"
	"time"
)

const (
	totpPeriodSeconds = 30
	totpModulus       = 1_000_000
	totpSecretBytes   = 20
	totpDrift         = 1

	recoveryCodeCount = 10
	recoveryCodeBytes = 10
)

var base32NoPadding = base32.StdEncoding.WithPadding(base32.NoPadding)

// NewTOTPSecret generates a shared secret.
func NewTOTPSecret() ([]byte, error) {
	secret := make([]byte, totpSecretBytes)
	if _, err := rand.Read(secret); err != nil {
		return nil, fmt.Errorf("generate totp secret: %w", err)
	}
	return secret, nil
}

// EncodeTOTPSecret returns the text typed into an authenticator app.
func EncodeTOTPSecret(secret []byte) string {
	return base32NoPadding.EncodeToString(secret)
}

// TOTPURI returns the address encoded in the enrollment QR code.
func TOTPURI(issuer, account string, secret []byte) string {
	query := url.Values{
		"secret": {EncodeTOTPSecret(secret)}, "issuer": {issuer}, "algorithm": {"SHA1"},
		"digits": {"6"}, "period": {"30"},
	}
	label := url.PathEscape(issuer + ":" + account)
	return "otpauth://totp/" + label + "?" + query.Encode()
}

// TOTPCode computes the RFC 6238 code of a time step.
func TOTPCode(secret []byte, step int64) string {
	var counter [8]byte
	binary.BigEndian.PutUint64(counter[:], uint64(step)) //nolint:gosec // steps are positive
	mac := hmac.New(sha1.New, secret)
	mac.Write(counter[:])
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	value := binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7fffffff
	return fmt.Sprintf("%06d", value%totpModulus)
}

// MatchTOTP finds the time step of a code, tolerating one step of clock drift.
func MatchTOTP(secret []byte, code string, now time.Time) (int64, bool) {
	code = strings.TrimSpace(code)
	if len(code) != 6 {
		return 0, false
	}
	current := now.Unix() / totpPeriodSeconds
	for step := current - totpDrift; step <= current+totpDrift; step++ {
		if subtle.ConstantTimeCompare([]byte(TOTPCode(secret, step)), []byte(code)) == 1 {
			return step, true
		}
	}
	return 0, false
}

// NewRecoveryCodes generates the single-use codes shown once to the user.
func NewRecoveryCodes() ([]string, error) {
	codes := make([]string, 0, recoveryCodeCount)
	for range recoveryCodeCount {
		raw := make([]byte, recoveryCodeBytes)
		if _, err := rand.Read(raw); err != nil {
			return nil, fmt.Errorf("generate recovery code: %w", err)
		}
		encoded := base32NoPadding.EncodeToString(raw)
		codes = append(codes, encoded[:8]+"-"+encoded[8:16])
	}
	return codes, nil
}

// NormalizeRecoveryCode removes the formatting of a typed recovery code.
func NormalizeRecoveryCode(code string) string {
	return strings.ToUpper(strings.NewReplacer("-", "", " ", "").Replace(code))
}
