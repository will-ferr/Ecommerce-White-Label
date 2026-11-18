package domain

import (
	"encoding/json"
	"math"
	"strings"
	"testing"

	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/shared/apperr"
)

func TestValidateSlug(t *testing.T) {
	valid := []string{"aurora", "loja-01", "abc", strings.Repeat("a", 63)}
	for _, slug := range valid {
		if err := ValidateSlug(slug); err != nil {
			t.Errorf("ValidateSlug(%q) = %v", slug, err)
		}
	}
	invalid := []string{"", "ab", "-loja", "loja-", "Loja", "loja_a", "loja.a", "lojá", strings.Repeat("a", 64), "admin", "console", "api", "www"}
	for _, slug := range invalid {
		if err := ValidateSlug(slug); err == nil {
			t.Errorf("ValidateSlug(%q) accepted an invalid slug", slug)
		}
	}
}

func TestNormalizeHost(t *testing.T) {
	valid := map[string]string{
		"LOJA.com.BR":        "loja.com.br",
		"loja.com.br:8443":   "loja.com.br",
		"loja.com.br.":       "loja.com.br",
		" aurora.localhost ": "aurora.localhost",
		"xn--caf-dma.com.br": "xn--caf-dma.com.br",
	}
	for raw, want := range valid {
		if got, ok := NormalizeHost(raw); !ok || got != want {
			t.Errorf("NormalizeHost(%q) = %q, %v; want %q", raw, got, ok, want)
		}
	}
	invalid := []string{
		"", "localhost", "127.0.0.1", "127.0.0.1:8080", "[::1]:8080", "::1", "api:8080",
		"loja..com", "-loja.com", "loja-.com", "loja_a.com", "lojá.com", "loja.com/path",
		"loja.com\r\nX-Injected: 1", "user@loja.com", "' OR 1=1 --.com", strings.Repeat("a.", 130) + "com",
	}
	for _, raw := range invalid {
		if got, ok := NormalizeHost(raw); ok {
			t.Errorf("NormalizeHost(%q) accepted as %q", raw, got)
		}
	}
}

func TestValidateCustomHostRefusesPlatformDomains(t *testing.T) {
	if _, err := ValidateCustomHost("loja.exemplo.com", "plataforma.com"); err != nil {
		t.Errorf("valid domain refused: %v", err)
	}
	for _, host := range []string{"plataforma.com", "outra-loja.plataforma.com", "a.b.plataforma.com"} {
		if _, err := ValidateCustomHost(host, "plataforma.com"); err == nil {
			t.Errorf("%s must be refused", host)
		}
	}
	if _, err := ValidateCustomHost("evilplataforma.com", "plataforma.com"); err != nil {
		t.Errorf("a different domain with a similar suffix must be accepted: %v", err)
	}
}

func TestFeatureRequiresAllThreeSwitches(t *testing.T) {
	for mask := range 8 {
		state := FeatureState{GloballyEnabled: mask&1 != 0, InPlan: mask&2 != 0, TenantEnabled: mask&4 != 0}
		if got, want := state.Enabled(), mask == 7; got != want {
			t.Errorf("%+v Enabled() = %v, want %v", state, got, want)
		}
	}
}

func TestSettingsValidate(t *testing.T) {
	if err := DefaultSettings("Loja Aurora").Validate(); err != nil {
		t.Fatalf("default settings are invalid: %v", err)
	}
	tests := map[string]func(*Settings){
		"empty name":         func(s *Settings) { s.StoreName = "" },
		"long name":          func(s *Settings) { s.StoreName = strings.Repeat("a", 121) },
		"control characters": func(s *Settings) { s.StoreName = "Loja\nFalsa" },
		"bad currency":       func(s *Settings) { s.Currency = "REAL" },
		"lowercase currency": func(s *Settings) { s.Currency = "brl" },
		"unknown currency":   func(s *Settings) { s.Currency = "ZZZ" },
		"bad locale":         func(s *Settings) { s.Locale = "idioma invalido!" },
		"bad timezone":       func(s *Settings) { s.Timezone = "Brasil/Curitiba" },
		"local timezone":     func(s *Settings) { s.Timezone = "Local" },
		"empty timezone":     func(s *Settings) { s.Timezone = "" },
		"long SEO title":     func(s *Settings) { s.SEOTitle = strings.Repeat("a", 71) },
		"long description":   func(s *Settings) { s.SEODescription = strings.Repeat("a", 161) },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			settings := DefaultSettings("Loja")
			mutate(&settings)
			if err := settings.Validate(); err == nil {
				t.Error("Validate() accepted invalid settings")
			}
		})
	}
}

func themeJSON(t *testing.T, mutate func(map[string]any)) []byte {
	t.Helper()
	raw, err := json.Marshal(DefaultTheme())
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	mutate(doc)
	out, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func section(doc map[string]any, name string) map[string]any {
	value, _ := doc[name].(map[string]any)
	return value
}

func TestDefaultThemeIsValid(t *testing.T) {
	if _, err := ParseTheme(themeJSON(t, func(map[string]any) {})); err != nil {
		t.Fatalf("default theme is invalid: %v", err)
	}
}

func TestParseThemeRefusesUnsafeValues(t *testing.T) {
	attacks := []string{
		"red", "#fff", "#GGGGGG", "#0D5F52", "#0d5f52;background:url(https://evil.test)",
		"#0d5f52}body{display:none}", "</style><script>alert(1)</script>", "var(--x)", "expression(alert(1))",
	}
	for _, attack := range attacks {
		t.Run(attack, func(t *testing.T) {
			data := themeJSON(t, func(doc map[string]any) { section(doc, "colors")["primary"] = attack })
			if _, err := ParseTheme(data); err == nil {
				t.Errorf("color %q was accepted", attack)
			}
		})
	}

	tests := map[string]func(map[string]any){
		"unknown root field":  func(doc map[string]any) { doc["customCss"] = "body{display:none}" },
		"unknown color":       func(doc map[string]any) { section(doc, "colors")["danger"] = "#000000" },
		"font outside list":   func(doc map[string]any) { section(doc, "typography")["body"] = "Comic Sans'; }" },
		"radius outside list": func(doc map[string]any) { section(doc, "shape")["radius"] = "999px" },
		"unknown density":     func(doc map[string]any) { section(doc, "spacing")["density"] = "tight" },
		"unknown layout":      func(doc map[string]any) { section(doc, "layout")["header"] = "../../evil" },
		"http logo":           func(doc map[string]any) { section(doc, "assets")["logoUrl"] = "http://cdn.test/logo.png" },
		"script logo":         func(doc map[string]any) { section(doc, "assets")["logoUrl"] = "javascript:alert(1)" },
		"data favicon": func(doc map[string]any) {
			section(doc, "assets")["faviconUrl"] = "data:image/svg+xml,<svg onload=alert(1)>"
		},
		"credentials in logo": func(doc map[string]any) { section(doc, "assets")["logoUrl"] = "https://user:pass@cdn.test/logo.png" },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseTheme(themeJSON(t, mutate)); err == nil {
				t.Error("ParseTheme() accepted an unsafe theme")
			}
		})
	}
}

func TestParseThemeRefusesLowContrast(t *testing.T) {
	data := themeJSON(t, func(doc map[string]any) { section(doc, "colors")["onPrimary"] = "#10665a" })

	_, err := ParseTheme(data)

	appErr, ok := apperr.As(err)
	if !ok || len(appErr.Fields) == 0 || appErr.Fields[0].Code != "low_contrast" {
		t.Fatalf("expected a contrast error, got %v", err)
	}
}

func TestParseThemeRefusesMalformedDocuments(t *testing.T) {
	inputs := map[string][]byte{
		"not json":     []byte("{"),
		"two objects":  append(themeJSON(t, func(map[string]any) {}), []byte(" {}")...),
		"too large":    []byte(`{"padding":"` + strings.Repeat("a", maxThemeBytes) + `"}`),
		"empty object": []byte("{}"),
	}
	for name, data := range inputs {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseTheme(data); err == nil {
				t.Error("ParseTheme() accepted a malformed document")
			}
		})
	}
}

func TestContrastRatioMatchesWCAGReferences(t *testing.T) {
	if got := contrastRatio("#000000", "#ffffff"); math.Abs(got-21) > 0.01 {
		t.Errorf("black on white = %.2f", got)
	}
	if got := contrastRatio("#777777", "#ffffff"); math.Abs(got-4.48) > 0.01 {
		t.Errorf("#777777 on white = %.2f", got)
	}
}
