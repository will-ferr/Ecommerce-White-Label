package domain

import (
	"strings"
	"testing"
)

func price(v int64) *int64 { return &v }

func validProduct() ProductInput {
	return ProductInput{
		Name: "Café Especial", Status: StatusActive,
		Variants: []VariantInput{
			{SKU: "CAFE-250", Price: 4990, Options: map[string]string{"Peso": "250 g"}},
			{SKU: "CAFE-500", Price: 8990, CompareAt: price(9990), Options: map[string]string{"Peso": "500 g"}},
		},
	}
}

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"Café Especial":          "cafe-especial",
		"  Camiseta   Básica!! ": "camiseta-basica",
		"Ação & Reação 2024":     "acao-reacao-2024",
		"---":                    "",
	}
	for input, want := range cases {
		if got := Slugify(input); got != want {
			t.Errorf("Slugify(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestNormalizeAcceptsValidProduct(t *testing.T) {
	got, err := validProduct().Normalize()
	if err != nil {
		t.Fatal(err)
	}
	if got.Slug != "cafe-especial" {
		t.Errorf("slug = %q", got.Slug)
	}
}

func TestNormalizeRefusesInvalidProducts(t *testing.T) {
	tests := map[string]func(*ProductInput){
		"no variants":           func(p *ProductInput) { p.Variants = nil },
		"duplicate sku":         func(p *ProductInput) { p.Variants[1].SKU = "cafe-250" },
		"negative price":        func(p *ProductInput) { p.Variants[0].Price = -1 },
		"absurd price":          func(p *ProductInput) { p.Variants[0].Price = MaxPriceAmount + 1 },
		"compare below price":   func(p *ProductInput) { p.Variants[1].CompareAt = price(100) },
		"inconsistent options":  func(p *ProductInput) { p.Variants[1].Options = map[string]string{"Cor": "Azul"} },
		"duplicate combination": func(p *ProductInput) { p.Variants[1].Options = map[string]string{"Peso": "250 g"} },
		"bad gtin":              func(p *ProductInput) { p.Variants[0].GTIN = "12AB" },
		"bad sku":               func(p *ProductInput) { p.Variants[0].SKU = "sku com espaço" },
		"unknown status":        func(p *ProductInput) { p.Status = "published" },
		"http image":            func(p *ProductInput) { p.Images = []Image{{URL: "http://cdn.test/a.jpg"}} },
		"script image":          func(p *ProductInput) { p.Images = []Image{{URL: "javascript:alert(1)"}} },
		"control chars in name": func(p *ProductInput) { p.Name = "Café\x00" },
		"long description":      func(p *ProductInput) { p.Description = strings.Repeat("a", 5001) },
		"bad slug":              func(p *ProductInput) { p.Slug = "Café Especial" },
		"too many options":      func(p *ProductInput) { tooManyOptions(p) },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			product := validProduct()
			mutate(&product)
			if _, err := product.Normalize(); err == nil {
				t.Error("an invalid product was accepted")
			}
		})
	}
}

func tooManyOptions(p *ProductInput) {
	options := map[string]string{"A": "1", "B": "1", "C": "1", "D": "1"}
	p.Variants = []VariantInput{{SKU: "X", Price: 1, Options: options}}
}

func TestVariantLabelIsStable(t *testing.T) {
	label := VariantLabel(map[string]string{"Tamanho": "M", "Cor": "Azul"})
	if label != "Azul / M" {
		t.Errorf("label = %q", label)
	}
}

func TestDescriptionKeepsLineBreaks(t *testing.T) {
	product := validProduct()
	product.Description = "Primeira linha\nSegunda linha"
	if _, err := product.Normalize(); err != nil {
		t.Errorf("line breaks must be allowed: %v", err)
	}
}
