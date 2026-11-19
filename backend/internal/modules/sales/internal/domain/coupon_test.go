package domain

import (
	"errors"
	"testing"
	"time"

	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/shared/apperr"
)

func TestCouponDiscount(t *testing.T) {
	cases := []struct {
		name     string
		coupon   Coupon
		subtotal int64
		shipping int64
		want     int64
	}{
		{"percent rounds down", Coupon{Kind: CouponPercent, Value: 15}, 9990, 1500, 1498},
		{"fixed amount", Coupon{Kind: CouponFixed, Value: 2000}, 9990, 1500, 2000},
		{"fixed never exceeds the subtotal", Coupon{Kind: CouponFixed, Value: 50000}, 9990, 1500, 9990},
		{"free shipping", Coupon{Kind: CouponFreeShipping}, 9990, 1500, 1500},
		{"free pickup", Coupon{Kind: CouponFreeShipping}, 9990, 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.coupon.Discount(tc.subtotal, tc.shipping); got != tc.want {
				t.Errorf("Discount() = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestCouponCheck(t *testing.T) {
	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	before, after := now.Add(-time.Hour), now.Add(time.Hour)
	cases := []struct {
		name   string
		coupon Coupon
		code   string
	}{
		{"valid", Coupon{Active: true, MinSubtotal: 5000}, ""},
		{"inactive looks unknown", Coupon{Active: false}, "COUPON_NOT_FOUND"},
		{"not started", Coupon{Active: true, StartsAt: &after}, "COUPON_NOT_STARTED"},
		{"ended", Coupon{Active: true, EndsAt: &before}, "COUPON_EXPIRED"},
		{"ends exactly now", Coupon{Active: true, EndsAt: &now}, "COUPON_EXPIRED"},
		{"below minimum", Coupon{Active: true, MinSubtotal: 10000}, "COUPON_MIN_SUBTOTAL"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.coupon.Check(now, 9990)
			if tc.code == "" {
				if err != nil {
					t.Errorf("Check() = %v", err)
				}
				return
			}
			var appErr *apperr.Error
			if !errors.As(err, &appErr) || appErr.Code != tc.code {
				t.Errorf("Check() = %v, want %s", err, tc.code)
			}
		})
	}
}

func TestCouponValidate(t *testing.T) {
	valid := Coupon{Code: "BEMVINDO10", Kind: CouponPercent, Value: 10}
	if err := valid.Validate(); err != nil {
		t.Fatalf("Validate() = %v", err)
	}
	start, end := time.Now(), time.Now().Add(-time.Hour)
	zero := int32(0)
	for name, coupon := range map[string]Coupon{
		"lowercase code":      {Code: "bemvindo", Kind: CouponPercent, Value: 10},
		"percent above 100":   {Code: "DEMAIS", Kind: CouponPercent, Value: 150},
		"free shipping value": {Code: "FRETE", Kind: CouponFreeShipping, Value: 10},
		"unknown kind":        {Code: "BRINDE", Kind: "gift", Value: 1},
		"end before start":    {Code: "DATAS", Kind: CouponFixed, Value: 100, StartsAt: &start, EndsAt: &end},
		"zero usage limit":    {Code: "LIMITE", Kind: CouponFixed, Value: 100, UsageLimit: &zero},
	} {
		if err := coupon.Validate(); err == nil {
			t.Errorf("%s: invalid coupon accepted", name)
		}
	}
	if NormalizeCouponCode("  bemvindo10 ") != "BEMVINDO10" {
		t.Error("codes are not case-insensitive")
	}
}
