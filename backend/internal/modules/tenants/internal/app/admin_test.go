package app

import (
	"context"
	"errors"
	"testing"

	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/modules/tenants/internal/domain"
)

type fakeAdminStore struct {
	AdminStore
	domain   DomainRecord
	verified []string
	created  []NewTenant
	token    string
	themes   int
}

func (f *fakeAdminStore) Domain(context.Context, string) (DomainRecord, error) { return f.domain, nil }

func (f *fakeAdminStore) MarkDomainVerified(_ context.Context, host string) error {
	f.verified = append(f.verified, host)
	return nil
}

func (f *fakeAdminStore) CreateTenant(_ context.Context, tenant NewTenant) error {
	f.created = append(f.created, tenant)
	return nil
}

func (f *fakeAdminStore) AddCustomDomain(_ context.Context, _, _, token string) error {
	f.token = token
	return nil
}

func (f *fakeAdminStore) PublishTheme(context.Context, string, domain.Theme) (int32, error) {
	f.themes++
	return 2, nil
}

type fakeDNS struct {
	values []string
	err    error
	asked  string
}

func (f *fakeDNS) LookupTXT(_ context.Context, name string) ([]string, error) {
	f.asked = name
	return f.values, f.err
}

func TestVerifyDomain(t *testing.T) {
	const token = "expected-token"
	tests := map[string]struct {
		dns          fakeDNS
		wantVerified bool
		wantCode     string
	}{
		"matching record":    {dns: fakeDNS{values: []string{"other", " " + token + " "}}, wantVerified: true},
		"different record":   {dns: fakeDNS{values: []string{"attacker-token"}}, wantCode: "DOMAIN_NOT_VERIFIED"},
		"no records":         {dns: fakeDNS{}, wantCode: "DOMAIN_NOT_VERIFIED"},
		"empty record":       {dns: fakeDNS{values: []string{""}}, wantCode: "DOMAIN_NOT_VERIFIED"},
		"dns lookup failure": {dns: fakeDNS{err: errors.New("no such host")}, wantCode: "DOMAIN_NOT_VERIFIED"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			store := &fakeAdminStore{domain: DomainRecord{Host: "loja.com.br", VerificationToken: token}}
			admin := NewAdmin(store, &tt.dns, "plataforma.com")

			err := admin.VerifyDomain(context.Background(), "LOJA.com.br")

			if tt.dns.asked != "_ecommerce-verify.loja.com.br" {
				t.Errorf("queried %q", tt.dns.asked)
			}
			if tt.wantVerified {
				if err != nil || len(store.verified) != 1 {
					t.Fatalf("err=%v verified=%v", err, store.verified)
				}
				return
			}
			wantCode(t, err, tt.wantCode)
			if len(store.verified) != 0 {
				t.Error("domain must stay unverified")
			}
		})
	}
}

func TestAddDomainRefusesPlatformSubdomains(t *testing.T) {
	store := &fakeAdminStore{}
	admin := NewAdmin(store, &fakeDNS{}, "plataforma.com")

	if _, _, err := admin.AddDomain(context.Background(), "aurora", "outra-loja.plataforma.com"); err == nil {
		t.Fatal("a platform subdomain was accepted")
	}
	if store.token != "" {
		t.Error("nothing should be stored")
	}
}

func TestAddDomainGeneratesUniqueTokens(t *testing.T) {
	store := &fakeAdminStore{}
	admin := NewAdmin(store, &fakeDNS{}, "plataforma.com")

	record, first, err := admin.AddDomain(context.Background(), "aurora", "loja.com.br")
	if err != nil {
		t.Fatal(err)
	}
	_, second, err := admin.AddDomain(context.Background(), "aurora", "loja2.com.br")
	if err != nil {
		t.Fatal(err)
	}
	if record != "_ecommerce-verify.loja.com.br" || len(first) != 64 || first == second {
		t.Errorf("record=%q first=%q second=%q", record, first, second)
	}
}

func TestCreateTenantValidatesBeforeStoring(t *testing.T) {
	store := &fakeAdminStore{}
	admin := NewAdmin(store, &fakeDNS{}, "plataforma.com")

	for _, tt := range []struct{ slug, name string }{{"admin", "Loja"}, {"Loja Invalida", "Loja"}, {"aurora", ""}} {
		if _, err := admin.CreateTenant(context.Background(), tt.slug, tt.name, "starter"); err == nil {
			t.Errorf("CreateTenant(%q, %q) accepted invalid data", tt.slug, tt.name)
		}
	}
	if len(store.created) != 0 {
		t.Fatalf("invalid tenants were stored: %+v", store.created)
	}

	host, err := admin.CreateTenant(context.Background(), "aurora", "Aurora", "starter")
	if err != nil || host != "aurora.plataforma.com" {
		t.Fatalf("host=%q err=%v", host, err)
	}
}

func TestPublishThemeRefusesInvalidDocumentBeforeStoring(t *testing.T) {
	store := &fakeAdminStore{}
	admin := NewAdmin(store, &fakeDNS{}, "plataforma.com")

	if _, err := admin.PublishTheme(context.Background(), "aurora", []byte(`{"customCss":"body{}"}`)); err == nil {
		t.Fatal("an invalid theme was accepted")
	}
	if store.themes != 0 {
		t.Error("an invalid theme reached the store")
	}
}

func TestSetStatusRefusesUnknownStatus(t *testing.T) {
	admin := NewAdmin(&fakeAdminStore{}, &fakeDNS{}, "plataforma.com")

	wantCode(t, admin.SetStatus(context.Background(), "aurora", "deleted"), "INVALID_STATUS")
}
