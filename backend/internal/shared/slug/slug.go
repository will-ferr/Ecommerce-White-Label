// Package slug turns names into address segments.
package slug

import (
	"regexp"
	"strings"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

var pattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// Make turns a name into an address segment of up to limit bytes:
// "Café Especial" becomes "cafe-especial".
func Make(text string, limit int) string {
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
	result := strings.TrimSuffix(builder.String(), "-")
	if len(result) > limit {
		result = strings.TrimSuffix(result[:limit], "-")
	}
	return result
}

// Valid reports whether a segment has only lowercase letters, digits and single hyphens.
func Valid(value string, limit int) bool {
	return len(value) <= limit && pattern.MatchString(value)
}
