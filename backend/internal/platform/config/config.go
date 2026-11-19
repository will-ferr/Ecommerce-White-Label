// Package config loads and validates the process configuration from the
// environment. Invalid configuration stops the process during startup.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/security"
)

// Environment identifies where the process is running.
type Environment string

// Supported environments.
const (
	Development Environment = "development"
	Testing     Environment = "testing"
	Staging     Environment = "staging"
	Production  Environment = "production"
)

// IsHardened reports whether the environment must meet production-grade rules.
func (e Environment) IsHardened() bool { return e == Staging || e == Production }

// Config is the complete API process configuration.
type Config struct {
	Environment Environment
	ServiceName string
	LogLevel    slog.Level
	HTTP        HTTP
	Database    Database
	Redis       Redis
	Telemetry   Telemetry
	Auth        Auth
}

// HTTP configures the HTTP server.
type HTTP struct {
	Addr              string
	ReadHeaderTimeout time.Duration
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
	RequestTimeout    time.Duration
	ShutdownTimeout   time.Duration
	DrainDelay        time.Duration
	MaxBodyBytes      int64
}

// Database configures a PostgreSQL connection pool.
type Database struct {
	Host     string
	Port     int
	Name     string
	User     string
	Password security.Secret
	SSLMode  string
	MaxConns int32
	MinConns int32
}

// DSN returns the connection string. It contains the password and must never
// be logged.
func (d Database) DSN() string {
	u := url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(d.User, d.Password.Reveal()),
		Host:     net.JoinHostPort(d.Host, strconv.Itoa(d.Port)),
		Path:     d.Name,
		RawQuery: url.Values{"sslmode": {d.SSLMode}}.Encode(),
	}
	return u.String()
}

// Redis configures the Redis client.
type Redis struct {
	Addr     string
	Password security.Secret
	DB       int
	TLS      bool
}

// Telemetry configures the OpenTelemetry exporters.
type Telemetry struct {
	Enabled        bool
	OTLPEndpoint   string
	OTLPInsecure   bool
	MetricInterval time.Duration
}

// Lookup reads a configuration key. os.LookupEnv satisfies it.
type Lookup func(key string) (string, bool)

// Load reads the API configuration and reports every invalid value at once.
func Load(lookup Lookup) (Config, error) {
	r := reader{lookup: lookup}

	cfg := Config{
		Environment: r.environment(),
		ServiceName: r.str("SERVICE_NAME", "ecommerce-api"),
		LogLevel:    r.logLevel(),
		HTTP: HTTP{
			Addr:              r.str("HTTP_ADDR", ":8080"),
			ReadHeaderTimeout: r.duration("HTTP_READ_HEADER_TIMEOUT", 5*time.Second),
			ReadTimeout:       r.duration("HTTP_READ_TIMEOUT", 15*time.Second),
			WriteTimeout:      r.duration("HTTP_WRITE_TIMEOUT", 30*time.Second),
			IdleTimeout:       r.duration("HTTP_IDLE_TIMEOUT", 60*time.Second),
			RequestTimeout:    r.duration("HTTP_REQUEST_TIMEOUT", 15*time.Second),
			ShutdownTimeout:   r.duration("HTTP_SHUTDOWN_TIMEOUT", 20*time.Second),
			DrainDelay:        r.duration("HTTP_DRAIN_DELAY", 0),
			MaxBodyBytes:      int64(r.integer("HTTP_MAX_BODY_BYTES", 1<<20, 1024, 50<<20)),
		},
		Database: r.database("DB_USER", "DB_PASSWORD"),
		Redis: Redis{
			Addr:     r.required("REDIS_ADDR"),
			Password: security.NewSecret(r.str("REDIS_PASSWORD", "")),
			DB:       r.integer("REDIS_DB", 0, 0, 15),
			TLS:      r.boolean("REDIS_TLS", false),
		},
		Telemetry: Telemetry{
			Enabled:        r.boolean("TELEMETRY_ENABLED", true),
			OTLPEndpoint:   r.str("TELEMETRY_OTLP_ENDPOINT", "otel-collector:4317"),
			OTLPInsecure:   r.boolean("TELEMETRY_OTLP_INSECURE", false),
			MetricInterval: r.duration("TELEMETRY_METRIC_INTERVAL", 15*time.Second),
		},
		Auth: r.auth(),
	}

	r.check(cfg.HTTP.WriteTimeout > cfg.HTTP.RequestTimeout,
		"HTTP_WRITE_TIMEOUT must be greater than HTTP_REQUEST_TIMEOUT")
	r.check(cfg.Database.MinConns <= cfg.Database.MaxConns,
		"DB_MIN_CONNS must not exceed DB_MAX_CONNS")

	if cfg.Environment.IsHardened() {
		r.check(cfg.Database.SSLMode == "verify-full" || cfg.Database.SSLMode == "verify-ca",
			"DB_SSLMODE must be verify-full or verify-ca in staging and production")
		r.check(cfg.Redis.TLS, "REDIS_TLS must be true in staging and production")
		r.check(!cfg.Redis.Password.IsZero(), "REDIS_PASSWORD is required in staging and production")
		r.check(!cfg.Telemetry.OTLPInsecure, "TELEMETRY_OTLP_INSECURE must be false in staging and production")
		r.checkHardenedAuth(cfg.Auth)
	}

	if err := errors.Join(r.errs...); err != nil {
		return Config{}, fmt.Errorf("invalid configuration: %w", err)
	}
	return cfg, nil
}

// LoadMigration reads the privileged connection used only to apply migrations.
func LoadMigration(lookup Lookup) (Database, error) {
	r := reader{lookup: lookup}
	db := r.database("MIGRATION_DB_USER", "MIGRATION_DB_PASSWORD")
	if err := errors.Join(r.errs...); err != nil {
		return Database{}, fmt.Errorf("invalid migration configuration: %w", err)
	}
	return db, nil
}

type reader struct {
	lookup Lookup
	errs   []error
}

func (r *reader) value(key string) (string, bool) {
	v, ok := r.lookup(key)
	v = strings.TrimSpace(v)
	return v, ok && v != ""
}

func (r *reader) fail(format string, args ...any) {
	r.errs = append(r.errs, fmt.Errorf(format, args...))
}

func (r *reader) check(ok bool, message string) {
	if !ok {
		r.errs = append(r.errs, errors.New(message))
	}
}

func (r *reader) str(key, fallback string) string {
	if v, ok := r.value(key); ok {
		return v
	}
	return fallback
}

func (r *reader) required(key string) string {
	v, ok := r.value(key)
	if !ok {
		r.fail("%s is required", key)
	}
	return v
}

func (r *reader) integer(key string, fallback, minimum, maximum int) int {
	raw, ok := r.value(key)
	if !ok {
		return fallback
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v < minimum || v > maximum {
		r.fail("%s must be an integer between %d and %d", key, minimum, maximum)
		return fallback
	}
	return v
}

func (r *reader) boolean(key string, fallback bool) bool {
	raw, ok := r.value(key)
	if !ok {
		return fallback
	}
	v, err := strconv.ParseBool(raw)
	if err != nil {
		r.fail("%s must be a boolean", key)
		return fallback
	}
	return v
}

func (r *reader) duration(key string, fallback time.Duration) time.Duration {
	raw, ok := r.value(key)
	if !ok {
		return fallback
	}
	v, err := time.ParseDuration(raw)
	if err != nil || v < 0 {
		r.fail("%s must be a non-negative duration", key)
		return fallback
	}
	return v
}

func (r *reader) environment() Environment {
	env := Environment(r.required("APP_ENV"))
	if env != "" && !slices.Contains([]Environment{Development, Testing, Staging, Production}, env) {
		r.fail("APP_ENV must be development, testing, staging or production")
	}
	return env
}

func (r *reader) logLevel() slog.Level {
	var level slog.Level
	if err := level.UnmarshalText([]byte(r.str("LOG_LEVEL", "INFO"))); err != nil {
		r.fail("LOG_LEVEL must be DEBUG, INFO, WARN or ERROR")
		return slog.LevelInfo
	}
	return level
}

func (r *reader) database(userKey, passwordKey string) Database {
	sslMode := r.str("DB_SSLMODE", "verify-full")
	if !slices.Contains([]string{"disable", "require", "verify-ca", "verify-full"}, sslMode) {
		r.fail("DB_SSLMODE must be disable, require, verify-ca or verify-full")
	}
	return Database{
		Host:     r.required("DB_HOST"),
		Port:     r.integer("DB_PORT", 5432, 1, 65535),
		Name:     r.required("DB_NAME"),
		User:     r.required(userKey),
		Password: security.NewSecret(r.required(passwordKey)),
		SSLMode:  sslMode,
		MaxConns: int32(r.integer("DB_MAX_CONNS", 20, 1, 500)), //nolint:gosec // bounded by the range check
		MinConns: int32(r.integer("DB_MIN_CONNS", 2, 0, 500)),  //nolint:gosec // bounded by the range check
	}
}
