package config

import (
	"encoding/base64"
	"fmt"
	"strings"
	"testing"
	"time"
)

func validEnv() map[string]string {
	return map[string]string{
		"APP_ENV":     "development",
		"DB_HOST":     "postgres",
		"DB_NAME":     "ecommerce",
		"DB_USER":     "app_runtime",
		"DB_PASSWORD": "p@ss/word",
		"DB_SSLMODE":  "disable",
		"REDIS_ADDR":  "redis:6379",

		"AUTH_SIGNING_KEY":   base64.StdEncoding.EncodeToString([]byte("test-signing-key-with-32-bytes-0")),
		"MFA_ENCRYPTION_KEY": base64.StdEncoding.EncodeToString([]byte("test-mfa-key-with-exact-32-bytes")),
		"ADMIN_PUBLIC_URL":   "https://admin.example.com",
		"CONSOLE_PUBLIC_URL": "https://console.example.com",
		"SMTP_ADDR":          "mail.example.com:587",
		"SMTP_FROM":          "no-reply@example.com",
	}
}

func hardenedEnv() map[string]string {
	env := validEnv()
	env["APP_ENV"] = "production"
	env["DB_SSLMODE"] = "verify-full"
	env["REDIS_TLS"] = "true"
	env["REDIS_PASSWORD"] = "redis-secret"
	return env
}

func lookup(env map[string]string) Lookup {
	return func(key string) (string, bool) {
		v, ok := env[key]
		return v, ok
	}
}

func TestLoadAppliesDefaults(t *testing.T) {
	cfg, err := Load(lookup(validEnv()))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.HTTP.Addr != ":8080" || cfg.HTTP.RequestTimeout != 15*time.Second {
		t.Errorf("unexpected HTTP defaults: %+v", cfg.HTTP)
	}
	if cfg.Database.Port != 5432 || cfg.Database.MaxConns != 20 {
		t.Errorf("unexpected database defaults: %+v", cfg.Database)
	}
}

func TestLoadReportsEveryProblemAtOnce(t *testing.T) {
	_, err := Load(lookup(map[string]string{"HTTP_MAX_BODY_BYTES": "1"}))
	if err == nil {
		t.Fatal("Load() expected an error")
	}
	for _, want := range []string{"APP_ENV", "DB_HOST", "DB_PASSWORD", "REDIS_ADDR", "HTTP_MAX_BODY_BYTES"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %s", err, want)
		}
	}
}

func TestLoadRejectsInvalidValues(t *testing.T) {
	tests := map[string]struct{ key, value string }{
		"unknown environment": {"APP_ENV", "qa"},
		"bad log level":       {"LOG_LEVEL", "LOUD"},
		"bad duration":        {"HTTP_READ_TIMEOUT", "soon"},
		"negative duration":   {"HTTP_DRAIN_DELAY", "-1s"},
		"bad boolean":         {"REDIS_TLS", "maybe"},
		"port out of range":   {"DB_PORT", "70000"},
		"bad ssl mode":        {"DB_SSLMODE", "prefer"},
		"write below request": {"HTTP_WRITE_TIMEOUT", "1s"},
		"min conns above max": {"DB_MIN_CONNS", "400"},
		"short signing key":   {"AUTH_SIGNING_KEY", "c2hvcnQ="},
		"key is not base64":   {"MFA_ENCRYPTION_KEY", "not base64!"},
		"relative admin url":  {"ADMIN_PUBLIC_URL", "admin.example.com"},
		"bad trusted proxy":   {"TRUSTED_PROXIES", "10.0.0.1"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			env := validEnv()
			env[tt.key] = tt.value
			if _, err := Load(lookup(env)); err == nil {
				t.Errorf("Load() accepted %s=%s", tt.key, tt.value)
			}
		})
	}
}

func TestHardenedEnvironmentsRequireSecureTransport(t *testing.T) {
	tests := map[string]struct{ key, value string }{
		"database without verification": {"DB_SSLMODE", "require"},
		"redis without tls":             {"REDIS_TLS", "false"},
		"redis without password":        {"REDIS_PASSWORD", ""},
		"insecure telemetry":            {"TELEMETRY_OTLP_INSECURE", "true"},
		"insecure cookie":               {"AUTH_COOKIE_SECURE", "false"},
		"rate limit disabled":           {"RATE_LIMIT_ENABLED", "false"},
		"mail without tls":              {"SMTP_REQUIRE_TLS", "false"},
		"store links without tls":       {"STORE_PUBLIC_SCHEME", "http"},
		"sample signing key":            {"AUTH_SIGNING_KEY", base64.StdEncoding.EncodeToString([]byte("development-only-signing-key-001"))},
		"sample mfa key":                {"MFA_ENCRYPTION_KEY", base64.StdEncoding.EncodeToString([]byte("development-only-mfa-encrypt-001"))},
	}
	for _, environment := range []string{"staging", "production"} {
		for name, tt := range tests {
			t.Run(environment+"/"+name, func(t *testing.T) {
				env := hardenedEnv()
				env["APP_ENV"] = environment
				env[tt.key] = tt.value
				if _, err := Load(lookup(env)); err == nil {
					t.Errorf("Load() accepted %s=%q in %s", tt.key, tt.value, environment)
				}
			})
		}
	}
	if _, err := Load(lookup(hardenedEnv())); err != nil {
		t.Errorf("Load() rejected a valid production configuration: %v", err)
	}
}

func TestDSNEscapesCredentials(t *testing.T) {
	cfg, err := Load(lookup(validEnv()))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	want := "postgres://app_runtime:p%40ss%2Fword@postgres:5432/ecommerce?sslmode=disable"
	if got := cfg.Database.DSN(); got != want {
		t.Errorf("DSN() = %q, want %q", got, want)
	}
}

func TestConfigFormattingNeverExposesSecrets(t *testing.T) {
	env := validEnv()
	env["REDIS_PASSWORD"] = "redis-secret"
	cfg, err := Load(lookup(env))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	for _, format := range []string{"%v", "%+v", "%#v", "%s"} {
		out := fmt.Sprintf(format, cfg)
		if strings.Contains(out, "p@ss/word") || strings.Contains(out, "redis-secret") {
			t.Errorf("format %s exposed a secret: %s", format, out)
		}
	}
}

func TestLoadMigrationUsesPrivilegedCredentials(t *testing.T) {
	env := validEnv()
	env["MIGRATION_DB_USER"] = "app_owner"
	env["MIGRATION_DB_PASSWORD"] = "owner-secret"
	db, err := LoadMigration(lookup(env))
	if err != nil {
		t.Fatalf("LoadMigration() error = %v", err)
	}
	if db.User != "app_owner" || db.Password.Reveal() != "owner-secret" {
		t.Errorf("unexpected credentials for user %q", db.User)
	}
}
