// Package cache manages the Redis client.
package cache

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"

	"github.com/redis/go-redis/extra/redisotel/v9"
	"github.com/redis/go-redis/v9"

	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/config"
)

// Connect creates an instrumented Redis client. Redis is an optional
// dependency, so an unreachable server does not stop the API from starting.
func Connect(cfg config.Redis) (*redis.Client, error) {
	options := &redis.Options{
		Addr:     cfg.Addr,
		Password: cfg.Password.Reveal(),
		DB:       cfg.DB,
	}
	if cfg.TLS {
		options.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	}

	client := redis.NewClient(options)
	if err := errors.Join(redisotel.InstrumentTracing(client), redisotel.InstrumentMetrics(client)); err != nil {
		return nil, errors.Join(fmt.Errorf("instrument redis client: %w", err), client.Close())
	}
	return client, nil
}

// Ping reports whether Redis answers.
func Ping(ctx context.Context, client *redis.Client) error {
	return client.Ping(ctx).Err()
}
