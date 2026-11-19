// Package domain holds the identity rules, free of infrastructure concerns.
package domain

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/shared/apperr"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/shared/email"
)

// Realms separate the three kinds of account.
const (
	RealmCustomer = "customer"
	RealmStaff    = "staff"
	RealmPlatform = "platform"
)

// Account statuses.
const (
	StatusActive   = "active"
	StatusInvited  = "invited"
	StatusDisabled = "disabled"
)

// Single-use token kinds.
const (
	TokenEmailVerification = "email_verification" //nolint:gosec // Name of a token kind, not a credential.
	TokenPasswordReset     = "password_reset"
	TokenInvitation        = "invitation"
)

const (
	minPasswordLength = 10
	maxPasswordLength = 128
	maxNameLength     = 120
)

// commonPasswords refuses frequent choices that satisfy the length rule.
var commonPasswords = map[string]struct{}{
	"1234567890": {}, "12345678910": {}, "123456789012": {}, "0123456789": {}, "0987654321": {},
	"1q2w3e4r5t": {}, "1qaz2wsx3edc": {}, "qwertyuiop": {}, "qwerty12345": {}, "qwerty123456": {},
	"password12": {}, "password123": {}, "password1234": {}, "passw0rd123": {}, "administrator": {},
	"admin12345": {}, "admin123456": {}, "letmein1234": {}, "welcome1234": {}, "iloveyou12": {},
	"senha12345": {}, "senha123456": {}, "minhasenha": {}, "minhasenha123": {}, "brasil1234": {},
	"brasil12345": {}, "mudar12345": {}, "mudar123456": {}, "aaaaaaaaaa": {}, "abcdefghij": {},
}

func fieldError(field, code, message string) *apperr.Error {
	return apperr.Validation(apperr.FieldError{Field: field, Code: code, Message: message})
}

// NormalizeEmail validates an address and returns its canonical form.
func NormalizeEmail(raw string) (string, error) {
	return email.Normalize(raw)
}

// NormalizeName validates a display name.
func NormalizeName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	length := utf8.RuneCountInString(name)
	if length == 0 || length > maxNameLength || strings.ContainsFunc(name, unicode.IsControl) {
		return "", fieldError("name", "invalid", "Enter a name with up to 120 characters.")
	}
	return name, nil
}

// ValidatePassword applies the password policy. Long passphrases are favored
// over composition rules, as recommended by NIST SP 800-63B.
func ValidatePassword(password, email string) error {
	length := utf8.RuneCountInString(password)
	if length < minPasswordLength {
		return fieldError("password", "too_short", "Use at least 10 characters.")
	}
	if length > maxPasswordLength {
		return fieldError("password", "too_long", "Use at most 128 characters.")
	}
	lowered := strings.ToLower(password)
	if _, common := commonPasswords[lowered]; common {
		return fieldError("password", "common", "This password is too common.")
	}
	if local, _, ok := strings.Cut(email, "@"); ok && len(local) >= 4 && strings.Contains(lowered, local) {
		return fieldError("password", "contains_email", "The password must not contain your e-mail.")
	}
	return nil
}

// DeviceLabel summarizes a User-Agent for the session list.
func DeviceLabel(userAgent string) string {
	browsers := []struct{ token, name string }{
		{"Edg/", "Edge"}, {"OPR/", "Opera"}, {"Chrome/", "Chrome"}, {"Firefox/", "Firefox"}, {"Safari/", "Safari"},
	}
	systems := []struct{ token, name string }{
		{"Android", "Android"}, {"iPhone", "iPhone"}, {"iPad", "iPad"}, {"Windows", "Windows"},
		{"Mac OS X", "macOS"}, {"Linux", "Linux"},
	}
	browser, system := "Navegador desconhecido", "sistema desconhecido"
	for _, candidate := range browsers {
		if strings.Contains(userAgent, candidate.token) {
			browser = candidate.name
			break
		}
	}
	for _, candidate := range systems {
		if strings.Contains(userAgent, candidate.token) {
			system = candidate.name
			break
		}
	}
	return browser + " em " + system
}
