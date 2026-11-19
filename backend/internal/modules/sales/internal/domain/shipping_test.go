package domain

import "testing"

func TestQuote(t *testing.T) {
	rates := []Rate{
		{PostalCodeStart: "01000000", PostalCodeEnd: "19999999", MaxWeightGrams: 1000, Price: 1500, MinDays: 2, MaxDays: 4},
		{PostalCodeStart: "01000000", PostalCodeEnd: "19999999", MaxWeightGrams: 5000, Price: 2500, MinDays: 2, MaxDays: 4},
		{PostalCodeStart: "20000000", PostalCodeEnd: "99999999", MaxWeightGrams: 5000, Price: 4500, MinDays: 5, MaxDays: 10},
	}
	cases := []struct {
		name       string
		postalCode string
		weight     int32
		price      int64
		found      bool
	}{
		{"light parcel uses the smallest bracket", "01310100", 800, 1500, true},
		{"limit is inclusive", "01310100", 1000, 1500, true},
		{"heavier parcel uses the next bracket", "01310100", 1001, 2500, true},
		{"other region", "69900000", 300, 4500, true},
		{"too heavy", "01310100", 5001, 0, false},
		{"invalid postal code", "0131010", 100, 0, false},
		{"parcel without weight", "20040002", 0, 4500, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rate, found := Quote(rates, tc.postalCode, tc.weight)
			if found != tc.found || rate.Price != tc.price {
				t.Errorf("Quote() = %+v, %v; want price %d, %v", rate, found, tc.price, tc.found)
			}
		})
	}
}

func TestValidateRates(t *testing.T) {
	valid := Rate{PostalCodeStart: "01000000", PostalCodeEnd: "19999999", MaxWeightGrams: 1000, Price: 1500, MinDays: 1, MaxDays: 3}
	if problems := ValidateRates([]Rate{valid}); len(problems) != 0 {
		t.Fatalf("valid table rejected: %v", problems)
	}
	reversed := valid
	reversed.PostalCodeStart, reversed.PostalCodeEnd = valid.PostalCodeEnd, valid.PostalCodeStart
	noWeight := valid
	noWeight.MaxWeightGrams = 0
	slow := valid
	slow.MinDays = 5
	problems := ValidateRates([]Rate{valid, reversed, noWeight, slow})
	if len(problems) != 3 || problems[0].Line != 2 || problems[1].Line != 3 || problems[2].Line != 4 {
		t.Errorf("problems = %+v", problems)
	}
	if problems := ValidateRates(nil); len(problems) != 1 {
		t.Errorf("an empty table was accepted")
	}
}
