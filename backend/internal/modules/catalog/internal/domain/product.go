// Package domain holds the catalog rules.
package domain

import (
	"maps"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"

	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/shared/apperr"
)

// Product statuses.
const (
	StatusDraft    = "draft"
	StatusActive   = "active"
	StatusArchived = "archived"
)

// Limits of a product.
const (
	MaxPriceAmount  = 100_000_000_00
	maxVariants     = 50
	maxImages       = 10
	maxOptions      = 3
	maxOptionLength = 40
)

var (
	slugPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	skuPattern  = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)
	gtinPattern = regexp.MustCompile(`^[0-9]{8,14}$`)
)

// Slugify turns a name into an address segment: "Café Especial" becomes "cafe-especial".
func Slugify(text string) string {
	decomposed, _, err := transform.String(transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC), text)
	if err != nil {
		decomposed = text
	}
	var builder strings.Builder
	dash := false
	for _, r := range strings.ToLower(decomposed) {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			builder.WriteRune(r)
			dash = false
		case builder.Len() > 0 && !dash:
			builder.WriteByte('-')
			dash = true
		}
	}
	slug := strings.TrimSuffix(builder.String(), "-")
	if len(slug) > 120 {
		slug = strings.TrimSuffix(slug[:120], "-")
	}
	return slug
}

// Image is a product picture hosted on HTTPS.
type Image struct {
	URL string `json:"url"`
	Alt string `json:"alt"`
}

// VariantInput is one sellable version of a product.
type VariantInput struct {
	ID          *uuid.UUID        `json:"id"`
	SKU         string            `json:"sku"`
	GTIN        string            `json:"gtin"`
	Options     map[string]string `json:"options"`
	WeightGrams int32             `json:"weightGrams"`
	Price       int64             `json:"price"`
	CompareAt   *int64            `json:"compareAtPrice"`
	Cost        *int64            `json:"costPrice"`
}

// ProductInput is what an administrator sends to create or change a product.
type ProductInput struct {
	Name           string         `json:"name"`
	Slug           string         `json:"slug"`
	Description    string         `json:"description"`
	Brand          string         `json:"brand"`
	Status         string         `json:"status"`
	CategoryID     *uuid.UUID     `json:"categoryId"`
	Featured       bool           `json:"featured"`
	SEOTitle       string         `json:"seoTitle"`
	SEODescription string         `json:"seoDescription"`
	Images         []Image        `json:"images"`
	Variants       []VariantInput `json:"variants"`
}

type errors struct{ fields []apperr.FieldError }

func (e *errors) add(field, code, message string) {
	e.fields = append(e.fields, apperr.FieldError{Field: field, Code: code, Message: message})
}

func textOK(value string, minimum, maximum int, multiline bool) bool {
	length := utf8.RuneCountInString(value)
	if length < minimum || length > maximum {
		return false
	}
	return !strings.ContainsFunc(value, func(r rune) bool {
		allowedBreak := multiline && (r == '\n' || r == '\r')
		return unicode.IsControl(r) && !allowedBreak
	})
}

func httpsURL(raw string) bool {
	parsed, err := url.Parse(raw)
	return err == nil && len(raw) <= 2048 && parsed.Scheme == "https" && parsed.Host != "" && parsed.User == nil
}

// Normalize cleans the input and reports every invalid field.
func (p ProductInput) Normalize() (ProductInput, error) {
	p.Name, p.Brand = strings.TrimSpace(p.Name), strings.TrimSpace(p.Brand)
	p.SEOTitle, p.SEODescription = strings.TrimSpace(p.SEOTitle), strings.TrimSpace(p.SEODescription)
	p.Description = strings.TrimSpace(p.Description)
	p.Slug = strings.TrimSpace(p.Slug)
	if p.Slug == "" {
		p.Slug = Slugify(p.Name)
	}

	var errs errors
	if !textOK(p.Name, 1, 160, false) {
		errs.add("name", "invalid", "Enter a name with up to 160 characters.")
	}
	if !slugPattern.MatchString(p.Slug) || len(p.Slug) > 120 {
		errs.add("slug", "invalid", "Use lowercase letters, numbers and hyphens.")
	}
	if !textOK(p.Description, 0, 5000, true) {
		errs.add("description", "invalid", "Use up to 5000 characters.")
	}
	if !textOK(p.Brand, 0, 80, false) {
		errs.add("brand", "invalid", "Use up to 80 characters.")
	}
	if !slices.Contains([]string{StatusDraft, StatusActive, StatusArchived}, p.Status) {
		errs.add("status", "invalid", "Choose a valid status.")
	}
	if !textOK(p.SEOTitle, 0, 70, false) {
		errs.add("seoTitle", "invalid", "Use up to 70 characters.")
	}
	if !textOK(p.SEODescription, 0, 160, false) {
		errs.add("seoDescription", "invalid", "Use up to 160 characters.")
	}

	if len(p.Images) > maxImages {
		errs.add("images", "too_many", "Use up to 10 images.")
	}
	for i := range p.Images {
		p.Images[i].Alt = strings.TrimSpace(p.Images[i].Alt)
		if !httpsURL(p.Images[i].URL) || !textOK(p.Images[i].Alt, 0, 160, false) {
			errs.add("images", "invalid", "Use HTTPS addresses for images.")
			break
		}
	}

	p.Variants = normalizeVariants(p.Variants, &errs)
	if len(errs.fields) > 0 {
		return ProductInput{}, apperr.Validation(errs.fields...)
	}
	return p, nil
}

func normalizeVariants(variants []VariantInput, errs *errors) []VariantInput {
	if len(variants) == 0 || len(variants) > maxVariants {
		errs.add("variants", "count", "A product needs from 1 to 50 variants.")
		return variants
	}
	skus := map[string]struct{}{}
	combinations := map[string]struct{}{}
	var optionNames []string

	for i := range variants {
		v := &variants[i]
		v.SKU, v.GTIN = strings.TrimSpace(v.SKU), strings.TrimSpace(v.GTIN)
		if !skuPattern.MatchString(v.SKU) {
			errs.add("variants.sku", "invalid", "Use up to 64 letters, numbers, dots, hyphens or underscores.")
		}
		if _, duplicate := skus[strings.ToLower(v.SKU)]; duplicate {
			errs.add("variants.sku", "duplicate", "Each variant needs its own SKU.")
		}
		skus[strings.ToLower(v.SKU)] = struct{}{}
		if v.GTIN != "" && !gtinPattern.MatchString(v.GTIN) {
			errs.add("variants.gtin", "invalid", "Use a GTIN with 8 to 14 digits.")
		}
		if v.Price < 0 || v.Price > MaxPriceAmount {
			errs.add("variants.price", "invalid", "Enter a valid price.")
		}
		if v.CompareAt != nil && (*v.CompareAt <= v.Price || *v.CompareAt > MaxPriceAmount) {
			errs.add("variants.compareAtPrice", "invalid", "The original price must be higher than the price.")
		}
		if v.Cost != nil && (*v.Cost < 0 || *v.Cost > MaxPriceAmount) {
			errs.add("variants.costPrice", "invalid", "Enter a valid cost.")
		}
		if v.WeightGrams < 0 || v.WeightGrams > 1_000_000 {
			errs.add("variants.weightGrams", "invalid", "Enter a valid weight.")
		}

		cleaned := make(map[string]string, len(v.Options))
		for key, value := range v.Options {
			key, value = strings.TrimSpace(key), strings.TrimSpace(value)
			if !textOK(key, 1, maxOptionLength, false) || !textOK(value, 1, maxOptionLength, false) {
				errs.add("variants.options", "invalid", "Option names and values need 1 to 40 characters.")
			}
			cleaned[key] = value
		}
		v.Options = cleaned
		names := slices.Sorted(maps.Keys(cleaned))
		if len(names) > maxOptions {
			errs.add("variants.options", "too_many", "Use up to 3 options, such as color and size.")
		}
		if i == 0 {
			optionNames = names
		} else if !slices.Equal(optionNames, names) {
			errs.add("variants.options", "inconsistent", "Every variant must use the same option names.")
		}
		label := VariantLabel(cleaned)
		if _, duplicate := combinations[label]; duplicate {
			errs.add("variants.options", "duplicate", "Two variants have the same options.")
		}
		combinations[label] = struct{}{}
	}
	return variants
}

// VariantLabel describes a variant by its option values in a stable order.
func VariantLabel(options map[string]string) string {
	values := make([]string, 0, len(options))
	for _, key := range slices.Sorted(maps.Keys(options)) {
		values = append(values, options[key])
	}
	return strings.Join(values, " / ")
}

// CategoryInput is what an administrator sends to create or change a category.
type CategoryInput struct {
	Name        string     `json:"name"`
	Slug        string     `json:"slug"`
	Description string     `json:"description"`
	ParentID    *uuid.UUID `json:"parentId"`
	Position    int32      `json:"position"`
}

// Normalize cleans the input and reports every invalid field.
func (c CategoryInput) Normalize() (CategoryInput, error) {
	c.Name, c.Description = strings.TrimSpace(c.Name), strings.TrimSpace(c.Description)
	c.Slug = strings.TrimSpace(c.Slug)
	if c.Slug == "" {
		c.Slug = Slugify(c.Name)
	}
	var errs errors
	if !textOK(c.Name, 1, 80, false) {
		errs.add("name", "invalid", "Enter a name with up to 80 characters.")
	}
	if !slugPattern.MatchString(c.Slug) || len(c.Slug) > 80 {
		errs.add("slug", "invalid", "Use lowercase letters, numbers and hyphens.")
	}
	if !textOK(c.Description, 0, 1000, true) {
		errs.add("description", "invalid", "Use up to 1000 characters.")
	}
	if len(errs.fields) > 0 {
		return CategoryInput{}, apperr.Validation(errs.fields...)
	}
	return c, nil
}
