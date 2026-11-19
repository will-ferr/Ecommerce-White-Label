package sales

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/modules/sales/internal/domain"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/modules/sales/internal/sqlcgen"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/modules/tenants"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/authz"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/httpx"
)

// DailySales is the paid revenue of one day.
type DailySales struct {
	Date    string `json:"date"`
	Orders  int32  `json:"orders"`
	Revenue int64  `json:"revenue"`
}

// ProductSales is the paid revenue of one product.
type ProductSales struct {
	Name     string `json:"name"`
	Quantity int32  `json:"quantity"`
	Revenue  int64  `json:"revenue"`
}

// SalesReport summarizes the sales of a period. Orders count once paid.
type SalesReport struct {
	From           string         `json:"from"`
	To             string         `json:"to"`
	Timezone       string         `json:"timezone"`
	Currency       string         `json:"currency"`
	PaidOrders     int32          `json:"paidOrders"`
	Revenue        int64          `json:"revenue"`
	AverageTicket  int64          `json:"averageTicket"`
	Discounts      int64          `json:"discounts"`
	CanceledOrders int32          `json:"canceledOrders"`
	AllOrders      int32          `json:"allOrders"`
	Days           []DailySales   `json:"days"`
	TopProducts    []ProductSales `json:"topProducts"`
}

// Report computes the sales report of the store for a period of days in its timezone.
func (s *Service) Report(ctx context.Context, from, to string) (SalesReport, error) {
	var report SalesReport
	err := s.in(ctx, func(tx pgx.Tx, q *sqlcgen.Queries, tenantID uuid.UUID) error {
		timezone, err := tenants.StoreTimezone(ctx, tx, tenantID)
		if err != nil {
			return err
		}
		location, err := time.LoadLocation(timezone)
		if err != nil {
			return fmt.Errorf("load timezone: %w", err)
		}
		period, err := domain.ParsePeriod(from, to, location, s.now())
		if err != nil {
			return err
		}
		currency, err := tenants.StoreCurrency(ctx, tx, tenantID)
		if err != nil {
			return err
		}
		totals, err := q.ReportTotals(ctx, sqlcgen.ReportTotalsParams{TenantID: tenantID, FromAt: period.Start, ToAt: period.End})
		if err != nil {
			return fmt.Errorf("report totals: %w", err)
		}
		daily, err := q.ReportDaily(ctx, sqlcgen.ReportDailyParams{TenantID: tenantID, Timezone: timezone, FromAt: period.Start, ToAt: period.End})
		if err != nil {
			return fmt.Errorf("report days: %w", err)
		}
		top, err := q.ReportTopProducts(ctx, sqlcgen.ReportTopProductsParams{TenantID: tenantID, FromAt: period.Start, ToAt: period.End})
		if err != nil {
			return fmt.Errorf("report products: %w", err)
		}

		report = SalesReport{
			From: period.From, To: period.To, Timezone: timezone, Currency: currency,
			PaidOrders: totals.PaidOrders, Revenue: totals.Revenue, Discounts: totals.Discounts,
			CanceledOrders: totals.CanceledOrders, AllOrders: totals.AllOrders, TopProducts: []ProductSales{},
		}
		if totals.PaidOrders > 0 {
			report.AverageTicket = totals.Revenue / int64(totals.PaidOrders)
		}
		byDay := map[string]sqlcgen.ReportDailyRow{}
		for _, row := range daily {
			byDay[row.Day] = row
		}
		// Days without sales are listed too, so charts keep their scale.
		for _, day := range period.Days() {
			row := byDay[day]
			report.Days = append(report.Days, DailySales{Date: day, Orders: row.Orders, Revenue: row.Revenue})
		}
		for _, row := range top {
			report.TopProducts = append(report.TopProducts, ProductSales{Name: row.ProductName, Quantity: row.Quantity, Revenue: row.Revenue})
		}
		return nil
	})
	return report, err
}

// RegisterReportRoutes adds the sales report for the store team.
func (m *Module) RegisterReportRoutes(router chi.Router) {
	m.route(router, http.MethodGet, "/reports/sales", authz.Permission("reports:read"), func(w http.ResponseWriter, r *http.Request) error {
		report, err := m.service.Report(r.Context(), r.URL.Query().Get("from"), r.URL.Query().Get("to"))
		if err != nil {
			return err
		}
		return httpx.WriteJSON(w, http.StatusOK, report)
	})
}
