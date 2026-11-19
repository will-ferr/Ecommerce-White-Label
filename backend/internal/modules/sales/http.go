package sales

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/authz"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/httpx"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/ratelimit"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/shared/apperr"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/shared/br"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/shared/reqctx"
)

const cartCookieMaxAge = 30 * 24 * time.Hour

var checkoutByIP = ratelimit.Rule{Max: 20, Window: 10 * time.Minute}

// Module exposes sales over HTTP.
type Module struct {
	service      *Service
	logger       *slog.Logger
	limiter      *ratelimit.Limiter
	cookieSecure bool
}

// New creates the module. A nil limiter disables rate limiting.
func New(deps Deps, limiter *ratelimit.Limiter) *Module {
	return &Module{service: NewService(deps), logger: deps.Logger, limiter: limiter, cookieSecure: deps.CookieSecure}
}

// Service returns the use cases, used by background jobs.
func (m *Module) Service() *Service { return m.service }

func (m *Module) route(router chi.Router, method, pattern string, policy authz.Policy, fn httpx.HandlerFunc) {
	router.Method(method, pattern, authz.Protect(m.logger, policy, httpx.Handle(m.logger, fn)))
}

func (m *Module) cookieName() string {
	if m.cookieSecure {
		return "__Host-wl_cart"
	}
	return "wl_cart"
}

func (m *Module) cartRef(r *http.Request) CartRef {
	if principal, ok := authz.FromContext(r.Context()); ok {
		return CartRef{UserID: &principal.UserID}
	}
	if cookie, err := r.Cookie(m.cookieName()); err == nil && len(cookie.Value) <= 64 {
		return CartRef{Token: cookie.Value}
	}
	return CartRef{}
}

func (m *Module) setCartCookie(w http.ResponseWriter, token string, maxAge time.Duration) {
	http.SetCookie(w, &http.Cookie{ //nolint:gosec // Secure is mandatory in staging and production, enforced by config.
		Name: m.cookieName(), Value: token, Path: "/", HttpOnly: true, Secure: m.cookieSecure,
		SameSite: http.SameSiteLaxMode, MaxAge: int(maxAge.Seconds()),
	})
}

func optionalPrincipal(r *http.Request) *authz.Principal {
	if principal, ok := authz.FromContext(r.Context()); ok {
		return &principal
	}
	return nil
}

func pathUUID(r *http.Request, name string) (uuid.UUID, error) {
	id, err := uuid.Parse(chi.URLParam(r, name))
	if err != nil {
		return uuid.Nil, apperr.New(apperr.KindNotFound, "RESOURCE_NOT_FOUND", "The resource does not exist.")
	}
	return id, nil
}

func orderNumber(r *http.Request) (int64, error) {
	number, err := strconv.ParseInt(chi.URLParam(r, "number"), 10, 64)
	if err != nil || number <= 0 {
		return 0, orderNotFound()
	}
	return number, nil
}

func limit(r *http.Request) int32 {
	value, err := strconv.Atoi(r.URL.Query().Get("limit"))
	if err != nil || value < 1 || value > 100 {
		return 0
	}
	return int32(value) //nolint:gosec // bounded above
}

// RegisterStoreRoutes adds the purchase flow of a store.
func (m *Module) RegisterStoreRoutes(router chi.Router) {
	public, signedIn := authz.Public, authz.Authenticated

	m.route(router, http.MethodGet, "/cart", public, func(w http.ResponseWriter, r *http.Request) error {
		view, err := m.service.Cart(r.Context(), m.cartRef(r))
		if err != nil {
			return err
		}
		return httpx.WriteJSON(w, http.StatusOK, view)
	})
	m.route(router, http.MethodPost, "/cart/items", public, func(w http.ResponseWriter, r *http.Request) error {
		var body struct {
			VariantID uuid.UUID `json:"variantId"`
			Quantity  int32     `json:"quantity"`
		}
		return m.changeCart(w, r, &body, func() (uuid.UUID, int32, bool) { return body.VariantID, body.Quantity, false })
	})
	m.route(router, http.MethodPut, "/cart/items/{variantId}", public, func(w http.ResponseWriter, r *http.Request) error {
		var body struct {
			Quantity int32 `json:"quantity"`
		}
		id, err := pathUUID(r, "variantId")
		if err != nil {
			return err
		}
		return m.changeCart(w, r, &body, func() (uuid.UUID, int32, bool) { return id, body.Quantity, true })
	})
	m.route(router, http.MethodDelete, "/cart/items/{variantId}", public, func(w http.ResponseWriter, r *http.Request) error {
		id, err := pathUUID(r, "variantId")
		if err != nil {
			return err
		}
		return m.changeCart(w, r, nil, func() (uuid.UUID, int32, bool) { return id, 0, true })
	})
	m.route(router, http.MethodPost, "/cart/merge", signedIn, func(w http.ResponseWriter, r *http.Request) error {
		if err := httpx.RequireSameOrigin(r); err != nil {
			return err
		}
		token := ""
		if cookie, err := r.Cookie(m.cookieName()); err == nil {
			token = cookie.Value
		}
		principal, _ := authz.FromContext(r.Context())
		view, err := m.service.MergeVisitorCart(r.Context(), principal.UserID, token)
		if err != nil {
			return err
		}
		m.setCartCookie(w, "", -1)
		return httpx.WriteJSON(w, http.StatusOK, view)
	})

	m.route(router, http.MethodPost, "/checkout/summary", public, func(w http.ResponseWriter, r *http.Request) error {
		var body struct {
			ShippingMethodID *uuid.UUID `json:"shippingMethodId"`
			PostalCode       string     `json:"postalCode"`
		}
		if err := httpx.DecodeJSON(r, &body); err != nil {
			return err
		}
		summary, err := m.service.Summary(r.Context(), m.cartRef(r), body.ShippingMethodID, br.PostalCodeDigits(body.PostalCode))
		if err != nil {
			return err
		}
		return httpx.WriteJSON(w, http.StatusOK, summary)
	})
	m.route(router, http.MethodPost, "/checkout/orders", public, func(w http.ResponseWriter, r *http.Request) error {
		if err := httpx.RequireSameOrigin(r); err != nil {
			return err
		}
		if m.limiter != nil {
			if err := m.limiter.Enforce(r.Context(), "checkout:ip", reqctx.ClientIP(r.Context()), checkoutByIP); err != nil {
				return err
			}
		}
		var body CheckoutInput
		if err := httpx.DecodeJSON(r, &body); err != nil {
			return err
		}
		body.StoreHost = httpx.Hostname(r.Host)
		placed, err := m.service.PlaceOrder(r.Context(), m.cartRef(r), r.Header.Get("Idempotency-Key"), body)
		var review *ReviewError
		if errors.As(err, &review) {
			return httpx.WriteJSON(w, http.StatusConflict, map[string]any{
				"type": "urn:ecommerce:problem:" + review.Code, "code": review.Code, "status": http.StatusConflict,
				"title": "Conflict", "detail": review.Message(), "summary": review.Summary,
			})
		}
		if err != nil {
			return err
		}
		return httpx.WriteJSON(w, http.StatusCreated, placed)
	})

	m.route(router, http.MethodGet, "/orders/{number}", public, func(w http.ResponseWriter, r *http.Request) error {
		number, err := orderNumber(r)
		if err != nil {
			return err
		}
		view, err := m.service.CustomerOrder(r.Context(), number, optionalPrincipal(r), r.URL.Query().Get("token"))
		if err != nil {
			return err
		}
		return httpx.WriteJSON(w, http.StatusOK, view)
	})
	m.route(router, http.MethodPost, "/webhooks/payments/{provider}", public, func(w http.ResponseWriter, r *http.Request) error {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			return apperr.Wrap(apperr.KindInvalid, "INVALID_BODY", "The request body could not be read.", err)
		}
		outcome, err := m.service.HandlePaymentNotification(r.Context(), chi.URLParam(r, "provider"), r.Header, body)
		if err != nil {
			return err
		}
		return httpx.WriteJSON(w, http.StatusOK, map[string]string{"outcome": outcome})
	})
	if m.service.testProvider != nil {
		m.route(router, http.MethodPost, "/orders/{number}/test-payment", public, func(w http.ResponseWriter, r *http.Request) error {
			if err := httpx.RequireSameOrigin(r); err != nil {
				return err
			}
			number, err := orderNumber(r)
			if err != nil {
				return err
			}
			var body struct {
				Token    string `json:"token"`
				Approved bool   `json:"approved"`
			}
			if err := httpx.DecodeJSON(r, &body); err != nil {
				return err
			}
			view, err := m.service.ConfirmTestPayment(r.Context(), number, optionalPrincipal(r), body.Token, body.Approved)
			if err != nil {
				return err
			}
			return httpx.WriteJSON(w, http.StatusOK, view)
		})
	}

	m.route(router, http.MethodGet, "/account/orders", signedIn, func(w http.ResponseWriter, r *http.Request) error {
		principal, _ := authz.FromContext(r.Context())
		page, err := m.service.CustomerOrders(r.Context(), principal, r.URL.Query().Get("cursor"))
		if err != nil {
			return err
		}
		return httpx.WriteJSON(w, http.StatusOK, page)
	})
	m.route(router, http.MethodGet, "/account/addresses", signedIn, func(w http.ResponseWriter, r *http.Request) error {
		principal, _ := authz.FromContext(r.Context())
		addresses, err := m.service.Addresses(r.Context(), principal)
		if err != nil {
			return err
		}
		return httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": addresses})
	})
	m.route(router, http.MethodPost, "/account/addresses", signedIn, func(w http.ResponseWriter, r *http.Request) error {
		var body AddressInput
		if err := httpx.DecodeJSON(r, &body); err != nil {
			return err
		}
		principal, _ := authz.FromContext(r.Context())
		id, err := m.service.AddAddress(r.Context(), principal, body)
		if err != nil {
			return err
		}
		return httpx.WriteJSON(w, http.StatusCreated, map[string]any{"id": id})
	})
	m.route(router, http.MethodDelete, "/account/addresses/{id}", signedIn, func(w http.ResponseWriter, r *http.Request) error {
		id, err := pathUUID(r, "id")
		if err != nil {
			return err
		}
		principal, _ := authz.FromContext(r.Context())
		if err := m.service.DeleteAddress(r.Context(), principal, id); err != nil {
			return err
		}
		w.WriteHeader(http.StatusNoContent)
		return nil
	})
}

func (m *Module) changeCart(w http.ResponseWriter, r *http.Request, body any, values func() (uuid.UUID, int32, bool)) error {
	if err := httpx.RequireSameOrigin(r); err != nil {
		return err
	}
	if body != nil {
		if err := httpx.DecodeJSON(r, body); err != nil {
			return err
		}
	}
	variantID, quantity, replace := values()
	view, token, err := m.service.ChangeItem(r.Context(), m.cartRef(r), variantID, quantity, replace)
	if err != nil {
		return err
	}
	if token != "" {
		m.setCartCookie(w, token, cartCookieMaxAge)
	}
	return httpx.WriteJSON(w, http.StatusOK, view)
}

// RegisterAdminRoutes adds order and delivery management for the store team.
func (m *Module) RegisterAdminRoutes(router chi.Router) {
	read, manage := authz.Permission("orders:read"), authz.Permission("orders:manage")

	m.route(router, http.MethodGet, "/orders", read, func(w http.ResponseWriter, r *http.Request) error {
		query := r.URL.Query()
		page, err := m.service.AdminOrders(r.Context(), OrderFilter{
			Status: query.Get("status"), Search: query.Get("q"), Cursor: query.Get("cursor"), Limit: limit(r),
		})
		if err != nil {
			return err
		}
		return httpx.WriteJSON(w, http.StatusOK, page)
	})
	m.route(router, http.MethodGet, "/orders/export", read, func(w http.ResponseWriter, r *http.Request) error {
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="pedidos.csv"`)
		query := r.URL.Query()
		return m.service.ExportOrders(r.Context(), OrderFilter{Status: query.Get("status"), Search: query.Get("q")}, w)
	})
	m.route(router, http.MethodGet, "/orders/{id}", read, func(w http.ResponseWriter, r *http.Request) error {
		id, err := pathUUID(r, "id")
		if err != nil {
			return err
		}
		view, err := m.service.AdminOrder(r.Context(), id)
		if err != nil {
			return err
		}
		return httpx.WriteJSON(w, http.StatusOK, view)
	})
	m.route(router, http.MethodPost, "/orders/{id}/status", manage, func(w http.ResponseWriter, r *http.Request) error {
		id, err := pathUUID(r, "id")
		if err != nil {
			return err
		}
		var body StatusChange
		if err := httpx.DecodeJSON(r, &body); err != nil {
			return err
		}
		principal, _ := authz.FromContext(r.Context())
		if err := m.service.ChangeStatus(r.Context(), principal, id, body); err != nil {
			return err
		}
		w.WriteHeader(http.StatusNoContent)
		return nil
	})
	m.route(router, http.MethodPost, "/orders/{id}/notes", manage, func(w http.ResponseWriter, r *http.Request) error {
		id, err := pathUUID(r, "id")
		if err != nil {
			return err
		}
		var body struct {
			Body string `json:"body"`
		}
		if err := httpx.DecodeJSON(r, &body); err != nil {
			return err
		}
		principal, _ := authz.FromContext(r.Context())
		if err := m.service.AddNote(r.Context(), principal, id, body.Body); err != nil {
			return err
		}
		w.WriteHeader(http.StatusNoContent)
		return nil
	})

	m.route(router, http.MethodGet, "/shipping/methods", read, func(w http.ResponseWriter, r *http.Request) error {
		methods, err := m.service.ShippingMethods(r.Context())
		if err != nil {
			return err
		}
		return httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": methods})
	})
	saveShipping := func(w http.ResponseWriter, r *http.Request, id *uuid.UUID) error {
		var body ShippingInput
		if err := httpx.DecodeJSON(r, &body); err != nil {
			return err
		}
		principal, _ := authz.FromContext(r.Context())
		saved, err := m.service.SaveShippingMethod(r.Context(), principal, id, body)
		if err != nil {
			return err
		}
		return httpx.WriteJSON(w, http.StatusOK, map[string]any{"id": saved})
	}
	m.route(router, http.MethodPost, "/shipping/methods", authz.Permission("shipping:manage"), func(w http.ResponseWriter, r *http.Request) error {
		return saveShipping(w, r, nil)
	})
	m.route(router, http.MethodPut, "/shipping/methods/{id}", authz.Permission("shipping:manage"), func(w http.ResponseWriter, r *http.Request) error {
		id, err := pathUUID(r, "id")
		if err != nil {
			return err
		}
		return saveShipping(w, r, &id)
	})
}
