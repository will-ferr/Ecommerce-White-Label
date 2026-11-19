package domain

import (
	"regexp"
	"slices"
	"strconv"
)

var postalCodePattern = regexp.MustCompile(`^[0-9]{8}$`)

// MaxRates bounds the size of a shipping table.
const MaxRates = 500

// Rate is one line of a shipping table: the price for parcels up to a weight
// sent to a range of postal codes.
type Rate struct {
	PostalCodeStart string `json:"postalCodeStart"`
	PostalCodeEnd   string `json:"postalCodeEnd"`
	MaxWeightGrams  int32  `json:"maxWeightGrams"`
	Price           int64  `json:"price"`
	MinDays         int32  `json:"minDays"`
	MaxDays         int32  `json:"maxDays"`
}

// RateProblem describes an invalid line of a shipping table.
type RateProblem struct {
	Line    int
	Message string
}

// ValidateRates checks each line of a table and returns the problems found.
func ValidateRates(rates []Rate) []RateProblem {
	var problems []RateProblem
	if len(rates) == 0 {
		return []RateProblem{{Line: 0, Message: "Add at least one rate."}}
	}
	if len(rates) > MaxRates {
		return []RateProblem{{Line: 0, Message: "Use up to " + strconv.Itoa(MaxRates) + " rates."}}
	}
	for i, rate := range rates {
		switch {
		case !postalCodePattern.MatchString(rate.PostalCodeStart) || !postalCodePattern.MatchString(rate.PostalCodeEnd):
			problems = append(problems, RateProblem{Line: i + 1, Message: "Postal codes must have 8 digits."})
		case rate.PostalCodeStart > rate.PostalCodeEnd:
			problems = append(problems, RateProblem{Line: i + 1, Message: "The first postal code must not exceed the last one."})
		case rate.MaxWeightGrams <= 0 || rate.MaxWeightGrams > 1_000_000:
			problems = append(problems, RateProblem{Line: i + 1, Message: "Enter a maximum weight between 1 g and 1000 kg."})
		case rate.Price < 0 || rate.Price > 1_000_000_00:
			problems = append(problems, RateProblem{Line: i + 1, Message: "Enter a valid price."})
		case rate.MinDays < 0 || rate.MaxDays < rate.MinDays || rate.MaxDays > 120:
			problems = append(problems, RateProblem{Line: i + 1, Message: "Enter a valid delivery time."})
		}
	}
	return problems
}

// Quote finds the rate for a parcel: among the lines that cover the postal
// code and the weight, the one with the smallest weight limit, then the
// cheapest. ok is false when no line covers the parcel.
func Quote(rates []Rate, postalCode string, weightGrams int32) (Rate, bool) {
	if !postalCodePattern.MatchString(postalCode) {
		return Rate{}, false
	}
	candidates := slices.DeleteFunc(slices.Clone(rates), func(rate Rate) bool {
		return postalCode < rate.PostalCodeStart || postalCode > rate.PostalCodeEnd || weightGrams > rate.MaxWeightGrams
	})
	if len(candidates) == 0 {
		return Rate{}, false
	}
	return slices.MinFunc(candidates, func(a, b Rate) int {
		if a.MaxWeightGrams != b.MaxWeightGrams {
			return int(a.MaxWeightGrams - b.MaxWeightGrams)
		}
		return int(a.Price - b.Price)
	}), true
}
