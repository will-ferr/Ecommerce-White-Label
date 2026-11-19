package sales

import (
	"context"
	"fmt"
	"strconv"

	"github.com/google/uuid"

	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/modules/sales/internal/domain"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/modules/sales/internal/sqlcgen"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/shared/apperr"
)

// Shipping method kinds.
const (
	shippingPickup = "pickup"
	shippingFlat   = "flat"
	shippingTable  = "table"
)

// Rate is one line of a shipping table.
type Rate = domain.Rate

func toRate(row sqlcgen.ShippingRate) Rate {
	return Rate{
		PostalCodeStart: row.PostalCodeStart, PostalCodeEnd: row.PostalCodeEnd, MaxWeightGrams: row.MaxWeightGrams,
		Price: row.PriceAmount, MinDays: row.MinDays, MaxDays: row.MaxDays,
	}
}

func loadRates(ctx context.Context, q *sqlcgen.Queries, tenantID uuid.UUID, methods []sqlcgen.ShippingMethod) (map[uuid.UUID][]Rate, error) {
	ids := []uuid.UUID{}
	for _, method := range methods {
		if method.Kind == shippingTable {
			ids = append(ids, method.ID)
		}
	}
	rates := map[uuid.UUID][]Rate{}
	if len(ids) == 0 {
		return rates, nil
	}
	rows, err := q.ListShippingRates(ctx, sqlcgen.ListShippingRatesParams{TenantID: tenantID, MethodIds: ids})
	if err != nil {
		return nil, fmt.Errorf("list shipping rates: %w", err)
	}
	for _, row := range rows {
		rates[row.MethodID] = append(rates[row.MethodID], toRate(row))
	}
	return rates, nil
}

// shippingOptions lists the delivery choices for a parcel. Pickup and flat
// options are always offered. Table options appear once the postal code is
// known, and only if a rate covers the destination and the weight.
func (s *Service) shippingOptions(ctx context.Context, q *sqlcgen.Queries, tenantID uuid.UUID, postalCode string, weightGrams int32) ([]ShippingOption, error) {
	active := true
	methods, err := q.ListShippingMethods(ctx, sqlcgen.ListShippingMethodsParams{TenantID: tenantID, Active: &active})
	if err != nil {
		return nil, fmt.Errorf("list shipping methods: %w", err)
	}
	rates, err := loadRates(ctx, q, tenantID, methods)
	if err != nil {
		return nil, err
	}
	options := []ShippingOption{}
	for _, method := range methods {
		option := toOption(method)
		if method.Kind == shippingTable {
			rate, ok := domain.Quote(rates[method.ID], postalCode, weightGrams)
			if !ok {
				continue
			}
			option.Price, option.MinDays, option.MaxDays = rate.Price, rate.MinDays, rate.MaxDays
		}
		options = append(options, option)
	}
	return options, nil
}

func validateRates(kind string, rates []Rate) []apperr.FieldError {
	if kind != shippingTable {
		if len(rates) > 0 {
			return []apperr.FieldError{{Field: "rates", Code: "invalid", Message: "Only shipping tables have rates."}}
		}
		return nil
	}
	var fields []apperr.FieldError
	for _, problem := range domain.ValidateRates(rates) {
		field := "rates"
		if problem.Line > 0 {
			field = "rates." + strconv.Itoa(problem.Line-1)
		}
		fields = append(fields, apperr.FieldError{Field: field, Code: "invalid", Message: problem.Message})
	}
	return fields
}

func saveRates(ctx context.Context, q *sqlcgen.Queries, tenantID, methodID uuid.UUID, rates []Rate) error {
	if err := q.DeleteShippingRates(ctx, sqlcgen.DeleteShippingRatesParams{TenantID: tenantID, MethodID: methodID}); err != nil {
		return fmt.Errorf("replace shipping rates: %w", err)
	}
	for _, rate := range rates {
		if err := q.InsertShippingRate(ctx, sqlcgen.InsertShippingRateParams{
			TenantID: tenantID, ID: newID(), MethodID: methodID, PostalCodeStart: rate.PostalCodeStart, PostalCodeEnd: rate.PostalCodeEnd,
			MaxWeightGrams: rate.MaxWeightGrams, PriceAmount: rate.Price, MinDays: rate.MinDays, MaxDays: rate.MaxDays,
		}); err != nil {
			return fmt.Errorf("insert shipping rate: %w", err)
		}
	}
	return nil
}
