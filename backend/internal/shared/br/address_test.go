package br

import "testing"

func valid() Address {
	return Address{
		Recipient: "  Maria   Silva ", PostalCode: "01310-100", Street: "Avenida Paulista", Number: "1000",
		District: "Bela Vista", City: "São Paulo", State: "sp",
	}
}

func TestNormalizeCleansAddress(t *testing.T) {
	got, err := valid().Normalize()
	if err != nil {
		t.Fatal(err)
	}
	if got.PostalCode != "01310100" || got.State != "SP" || got.Recipient != "Maria Silva" {
		t.Errorf("got %+v", got)
	}
}

func TestNormalizeRefusesInvalidFields(t *testing.T) {
	tests := map[string]func(*Address){
		"short cep":        func(a *Address) { a.PostalCode = "0131" },
		"letters in cep":   func(a *Address) { a.PostalCode = "0131A100" },
		"unknown state":    func(a *Address) { a.State = "XX" },
		"missing street":   func(a *Address) { a.Street = "" },
		"control chars":    func(a *Address) { a.City = "São\x00Paulo" },
		"long complement":  func(a *Address) { a.Complement = string(make([]byte, 81)) },
		"missing number":   func(a *Address) { a.Number = " " },
		"missing district": func(a *Address) { a.District = "" },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			address := valid()
			mutate(&address)
			if _, err := address.Normalize(); err == nil {
				t.Error("an invalid address was accepted")
			}
		})
	}
}
