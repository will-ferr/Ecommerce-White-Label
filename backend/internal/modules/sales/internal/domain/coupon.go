package domain

import (
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/shared/apperr"
)

// Coupon kinds.
const (
	CouponPercent      = "percent"
	CouponFixed        = "fixed"
	CouponFreeShipping = "free_shipping"
)

var couponCodePattern = regexp.MustCompile(`^[A-Z0-9_-]{3,30}$`)

// Coupon is a discount the buyer unlocks with a code.
type Coupon struct {
	Code             string     `json:"code"`
	Description      string     `json:"description"`
	Kind             string     `json:"kind"`
	Value            int64      `json:"value"`
	MinSubtotal      int64      `json:"minSubtotal"`
	StartsAt         *time.Time `json:"startsAt"`
	EndsAt           *time.Time `json:"endsAt"`
	UsageLimit       *int32     `json:"usageLimit"`
	PerCustomerLimit *int32     `json:"perCustomerLimit"`
	Active           bool       `json:"active"`
}

// NormalizeCouponCode makes codes case-insensitive for buyers.
func NormalizeCouponCode(raw string) string {
	return strings.ToUpper(strings.TrimSpace(raw))
}

// Validate reports every invalid field of a coupon being saved.
func (c Coupon) Validate() error {
	var fields []apperr.FieldError
	add := func(field, message string) {
		fields = append(fields, apperr.FieldError{Field: field, Code: "invalid", Message: message})
	}
	if !couponCodePattern.MatchString(c.Code) {
		add("code", "Use 3 to 30 letters, numbers, hyphens or underscores.")
	}
	if utf8.RuneCountInString(c.Description) > 160 {
		add("description", "Use up to 160 characters.")
	}
	switch c.Kind {
	case CouponPercent:
		if c.Value < 1 || c.Value > 100 {
			add("value", "Use a percentage between 1 and 100.")
		}
	case CouponFixed:
		if c.Value < 1 || c.Value > MaxOrderAmount {
			add("value", "Enter a valid amount.")
		}
	case CouponFreeShipping:
		if c.Value != 0 {
			add("value", "Free shipping has no value.")
		}
	default:
		add("kind", "Choose a valid kind.")
	}
	if c.MinSubtotal < 0 || c.MinSubtotal > MaxOrderAmount {
		add("minSubtotal", "Enter a valid amount.")
	}
	if c.StartsAt != nil && c.EndsAt != nil && !c.StartsAt.Before(*c.EndsAt) {
		add("endsAt", "The end must be after the start.")
	}
	if c.UsageLimit != nil && *c.UsageLimit < 1 {
		add("usageLimit", "Use a positive limit or leave it empty.")
	}
	if c.PerCustomerLimit != nil && *c.PerCustomerLimit < 1 {
		add("perCustomerLimit", "Use a positive limit or leave it empty.")
	}
	if len(fields) > 0 {
		return apperr.Validation(fields...)
	}
	return nil
}

func couponError(code, message string) error {
	return apperr.New(apperr.KindUnprocessable, code, message)
}

// Check reports why a coupon cannot be used now for a subtotal. Usage
// counts are checked by the caller, which holds the coupon locked.
func (c Coupon) Check(now time.Time, subtotal int64) error {
	switch {
	case !c.Active:
		return couponError("COUPON_NOT_FOUND", "This coupon does not exist.")
	case c.StartsAt != nil && now.Before(*c.StartsAt):
		return couponError("COUPON_NOT_STARTED", "This coupon is not valid yet.")
	case c.EndsAt != nil && !now.Before(*c.EndsAt):
		return couponError("COUPON_EXPIRED", "This coupon has expired.")
	case subtotal < c.MinSubtotal:
		return couponError("COUPON_MIN_SUBTOTAL", "The order does not reach the minimum amount of this coupon.")
	}
	return nil
}

// ErrCouponExhausted and ErrCouponAlreadyUsed report usage limits.
var (
	ErrCouponExhausted   = couponError("COUPON_EXHAUSTED", "This coupon has reached its usage limit.")
	ErrCouponAlreadyUsed = couponError("COUPON_ALREADY_USED", "You have already used this coupon.")
)

// Discount computes the discount for an order. It never exceeds what it
// applies to, so the total never becomes negative.
func (c Coupon) Discount(subtotal, shipping int64) int64 {
	switch c.Kind {
	case CouponPercent:
		return subtotal * c.Value / 100
	case CouponFixed:
		return min(c.Value, subtotal)
	case CouponFreeShipping:
		return shipping
	}
	return 0
}
