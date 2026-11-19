package domain

import (
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestNormalizeEmail(t *testing.T) {
	got, err := NormalizeEmail("  Maria.Silva@Exemplo.COM.br ")
	if err != nil || got != "maria.silva@exemplo.com.br" {
		t.Fatalf("NormalizeEmail() = %q, %v", got, err)
	}
	for _, bad := range []string{"", "maria", "maria@", "@exemplo.com", "maria@localhost", "a b@exemplo.com", "Maria <m@exemplo.com>", "m@exemplo.com\r\nBcc: x@y.com", strings.Repeat("a", 250) + "@exemplo.com"} {
		if _, err := NormalizeEmail(bad); err == nil {
			t.Errorf("NormalizeEmail(%q) accepted an invalid address", bad)
		}
	}
}

func TestValidatePassword(t *testing.T) {
	if err := ValidatePassword("cavalo correto bateria grampo", "maria@exemplo.com"); err != nil {
		t.Errorf("a long passphrase must be accepted: %v", err)
	}
	for name, password := range map[string]string{
		"short":          "curta123",
		"too long":       strings.Repeat("a", 129),
		"common":         "Senha123456",
		"contains email": "xxMARIAsilva99",
	} {
		t.Run(name, func(t *testing.T) {
			if err := ValidatePassword(password, "mariasilva@exemplo.com"); err == nil {
				t.Errorf("%q was accepted", password)
			}
		})
	}
}

func TestTOTPMatchesRFC6238Vectors(t *testing.T) {
	secret := []byte("12345678901234567890")
	vectors := map[int64]string{59: "287082", 1111111109: "081804", 1234567890: "005924", 2000000000: "279037"}
	for unix, want := range vectors {
		if got := TOTPCode(secret, unix/30); got != want {
			t.Errorf("code at %d = %s, want %s", unix, got, want)
		}
	}
}

func TestMatchTOTPToleratesOneStepOfDrift(t *testing.T) {
	secret := []byte("12345678901234567890")
	now := time.Unix(1_700_000_000, 0)
	current := now.Unix() / 30

	for _, offset := range []int64{-1, 0, 1} {
		step, ok := MatchTOTP(secret, TOTPCode(secret, current+offset), now)
		if !ok || step != current+offset {
			t.Errorf("offset %d: step=%d ok=%v", offset, step, ok)
		}
	}
	for _, offset := range []int64{-3, 3} {
		if _, ok := MatchTOTP(secret, TOTPCode(secret, current+offset), now); ok {
			t.Errorf("offset %d must be refused", offset)
		}
	}
	for _, bad := range []string{"", "12345", "1234567", "abcdef"} {
		if _, ok := MatchTOTP(secret, bad, now); ok {
			t.Errorf("code %q must be refused", bad)
		}
	}
}

func TestTOTPURIContainsEnrollmentData(t *testing.T) {
	parsed, err := url.Parse(TOTPURI("Aurora", "maria@exemplo.com", []byte("12345678901234567890")))
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Scheme != "otpauth" || parsed.Host != "totp" || parsed.Query().Get("issuer") != "Aurora" {
		t.Errorf("unexpected uri: %s", parsed)
	}
	if parsed.Query().Get("secret") != "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ" {
		t.Errorf("secret = %s", parsed.Query().Get("secret"))
	}
}

func TestRecoveryCodesAreUniqueAndNormalizable(t *testing.T) {
	codes, err := NewRecoveryCodes()
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]struct{}{}
	for _, code := range codes {
		seen[NormalizeRecoveryCode(code)] = struct{}{}
		if len(code) != 17 || code[8] != '-' {
			t.Errorf("unexpected format: %s", code)
		}
	}
	if len(codes) != 10 || len(seen) != 10 {
		t.Errorf("codes=%d unique=%d", len(codes), len(seen))
	}
	if NormalizeRecoveryCode("abcd-efgh ij") != "ABCDEFGHIJ" {
		t.Error("typed codes must be normalized")
	}
}

func TestDeviceLabel(t *testing.T) {
	ua := "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0 Safari/537.36"
	if got := DeviceLabel(ua); got != "Chrome em Windows" {
		t.Errorf("DeviceLabel() = %q", got)
	}
	if got := DeviceLabel("<script>alert(1)</script>"); strings.Contains(got, "script") {
		t.Errorf("the raw value must never be stored: %q", got)
	}
}
