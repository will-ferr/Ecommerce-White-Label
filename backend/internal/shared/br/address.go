// Package br holds Brazilian data formats.
package br

import (
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/shared/apperr"
)

// States lists the federative units.
var States = []string{
	"AC", "AL", "AP", "AM", "BA", "CE", "DF", "ES", "GO", "MA", "MT", "MS", "MG", "PA", "PB", "PR",
	"PE", "PI", "RJ", "RN", "RS", "RO", "RR", "SC", "SP", "SE", "TO",
}

// Address is a Brazilian postal address.
type Address struct {
	Recipient  string `json:"recipient"`
	PostalCode string `json:"postalCode"`
	Street     string `json:"street"`
	Number     string `json:"number"`
	Complement string `json:"complement"`
	District   string `json:"district"`
	City       string `json:"city"`
	State      string `json:"state"`
}

func clean(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func validText(value string, minimum, maximum int) bool {
	length := utf8.RuneCountInString(value)
	return length >= minimum && length <= maximum && !strings.ContainsFunc(value, unicode.IsControl)
}

// Normalize trims the fields and reports every invalid one.
func (a Address) Normalize() (Address, error) {
	a.Recipient, a.Street, a.Number = clean(a.Recipient), clean(a.Street), clean(a.Number)
	a.Complement, a.District, a.City = clean(a.Complement), clean(a.District), clean(a.City)
	a.State = strings.ToUpper(clean(a.State))
	a.PostalCode = strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		if r == '-' || r == '.' || r == ' ' {
			return -1
		}
		return 'x'
	}, a.PostalCode)

	var fields []apperr.FieldError
	add := func(field, message string) {
		fields = append(fields, apperr.FieldError{Field: "address." + field, Code: "invalid", Message: message})
	}
	if !validText(a.Recipient, 2, 120) {
		add("recipient", "Enter the name of the recipient.")
	}
	if len(a.PostalCode) != 8 || strings.Contains(a.PostalCode, "x") {
		add("postalCode", "Enter a CEP with 8 digits.")
	}
	if !validText(a.Street, 2, 160) {
		add("street", "Enter the street.")
	}
	if !validText(a.Number, 1, 20) {
		add("number", "Enter the number, or S/N.")
	}
	if !validText(a.Complement, 0, 80) {
		add("complement", "Use up to 80 characters.")
	}
	if !validText(a.District, 2, 80) {
		add("district", "Enter the district.")
	}
	if !validText(a.City, 2, 80) {
		add("city", "Enter the city.")
	}
	if !slices.Contains(States, a.State) {
		add("state", "Choose a valid state.")
	}
	if len(fields) > 0 {
		return Address{}, apperr.Validation(fields...)
	}
	return a, nil
}
