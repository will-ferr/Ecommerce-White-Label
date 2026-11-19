package app

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/modules/identity"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/modules/sales"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/authz"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/httpx"
)

// customerData is the full export of a customer account (LGPD art. 18, V).
type customerData struct {
	ExportedAt time.Time            `json:"exportedAt"`
	Account    identity.AccountData `json:"account"`
	sales.CustomerData
}

func route(router chi.Router, logger *slog.Logger, method, pattern string, policy authz.Policy, fn httpx.HandlerFunc) {
	router.Method(method, pattern, authz.Protect(logger, policy, httpx.Handle(logger, fn)))
}

func principal(r *http.Request) authz.Principal {
	p, _ := authz.FromContext(r.Context())
	return p
}

// registerPrivacyRoutes adds the data rights of customers to the store. The
// data spans the identity and sales modules, so the routes compose both.
func registerPrivacyRoutes(store chi.Router, logger *slog.Logger, accounts *identity.Module, orders *sales.Module) {

	route(store, logger, http.MethodGet, "/account/privacy/export", authz.Authenticated, func(w http.ResponseWriter, r *http.Request) error {
		account, err := accounts.ExportAccount(r.Context(), principal(r))
		if err != nil {
			return err
		}
		data, err := orders.Service().ExportCustomer(r.Context(), principal(r))
		if err != nil {
			return err
		}
		w.Header().Set("Content-Disposition", `attachment; filename="meus-dados.json"`)
		return httpx.WriteJSON(w, http.StatusOK, customerData{ExportedAt: time.Now().UTC(), Account: account, CustomerData: data})
	})
	route(store, logger, http.MethodPost, "/account/privacy/erase", authz.Authenticated, func(w http.ResponseWriter, r *http.Request) error {
		if err := httpx.RequireSameOrigin(r); err != nil {
			return err
		}
		var body struct {
			Password string `json:"password"`
		}
		if err := httpx.DecodeJSON(r, &body); err != nil {
			return err
		}
		p := principal(r)
		err := accounts.EraseAccount(r.Context(), p, body.Password, func(ctx context.Context, tx pgx.Tx) error {
			return sales.EraseCustomer(ctx, tx, p)
		})
		if err != nil {
			return err
		}
		w.WriteHeader(http.StatusNoContent)
		return nil
	})
}

// registerCustomerRoutes adds the customer list for the store team.
func registerCustomerRoutes(staff chi.Router, logger *slog.Logger, accounts *identity.Module) {
	route(staff, logger, http.MethodGet, "/customers", authz.Permission("customers:read"), func(w http.ResponseWriter, r *http.Request) error {
		page, err := accounts.Customers(r.Context(), principal(r), r.URL.Query().Get("q"), r.URL.Query().Get("cursor"))
		if err != nil {
			return err
		}
		return httpx.WriteJSON(w, http.StatusOK, page)
	})
}
