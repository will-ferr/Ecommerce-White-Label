package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"time"

	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/security"
)

// Worker configures the process that publishes events, consumes them and runs
// scheduled jobs.
type Worker struct {
	HealthAddr         string
	RabbitMQ           RabbitMQ
	RelayInterval      time.Duration
	MailInterval       time.Duration
	MessagingRetention time.Duration
}

// RabbitMQ configures the broker connection.
type RabbitMQ struct {
	Host     string
	Port     int
	VHost    string
	User     string
	Password security.Secret
	TLS      bool
}

// URL returns the connection address. It contains the password and must never
// be logged.
func (r RabbitMQ) URL() string {
	scheme := "amqp"
	if r.TLS {
		scheme = "amqps"
	}
	u := url.URL{
		Scheme: scheme,
		User:   url.UserPassword(r.User, r.Password.Reveal()),
		Host:   net.JoinHostPort(r.Host, strconv.Itoa(r.Port)),
		Path:   "/" + r.VHost,
	}
	return u.String()
}

// LoadWorker reads the configuration of the worker process: everything the
// API needs plus the broker and the job intervals.
func LoadWorker(lookup Lookup) (Config, Worker, error) {
	cfg, err := Load(lookup)
	r := reader{lookup: lookup}
	worker := Worker{
		HealthAddr: r.str("WORKER_HEALTH_ADDR", ":8081"),
		RabbitMQ: RabbitMQ{
			Host:     r.required("RABBITMQ_HOST"),
			Port:     r.integer("RABBITMQ_PORT", 5672, 1, 65535),
			VHost:    r.str("RABBITMQ_VHOST", ""),
			User:     r.required("RABBITMQ_USER"),
			Password: security.NewSecret(r.required("RABBITMQ_PASSWORD")),
			TLS:      r.boolean("RABBITMQ_TLS", false),
		},
		RelayInterval:      r.duration("MESSAGING_RELAY_INTERVAL", 500*time.Millisecond),
		MailInterval:       r.duration("MAIL_DISPATCH_INTERVAL", 2*time.Second),
		MessagingRetention: r.duration("MESSAGING_RETENTION", 7*24*time.Hour),
	}
	r.check(worker.RelayInterval >= 10*time.Millisecond, "MESSAGING_RELAY_INTERVAL must be at least 10ms")
	r.check(worker.MailInterval >= 100*time.Millisecond, "MAIL_DISPATCH_INTERVAL must be at least 100ms")
	r.check(worker.MessagingRetention >= 24*time.Hour, "MESSAGING_RETENTION must be at least 24h")
	if cfg.Environment.IsHardened() {
		r.check(worker.RabbitMQ.TLS, "RABBITMQ_TLS must be true in staging and production")
	}
	if err := errors.Join(err, errors.Join(r.errs...)); err != nil {
		return Config{}, Worker{}, fmt.Errorf("invalid worker configuration: %w", err)
	}
	return cfg, worker, nil
}
