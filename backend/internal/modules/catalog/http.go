package catalog

import (
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/modules/catalog/internal/domain"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/modules/inventory"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/authz"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/database"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/httpx"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/shared/apperr"
)

// Module exposes the catalog and inventory over HTTP.
type Module struct {
	service *Service
	db      *database.TenantDB
	logger  *slog.Logger
}

// New creates the module. The pool must use the runtime role.
func New(pool *pgxpool.Pool, logger *slog.Logger) *Module {
	db := database.NewTenantDB(pool)
	return &Module{service: NewService(db), db: db, logger: logger}
}

func (m *Module) route(router chi.Router, method, pattern string, policy authz.Policy, fn httpx.HandlerFunc) {
	router.Method(method, pattern, authz.Protect(m.logger, policy, httpx.Handle(m.logger, fn)))
}

func pathID(r *http.Request, name string) (uuid.UUID, error) {
	id, err := uuid.Parse(chi.URLParam(r, name))
	if err != nil {
		return uuid.Nil, apperr.New(apperr.KindNotFound, "RESOURCE_NOT_FOUND", "The resource does not exist.")
	}
	return id, nil
}

func principal(r *http.Request) authz.Principal {
	p, _ := authz.FromContext(r.Context())
	return p
}

func limitParam(r *http.Request) int32 {
	limit, err := strconv.Atoi(r.URL.Query().Get("limit"))
	if err != nil || limit < 1 || limit > 100 {
		return 0
	}
	return int32(limit) //nolint:gosec // bounded above
}

// RegisterStoreRoutes adds the public catalog of a store.
func (m *Module) RegisterStoreRoutes(router chi.Router) {
	m.route(router, http.MethodGet, "/catalog/categories", authz.Public, func(w http.ResponseWriter, r *http.Request) error {
		categories, err := m.service.Categories(r.Context())
		if err != nil {
			return err
		}
		return httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": categories})
	})
	m.route(router, http.MethodGet, "/catalog/products", authz.Public, func(w http.ResponseWriter, r *http.Request) error {
		query := r.URL.Query()
		page, err := m.service.StoreProducts(r.Context(), StoreFilter{
			CategorySlug: query.Get("category"), Featured: query.Get("featured") == "true",
			Search: query.Get("q"), Cursor: query.Get("cursor"), Limit: limitParam(r),
		})
		if err != nil {
			return err
		}
		return httpx.WriteJSON(w, http.StatusOK, page)
	})
	m.route(router, http.MethodGet, "/catalog/products/{slug}", authz.Public, func(w http.ResponseWriter, r *http.Request) error {
		product, err := m.service.StoreProduct(r.Context(), chi.URLParam(r, "slug"))
		if err != nil {
			return err
		}
		return httpx.WriteJSON(w, http.StatusOK, product)
	})
}

type productBody struct {
	domain.ProductInput
	Version int32 `json:"version"`
}

// RegisterAdminRoutes adds the catalog management of the store team.
func (m *Module) RegisterAdminRoutes(router chi.Router) {
	read, manage := authz.Permission("catalog:read"), authz.Permission("catalog:manage")

	m.route(router, http.MethodGet, "/catalog/categories", read, func(w http.ResponseWriter, r *http.Request) error {
		categories, err := m.service.Categories(r.Context())
		if err != nil {
			return err
		}
		return httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": categories})
	})
	m.route(router, http.MethodPost, "/catalog/categories", manage, func(w http.ResponseWriter, r *http.Request) error {
		var body domain.CategoryInput
		if err := httpx.DecodeJSON(r, &body); err != nil {
			return err
		}
		id, err := m.service.SaveCategory(r.Context(), principal(r), nil, body)
		if err != nil {
			return err
		}
		return httpx.WriteJSON(w, http.StatusCreated, map[string]any{"id": id})
	})
	m.route(router, http.MethodPut, "/catalog/categories/{id}", manage, func(w http.ResponseWriter, r *http.Request) error {
		id, err := pathID(r, "id")
		if err != nil {
			return err
		}
		var body domain.CategoryInput
		if err := httpx.DecodeJSON(r, &body); err != nil {
			return err
		}
		if _, err := m.service.SaveCategory(r.Context(), principal(r), &id, body); err != nil {
			return err
		}
		w.WriteHeader(http.StatusNoContent)
		return nil
	})
	m.route(router, http.MethodDelete, "/catalog/categories/{id}", manage, func(w http.ResponseWriter, r *http.Request) error {
		id, err := pathID(r, "id")
		if err != nil {
			return err
		}
		if err := m.service.DeleteCategory(r.Context(), principal(r), id); err != nil {
			return err
		}
		w.WriteHeader(http.StatusNoContent)
		return nil
	})

	m.route(router, http.MethodGet, "/catalog/products", read, func(w http.ResponseWriter, r *http.Request) error {
		query := r.URL.Query()
		page, err := m.service.AdminProducts(r.Context(), ListFilter{
			Status: query.Get("status"), Search: query.Get("q"), Cursor: query.Get("cursor"), Limit: limitParam(r),
		})
		if err != nil {
			return err
		}
		return httpx.WriteJSON(w, http.StatusOK, page)
	})
	m.route(router, http.MethodPost, "/catalog/products", manage, func(w http.ResponseWriter, r *http.Request) error {
		var body productBody
		if err := httpx.DecodeJSON(r, &body); err != nil {
			return err
		}
		id, err := m.service.SaveProduct(r.Context(), principal(r), nil, 0, body.ProductInput)
		if err != nil {
			return err
		}
		return httpx.WriteJSON(w, http.StatusCreated, map[string]any{"id": id})
	})
	m.route(router, http.MethodGet, "/catalog/products/{id}", read, func(w http.ResponseWriter, r *http.Request) error {
		id, err := pathID(r, "id")
		if err != nil {
			return err
		}
		product, err := m.service.AdminProduct(r.Context(), id)
		if err != nil {
			return err
		}
		return httpx.WriteJSON(w, http.StatusOK, product)
	})
	m.route(router, http.MethodPut, "/catalog/products/{id}", manage, func(w http.ResponseWriter, r *http.Request) error {
		id, err := pathID(r, "id")
		if err != nil {
			return err
		}
		var body productBody
		if err := httpx.DecodeJSON(r, &body); err != nil {
			return err
		}
		if _, err := m.service.SaveProduct(r.Context(), principal(r), &id, body.Version, body.ProductInput); err != nil {
			return err
		}
		w.WriteHeader(http.StatusNoContent)
		return nil
	})

	m.route(router, http.MethodPost, "/inventory/adjustments", authz.Permission("inventory:manage"), func(w http.ResponseWriter, r *http.Request) error {
		var body inventory.Adjustment
		if err := httpx.DecodeJSON(r, &body); err != nil {
			return err
		}
		var stock inventory.Stock
		err := m.service.in(r.Context(), func(tx pgx.Tx, _ *sqlcgenQueries, tenantID uuid.UUID) error {
			if err := m.service.requireVariant(r.Context(), tx, tenantID, body.VariantID); err != nil {
				return err
			}
			var err error
			stock, err = inventory.Adjust(r.Context(), tx, tenantID, body, principal(r).UserID.String())
			if err != nil {
				return err
			}
			return recordAudit(r.Context(), tx, principal(r), "inventory.stock.adjusted", "variant", body.VariantID)
		})
		if err != nil {
			return err
		}
		return httpx.WriteJSON(w, http.StatusOK, stock)
	})
	m.route(router, http.MethodGet, "/inventory/variants/{id}/movements", read, func(w http.ResponseWriter, r *http.Request) error {
		id, err := pathID(r, "id")
		if err != nil {
			return err
		}
		var movements []inventory.Movement
		err = m.service.in(r.Context(), func(tx pgx.Tx, _ *sqlcgenQueries, tenantID uuid.UUID) error {
			var err error
			movements, err = inventory.Movements(r.Context(), tx, tenantID, id)
			return err
		})
		if err != nil {
			return err
		}
		return httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": movements})
	})
}
