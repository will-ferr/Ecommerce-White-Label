package domain

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/url"
	"regexp"
	"slices"
	"strconv"

	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/shared/apperr"
)

// Contrast ratios required by WCAG 2.1 AA for normal text.
const minTextContrast = 4.5

const (
	maxThemeBytes  = 16 * 1024
	maxAssetURLLen = 2048
)

// Allowed values. A theme selects from these lists and can never supply CSS.
var (
	allowedFonts    = []string{"system", "humanist", "geometric", "serif", "slab"}
	allowedRadius   = []string{"none", "small", "medium", "large"}
	allowedDensity  = []string{"compact", "comfortable", "spacious"}
	allowedHeaders  = []string{"classic", "centered"}
	hexColorPattern = regexp.MustCompile(`^#[0-9a-f]{6}$`)
)

// Theme is the validated visual identity of a store.
type Theme struct {
	Colors     ThemeColors     `json:"colors"`
	Typography ThemeTypography `json:"typography"`
	Shape      ThemeShape      `json:"shape"`
	Spacing    ThemeSpacing    `json:"spacing"`
	Layout     ThemeLayout     `json:"layout"`
	Assets     ThemeAssets     `json:"assets"`
}

// ThemeColors lists the color tokens.
type ThemeColors struct {
	Canvas        string `json:"canvas"`
	Surface       string `json:"surface"`
	Ink           string `json:"ink"`
	Muted         string `json:"muted"`
	Line          string `json:"line"`
	Primary       string `json:"primary"`
	PrimaryStrong string `json:"primaryStrong"`
	OnPrimary     string `json:"onPrimary"`
}

// ThemeTypography selects font families from the allowed list.
type ThemeTypography struct {
	Body    string `json:"body"`
	Display string `json:"display"`
}

// ThemeShape selects the corner radius.
type ThemeShape struct {
	Radius string `json:"radius"`
}

// ThemeSpacing selects the layout density.
type ThemeSpacing struct {
	Density string `json:"density"`
}

// ThemeLayout selects registered layout variants.
type ThemeLayout struct {
	Header string `json:"header"`
}

// ThemeAssets points to brand images.
type ThemeAssets struct {
	LogoURL    string `json:"logoUrl"`
	FaviconURL string `json:"faviconUrl"`
}

// DefaultTheme is applied to new stores.
func DefaultTheme() Theme {
	return Theme{
		Colors: ThemeColors{
			Canvas: "#f6f4ee", Surface: "#fffefb", Ink: "#18211f", Muted: "#58635f", Line: "#dcd8cc",
			Primary: "#0d5f52", PrimaryStrong: "#094a40", OnPrimary: "#ffffff",
		},
		Typography: ThemeTypography{Body: "system", Display: "serif"},
		Shape:      ThemeShape{Radius: "medium"},
		Spacing:    ThemeSpacing{Density: "comfortable"},
		Layout:     ThemeLayout{Header: "classic"},
	}
}

// ParseTheme decodes and validates a theme, refusing unknown fields.
func ParseTheme(data []byte) (Theme, error) {
	if len(data) > maxThemeBytes {
		return Theme{}, fieldError("theme", "too_large", "The theme must not exceed 16 KB.")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()

	var theme Theme
	if err := decoder.Decode(&theme); err != nil {
		return Theme{}, fieldError("theme", "malformed", "The theme is not valid JSON or has unknown fields.")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return Theme{}, fieldError("theme", "malformed", "The theme must contain a single JSON object.")
	}
	if err := theme.Validate(); err != nil {
		return Theme{}, err
	}
	return theme, nil
}

// Validate reports every invalid token at once.
func (t Theme) Validate() error {
	var fields []apperr.FieldError
	add := func(field, code, message string) {
		fields = append(fields, apperr.FieldError{Field: field, Code: code, Message: message})
	}

	colors := []struct{ name, value string }{
		{"colors.canvas", t.Colors.Canvas}, {"colors.surface", t.Colors.Surface},
		{"colors.ink", t.Colors.Ink}, {"colors.muted", t.Colors.Muted}, {"colors.line", t.Colors.Line},
		{"colors.primary", t.Colors.Primary}, {"colors.primaryStrong", t.Colors.PrimaryStrong},
		{"colors.onPrimary", t.Colors.OnPrimary},
	}
	colorsValid := true
	for _, color := range colors {
		if !hexColorPattern.MatchString(color.value) {
			colorsValid = false
			add(color.name, "invalid", "Use a lowercase hexadecimal color such as #0d5f52.")
		}
	}
	if colorsValid {
		pairs := []struct{ field, foreground, background string }{
			{"colors.ink", t.Colors.Ink, t.Colors.Canvas},
			{"colors.ink", t.Colors.Ink, t.Colors.Surface},
			{"colors.muted", t.Colors.Muted, t.Colors.Canvas},
			{"colors.muted", t.Colors.Muted, t.Colors.Surface},
			{"colors.primary", t.Colors.Primary, t.Colors.Canvas},
			{"colors.primary", t.Colors.Primary, t.Colors.Surface},
			{"colors.onPrimary", t.Colors.OnPrimary, t.Colors.Primary},
			{"colors.onPrimary", t.Colors.OnPrimary, t.Colors.PrimaryStrong},
		}
		for _, pair := range pairs {
			if ratio := contrastRatio(pair.foreground, pair.background); ratio < minTextContrast {
				add(pair.field, "low_contrast", fmt.Sprintf("Contrast against %s is %.2f:1; the minimum is 4.5:1.", pair.background, ratio))
			}
		}
	}

	choices := []struct {
		field, value string
		allowed      []string
	}{
		{"typography.body", t.Typography.Body, allowedFonts},
		{"typography.display", t.Typography.Display, allowedFonts},
		{"shape.radius", t.Shape.Radius, allowedRadius},
		{"spacing.density", t.Spacing.Density, allowedDensity},
		{"layout.header", t.Layout.Header, allowedHeaders},
	}
	for _, choice := range choices {
		if !slices.Contains(choice.allowed, choice.value) {
			add(choice.field, "not_allowed", "Choose one of the supported values.")
		}
	}

	for _, asset := range []struct{ field, value string }{
		{"assets.logoUrl", t.Assets.LogoURL}, {"assets.faviconUrl", t.Assets.FaviconURL},
	} {
		if !validAssetURL(asset.value) {
			add(asset.field, "invalid", "Use an absolute HTTPS address.")
		}
	}

	if len(fields) > 0 {
		return apperr.Validation(fields...)
	}
	return nil
}

func validAssetURL(raw string) bool {
	if raw == "" {
		return true
	}
	if len(raw) > maxAssetURLLen {
		return false
	}
	parsed, err := url.Parse(raw)
	return err == nil && parsed.Scheme == "https" && parsed.Host != "" && parsed.User == nil
}

// contrastRatio implements the WCAG 2.1 formula for two valid hex colors.
func contrastRatio(first, second string) float64 {
	lighter, darker := relativeLuminance(first), relativeLuminance(second)
	if darker > lighter {
		lighter, darker = darker, lighter
	}
	return (lighter + 0.05) / (darker + 0.05)
}

func relativeLuminance(hex string) float64 {
	channel := func(start int) float64 {
		value, err := strconv.ParseUint(hex[start:start+2], 16, 8)
		if err != nil {
			return 0
		}
		c := float64(value) / 255
		if c <= 0.03928 {
			return c / 12.92
		}
		return math.Pow((c+0.055)/1.055, 2.4)
	}
	return 0.2126*channel(1) + 0.7152*channel(3) + 0.0722*channel(5)
}
