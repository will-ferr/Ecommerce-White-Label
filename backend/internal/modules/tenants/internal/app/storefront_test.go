package app

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/modules/tenants/internal/domain"
)

type fakeReader struct{ stored StoredStorefront }

func (f fakeReader) LoadStorefront(context.Context) (StoredStorefront, error) { return f.stored, nil }

func TestConfigAppliesFeatureRule(t *testing.T) {
	tokens, err := json.Marshal(domain.DefaultTheme())
	if err != nil {
		t.Fatal(err)
	}
	storefront := NewStorefront(fakeReader{StoredStorefront{
		ThemeTokens: tokens,
		Features: []domain.FeatureState{
			{Key: "reviews", GloballyEnabled: true, InPlan: true, TenantEnabled: true},
			{Key: "loyalty", GloballyEnabled: true, InPlan: false, TenantEnabled: true},
			{Key: "wishlist", GloballyEnabled: false, InPlan: true, TenantEnabled: true},
		},
	}}, slog.Default())

	config, err := storefront.Config(context.Background())

	if err != nil {
		t.Fatal(err)
	}
	if !config.Features["reviews"] || config.Features["loyalty"] || config.Features["wishlist"] {
		t.Errorf("features = %v", config.Features)
	}
}

func TestConfigReplacesCorruptedStoredTheme(t *testing.T) {
	var logs bytes.Buffer
	storefront := NewStorefront(fakeReader{StoredStorefront{
		ThemeVersion: 7,
		ThemeTokens:  json.RawMessage(`{"colors":{"primary":"red;}</style><script>alert(1)</script>"}}`),
	}}, slog.New(slog.NewJSONHandler(&logs, nil)))

	config, err := storefront.Config(context.Background())

	if err != nil {
		t.Fatal(err)
	}
	if config.Theme != domain.DefaultTheme() {
		t.Errorf("unsafe theme reached the response: %+v", config.Theme)
	}
	if !bytes.Contains(logs.Bytes(), []byte("tenants.theme.invalid")) {
		t.Errorf("the invalid theme must be logged: %s", logs.String())
	}
}
