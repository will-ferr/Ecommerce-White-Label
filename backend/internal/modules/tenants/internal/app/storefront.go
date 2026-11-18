package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/modules/tenants/internal/domain"
)

// StoredStorefront is the raw configuration of a store.
type StoredStorefront struct {
	Slug         string
	Settings     domain.Settings
	ThemeVersion int32
	ThemeTokens  json.RawMessage
	Features     []domain.FeatureState
}

// StorefrontReader loads the configuration of the tenant in the context.
type StorefrontReader interface {
	LoadStorefront(ctx context.Context) (StoredStorefront, error)
}

// StorefrontConfig is the public configuration of a store.
type StorefrontConfig struct {
	Slug         string
	Settings     domain.Settings
	ThemeVersion int32
	Theme        domain.Theme
	Features     map[string]bool
}

// Storefront serves the public configuration of the current store.
type Storefront struct {
	reader StorefrontReader
	logger *slog.Logger
}

// NewStorefront creates a Storefront.
func NewStorefront(reader StorefrontReader, logger *slog.Logger) *Storefront {
	return &Storefront{reader: reader, logger: logger}
}

// Config returns the configuration of the tenant in the context.
func (s *Storefront) Config(ctx context.Context) (StorefrontConfig, error) {
	stored, err := s.reader.LoadStorefront(ctx)
	if err != nil {
		return StorefrontConfig{}, fmt.Errorf("load storefront: %w", err)
	}

	// Stored themes are validated again, so a bad row can never reach a page.
	theme, err := domain.ParseTheme(stored.ThemeTokens)
	if err != nil {
		s.logger.ErrorContext(ctx, "tenants.theme.invalid", "theme_version", stored.ThemeVersion, "error", err)
		theme = domain.DefaultTheme()
	}

	features := make(map[string]bool, len(stored.Features))
	for _, feature := range stored.Features {
		features[feature.Key] = feature.Enabled()
	}

	return StorefrontConfig{
		Slug:         stored.Slug,
		Settings:     stored.Settings,
		ThemeVersion: stored.ThemeVersion,
		Theme:        theme,
		Features:     features,
	}, nil
}
