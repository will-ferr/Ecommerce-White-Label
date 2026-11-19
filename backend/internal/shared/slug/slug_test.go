package slug

import "testing"

func TestMake(t *testing.T) {
	for in, want := range map[string]string{
		"Café Especial":         "cafe-especial",
		"  Política de Trocas!": "politica-de-trocas",
		"Sobre nós -- 2026":     "sobre-nos-2026",
		"***":                   "",
	} {
		if got := Make(in, 120); got != want {
			t.Errorf("Make(%q) = %q, want %q", in, got, want)
		}
	}
	if got := Make("aaaa-bbbb", 5); got != "aaaa" {
		t.Errorf("Make() with limit = %q", got)
	}
	if !Valid("sobre-nos", 80) || Valid("Sobre", 80) || Valid("a--b", 80) {
		t.Error("Valid() accepted or refused the wrong segments")
	}
}
