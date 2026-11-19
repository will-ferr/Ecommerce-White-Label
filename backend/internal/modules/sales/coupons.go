package sales

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/modules/sales/internal/domain"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/modules/sales/internal/sqlcgen"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/authz"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/shared/apperr"
)

// CouponInput is a coupon as the store team writes it.
type CouponInput = domain.Coupon

// CouponView is a coupon with its usage.
type CouponView struct {
	ID uuid.UUID `json:"id"`
	domain.Coupon
	UsedCount int32     `json:"usedCount"`
	CreatedAt time.Time `json:"createdAt"`
}

// AppliedCoupon is the coupon accepted for an order.
type AppliedCoupon struct {
	Code        string `json:"code"`
	Description string `json:"description"`
}

func toCoupon(row sqlcgen.PromotionsCoupon) domain.Coupon {
	return domain.Coupon{
		Code: row.Code, Description: row.Description, Kind: row.Kind, Value: row.Value, MinSubtotal: row.MinSubtotal,
		StartsAt: row.StartsAt, EndsAt: row.EndsAt, UsageLimit: row.UsageLimit, PerCustomerLimit: row.PerCustomerLimit, Active: row.Active,
	}
}

func couponNotFound() error {
	return apperr.New(apperr.KindUnprocessable, "COUPON_NOT_FOUND", "This coupon does not exist.")
}

// Coupons lists the coupons of the store.
func (s *Service) Coupons(ctx context.Context) ([]CouponView, error) {
	views := []CouponView{}
	err := s.in(ctx, func(_ pgx.Tx, q *sqlcgen.Queries, tenantID uuid.UUID) error {
		rows, err := q.ListCoupons(ctx, tenantID)
		if err != nil {
			return fmt.Errorf("list coupons: %w", err)
		}
		for _, row := range rows {
			views = append(views, CouponView{ID: row.ID, Coupon: toCoupon(row), UsedCount: row.UsedCount, CreatedAt: row.CreatedAt})
		}
		return nil
	})
	return views, err
}

// SaveCoupon creates a coupon, or changes it when id is given. The code of an
// existing coupon never changes, so redemptions keep their meaning.
func (s *Service) SaveCoupon(ctx context.Context, principal authz.Principal, id *uuid.UUID, input CouponInput) (uuid.UUID, error) {
	input.Code = domain.NormalizeCouponCode(input.Code)
	input.Description = strings.TrimSpace(input.Description)
	if err := input.Validate(); err != nil {
		return uuid.Nil, err
	}
	couponID := newID()
	err := s.in(ctx, func(tx pgx.Tx, q *sqlcgen.Queries, tenantID uuid.UUID) error {
		var err error
		if id == nil {
			err = q.InsertCoupon(ctx, sqlcgen.InsertCouponParams{
				TenantID: tenantID, ID: couponID, Code: input.Code, Description: input.Description, Kind: input.Kind, Value: input.Value,
				MinSubtotal: input.MinSubtotal, StartsAt: input.StartsAt, EndsAt: input.EndsAt, UsageLimit: input.UsageLimit,
				PerCustomerLimit: input.PerCustomerLimit, Active: input.Active,
			})
			if isUniqueViolation(err) {
				return apperr.Validation(apperr.FieldError{Field: "code", Code: "taken", Message: "This code is already in use."})
			}
		} else {
			couponID = *id
			var affected int64
			affected, err = q.UpdateCoupon(ctx, sqlcgen.UpdateCouponParams{
				TenantID: tenantID, ID: couponID, Description: input.Description, Kind: input.Kind, Value: input.Value,
				MinSubtotal: input.MinSubtotal, StartsAt: input.StartsAt, EndsAt: input.EndsAt, UsageLimit: input.UsageLimit,
				PerCustomerLimit: input.PerCustomerLimit, Active: input.Active,
			})
			if err == nil && affected == 0 {
				return apperr.New(apperr.KindNotFound, "COUPON_NOT_FOUND", "The coupon does not exist.")
			}
		}
		if err != nil {
			return err
		}
		return userAudit(ctx, tx, principal, "promotions.coupon.saved", "coupon", input.Code, map[string]string{"kind": input.Kind})
	})
	return couponID, err
}

// previewCoupon checks a coupon for the summary. A problem is reported in the
// summary instead of failing it, so the buyer can fix the code.
func (s *Service) previewCoupon(ctx context.Context, q *sqlcgen.Queries, tenantID uuid.UUID, code string, summary *Summary) error {
	row, err := q.GetCouponByCode(ctx, sqlcgen.GetCouponByCodeParams{TenantID: tenantID, Code: code})
	if errors.Is(err, pgx.ErrNoRows) {
		summary.CouponError = "COUPON_NOT_FOUND"
		return nil
	}
	if err != nil {
		return fmt.Errorf("load coupon: %w", err)
	}
	coupon := toCoupon(row)
	if err := coupon.Check(s.now(), summary.Subtotal); err != nil {
		summary.CouponError = codeOf(err)
		return nil
	}
	if row.UsageLimit != nil && row.UsedCount >= *row.UsageLimit {
		summary.CouponError = codeOf(domain.ErrCouponExhausted)
		return nil
	}
	summary.Discount = coupon.Discount(summary.Subtotal, summary.ShippingAmount)
	summary.Total -= summary.Discount
	summary.Coupon = &AppliedCoupon{Code: coupon.Code, Description: coupon.Description}
	return nil
}

func couponCode(summary Summary) string {
	if summary.Coupon == nil {
		return ""
	}
	return summary.Coupon.Code
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func codeOf(err error) string {
	if appErr, ok := apperr.As(err); ok {
		return appErr.Code
	}
	return "COUPON_NOT_FOUND"
}

// redeemCoupon locks the coupon, checks every limit and records its use by
// the order. The lock makes the last available use go to one buyer only.
func (s *Service) redeemCoupon(ctx context.Context, q *sqlcgen.Queries, tenantID uuid.UUID, code, email string, summary Summary) (sqlcgen.PromotionsCoupon, int64, error) {
	row, err := q.LockCouponByCode(ctx, sqlcgen.LockCouponByCodeParams{TenantID: tenantID, Code: code})
	if errors.Is(err, pgx.ErrNoRows) {
		return row, 0, couponNotFound()
	}
	if err != nil {
		return row, 0, fmt.Errorf("lock coupon: %w", err)
	}
	coupon := toCoupon(row)
	if err := coupon.Check(s.now(), summary.Subtotal); err != nil {
		return row, 0, err
	}
	if row.UsageLimit != nil && row.UsedCount >= *row.UsageLimit {
		return row, 0, domain.ErrCouponExhausted
	}
	if row.PerCustomerLimit != nil {
		used, err := q.CountCustomerRedemptions(ctx, sqlcgen.CountCustomerRedemptionsParams{TenantID: tenantID, CouponID: row.ID, CustomerEmail: email})
		if err != nil {
			return row, 0, fmt.Errorf("count redemptions: %w", err)
		}
		if used >= *row.PerCustomerLimit {
			return row, 0, domain.ErrCouponAlreadyUsed
		}
	}
	return row, coupon.Discount(summary.Subtotal, summary.ShippingAmount), nil
}

func recordRedemption(ctx context.Context, q *sqlcgen.Queries, coupon sqlcgen.PromotionsCoupon, orderID uuid.UUID, email string, discount int64) error {
	if err := q.InsertRedemption(ctx, sqlcgen.InsertRedemptionParams{
		TenantID: coupon.TenantID, ID: newID(), CouponID: coupon.ID, OrderID: orderID, CustomerEmail: email, DiscountAmount: discount,
	}); err != nil {
		return fmt.Errorf("record coupon use: %w", err)
	}
	return q.AddCouponUse(ctx, sqlcgen.AddCouponUseParams{TenantID: coupon.TenantID, ID: coupon.ID, Delta: 1})
}

// releaseCoupon gives back the use of a coupon when its order is canceled.
func releaseCoupon(ctx context.Context, q *sqlcgen.Queries, tenantID, orderID uuid.UUID) error {
	couponID, err := q.ReleaseRedemption(ctx, sqlcgen.ReleaseRedemptionParams{TenantID: tenantID, OrderID: orderID})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("release coupon: %w", err)
	}
	return q.AddCouponUse(ctx, sqlcgen.AddCouponUseParams{TenantID: tenantID, ID: couponID, Delta: -1})
}
