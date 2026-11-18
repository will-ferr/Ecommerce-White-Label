package domain

import (
	"strings"
	"time"
	_ "time/tzdata" // Keeps timezone validation independent of the host system.
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/currency"
	"golang.org/x/text/language"

	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/shared/apperr"
)

// Field limits.
const (
	maxStoreNameLength      = 120
	maxSEOTitleLength       = 70
	maxSEODescriptionLength = 160
)

// Settings are the commercial and SEO settings of a store.
type Settings struct {
	StoreName      string
	Currency       string
	Locale         string
	Timezone       string
	SEOTitle       string
	SEODescription string
	SEOIndexable   bool
}

// DefaultSettings returns the settings of a new Brazilian store.
func DefaultSettings(storeName string) Settings {
	return Settings{
		StoreName: strings.TrimSpace(storeName),
		Currency:  "BRL",
		Locale:    "pt-BR",
		Timezone:  "America/Sao_Paulo",
	}
}

// Validate reports every invalid field at once.
func (s Settings) Validate() error {
	var fields []apperr.FieldError
	add := func(field, code, message string) {
		fields = append(fields, apperr.FieldError{Field: field, Code: code, Message: message})
	}

	if length := utf8.RuneCountInString(s.StoreName); length == 0 || length > maxStoreNameLength || hasControlChars(s.StoreName) {
		add("storeName", "invalid", "Use 1 to 120 printable characters.")
	}
	if unit, err := currency.ParseISO(s.Currency); err != nil || unit.String() != s.Currency {
		add("currency", "invalid", "Use an ISO 4217 currency code.")
	}
	if tag, err := language.Parse(s.Locale); err != nil || tag.String() != s.Locale {
		add("locale", "invalid", "Use a language tag such as pt-BR.")
	}
	if !validTimezone(s.Timezone) {
		add("timezone", "invalid", "Use an IANA timezone such as America/Sao_Paulo.")
	}
	if utf8.RuneCountInString(s.SEOTitle) > maxSEOTitleLength || hasControlChars(s.SEOTitle) {
		add("seoTitle", "invalid", "Use up to 70 printable characters.")
	}
	if utf8.RuneCountInString(s.SEODescription) > maxSEODescriptionLength || hasControlChars(s.SEODescription) {
		add("seoDescription", "invalid", "Use up to 160 printable characters.")
	}

	if len(fields) > 0 {
		return apperr.Validation(fields...)
	}
	return nil
}

func validTimezone(name string) bool {
	if name == "" || name == "Local" {
		return false
	}
	_, err := time.LoadLocation(name)
	return err == nil
}

func hasControlChars(value string) bool {
	return strings.ContainsFunc(value, unicode.IsControl)
}
