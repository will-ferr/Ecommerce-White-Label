// Package email validates e-mail addresses in one place for every module.
package email

import (
	"net/mail"
	"strings"

	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/shared/apperr"
)

const maxLength = 254

// Normalize validates an address and returns its canonical lowercase form.
func Normalize(raw string) (string, error) {
	address := strings.ToLower(strings.TrimSpace(raw))
	parsed, err := mail.ParseAddress(address)
	at := strings.LastIndex(address, "@")
	if err != nil || parsed.Address != address || len(address) > maxLength || at < 1 || !strings.Contains(address[at:], ".") {
		return "", apperr.Validation(apperr.FieldError{Field: "email", Code: "invalid", Message: "Enter a valid e-mail address."})
	}
	return address, nil
}
