// Package catalog manages categories, products, variants and their prices.
package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/modules/audit"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/modules/catalog/internal/domain"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/modules/catalog/internal/sqlcgen"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/modules/inventory"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/authz"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/database"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/tenancy"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/shared/apperr"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/shared/pagination"
)

// SaleVariant is what checkout needs to know about a variant.
type SaleVariant struct {
	VariantID   uuid.UUID
	ProductID   uuid.UUID
	ProductName string
	ProductSlug string
	Label       string
	SKU         string
	Price       int64
	WeightGrams int32
}

// VariantsForSale returns the variants that can be sold now. Archived variants
// and inactive products are absent from the result.
func VariantsForSale(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]SaleVariant, error) {
	rows, err := sqlcgen.New(tx).VariantsForSale(ctx, sqlcgen.VariantsForSaleParams{TenantID: tenantID, VariantIds: ids})
	if err != nil {
		return nil, fmt.Errorf("load variants: %w", err)
	}
	variants := make(map[uuid.UUID]SaleVariant, len(rows))
	for _, row := range rows {
		variants[row.ID] = SaleVariant{
			VariantID: row.ID, ProductID: row.ProductID, ProductName: row.ProductName, ProductSlug: row.ProductSlug,
			Label: domain.VariantLabel(decodeOptions(row.Options)), SKU: row.Sku, Price: row.PriceAmount, WeightGrams: row.WeightGrams,
		}
	}
	return variants, nil
}

func decodeOptions(raw json.RawMessage) map[string]string {
	options := map[string]string{}
	_ = json.Unmarshal(raw, &options)
	return options
}

// Service implements the catalog use cases.
type Service struct {
	db *database.TenantDB
}

// NewService creates a Service.
func NewService(db *database.TenantDB) *Service {
	return &Service{db: db}
}

func (s *Service) in(ctx context.Context, fn func(pgx.Tx, *sqlcgen.Queries, uuid.UUID) error) error {
	tenantID, ok := tenancy.FromContext(ctx)
	if !ok {
		return tenancy.ErrMissingTenant
	}
	return s.db.Run(ctx, func(tx pgx.Tx) error { return fn(tx, sqlcgen.New(tx), tenantID) })
}

func newID() uuid.UUID {
	id, err := uuid.NewV7()
	if err != nil {
		return uuid.New()
	}
	return id
}

func conflictOn(err error, code, message string) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return apperr.New(apperr.KindConflict, code, message)
	}
	return err
}

func notFound() error {
	return apperr.New(apperr.KindNotFound, "PRODUCT_NOT_FOUND", "The product does not exist.")
}

// Category is a node of the category tree.
type Category struct {
	ID          uuid.UUID  `json:"id"`
	ParentID    *uuid.UUID `json:"parentId"`
	Name        string     `json:"name"`
	Slug        string     `json:"slug"`
	Description string     `json:"description"`
	Position    int32      `json:"position"`
}

// Categories returns every category of the store.
func (s *Service) Categories(ctx context.Context) ([]Category, error) {
	categories := []Category{}
	err := s.in(ctx, func(_ pgx.Tx, q *sqlcgen.Queries, tenantID uuid.UUID) error {
		rows, err := q.ListCategories(ctx, tenantID)
		for _, row := range rows {
			categories = append(categories, Category{
				ID: row.ID, ParentID: row.ParentID, Name: row.Name, Slug: row.Slug, Description: row.Description, Position: row.Position,
			})
		}
		return err
	})
	return categories, err
}

func (s *Service) checkParent(ctx context.Context, q *sqlcgen.Queries, tenantID uuid.UUID, id uuid.UUID, parent *uuid.UUID) error {
	if parent == nil {
		return nil
	}
	invalid := apperr.Validation(apperr.FieldError{Field: "parentId", Code: "invalid", Message: "Choose a valid parent category."})
	if *parent == id {
		return invalid
	}
	rows, err := q.ListCategories(ctx, tenantID)
	if err != nil {
		return err
	}
	parents := map[uuid.UUID]*uuid.UUID{}
	for _, row := range rows {
		parents[row.ID] = row.ParentID
	}
	// Walk up from the new parent: reaching the category itself would create a cycle.
	for current, steps := parent, 0; current != nil; current, steps = parents[*current], steps+1 {
		if _, exists := parents[*current]; !exists || *current == id || steps > len(rows) {
			return invalid
		}
	}
	return nil
}

// SaveCategory creates a category, or changes it when id is given.
func (s *Service) SaveCategory(ctx context.Context, principal authz.Principal, id *uuid.UUID, input domain.CategoryInput) (uuid.UUID, error) {
	input, err := input.Normalize()
	if err != nil {
		return uuid.Nil, err
	}
	categoryID := newID()
	if id != nil {
		categoryID = *id
	}
	err = s.in(ctx, func(tx pgx.Tx, q *sqlcgen.Queries, tenantID uuid.UUID) error {
		if err := s.checkParent(ctx, q, tenantID, categoryID, input.ParentID); err != nil {
			return err
		}
		if id == nil {
			err = q.InsertCategory(ctx, sqlcgen.InsertCategoryParams{
				TenantID: tenantID, ID: categoryID, ParentID: input.ParentID, Name: input.Name, Slug: input.Slug,
				Description: input.Description, Position: input.Position,
			})
		} else {
			var affected int64
			affected, err = q.UpdateCategory(ctx, sqlcgen.UpdateCategoryParams{
				TenantID: tenantID, ID: categoryID, ParentID: input.ParentID, Name: input.Name, Slug: input.Slug,
				Description: input.Description, Position: input.Position,
			})
			if err == nil && affected == 0 {
				return apperr.New(apperr.KindNotFound, "CATEGORY_NOT_FOUND", "The category does not exist.")
			}
		}
		if err != nil {
			return conflictOn(err, "SLUG_IN_USE", "Another category already uses this address.")
		}
		return recordAudit(ctx, tx, principal, "catalog.category.saved", "category", categoryID)
	})
	return categoryID, err
}

// DeleteCategory removes an empty category.
func (s *Service) DeleteCategory(ctx context.Context, principal authz.Principal, id uuid.UUID) error {
	return s.in(ctx, func(tx pgx.Tx, q *sqlcgen.Queries, tenantID uuid.UUID) error {
		inUse, err := q.CategoryInUse(ctx, sqlcgen.CategoryInUseParams{TenantID: tenantID, CategoryID: &id})
		if err != nil {
			return err
		}
		if inUse {
			return apperr.New(apperr.KindConflict, "CATEGORY_IN_USE", "Move its products and subcategories before deleting it.")
		}
		affected, err := q.DeleteCategory(ctx, sqlcgen.DeleteCategoryParams{TenantID: tenantID, ID: id})
		if err != nil {
			return err
		}
		if affected == 0 {
			return apperr.New(apperr.KindNotFound, "CATEGORY_NOT_FOUND", "The category does not exist.")
		}
		return recordAudit(ctx, tx, principal, "catalog.category.deleted", "category", id)
	})
}

func recordAudit(ctx context.Context, tx pgx.Tx, principal authz.Principal, action, targetType string, targetID uuid.UUID) error {
	return audit.Record(ctx, tx, audit.Entry{
		TenantID: principal.TenantID, ActorType: audit.ActorUser, ActorID: principal.UserID.String(),
		Action: action, TargetType: targetType, TargetID: targetID.String(),
	})
}

// VariantView is a variant as shown to the store team.
type VariantView struct {
	ID          uuid.UUID         `json:"id"`
	SKU         string            `json:"sku"`
	GTIN        string            `json:"gtin"`
	Options     map[string]string `json:"options"`
	Label       string            `json:"label"`
	WeightGrams int32             `json:"weightGrams"`
	Price       int64             `json:"price"`
	CompareAt   *int64            `json:"compareAtPrice"`
	Cost        *int64            `json:"costPrice"`
	Status      string            `json:"status"`
	Stock       inventory.Stock   `json:"stock"`
}

// ProductView is a product as shown to the store team.
type ProductView struct {
	ID             uuid.UUID      `json:"id"`
	CategoryID     *uuid.UUID     `json:"categoryId"`
	Name           string         `json:"name"`
	Slug           string         `json:"slug"`
	Description    string         `json:"description"`
	Brand          string         `json:"brand"`
	Status         string         `json:"status"`
	Featured       bool           `json:"featured"`
	SEOTitle       string         `json:"seoTitle"`
	SEODescription string         `json:"seoDescription"`
	Version        int32          `json:"version"`
	Images         []domain.Image `json:"images"`
	Variants       []VariantView  `json:"variants"`
}

type productParts struct {
	images   map[uuid.UUID][]domain.Image
	variants map[uuid.UUID][]sqlcgen.CatalogVariant
	stock    map[uuid.UUID]inventory.Stock
}

func loadParts(ctx context.Context, tx pgx.Tx, q *sqlcgen.Queries, tenantID uuid.UUID, ids []uuid.UUID, variantStatus *string) (productParts, error) {
	parts := productParts{images: map[uuid.UUID][]domain.Image{}, variants: map[uuid.UUID][]sqlcgen.CatalogVariant{}}
	images, err := q.ListImages(ctx, sqlcgen.ListImagesParams{TenantID: tenantID, ProductIds: ids})
	if err != nil {
		return parts, fmt.Errorf("list images: %w", err)
	}
	for _, image := range images {
		parts.images[image.ProductID] = append(parts.images[image.ProductID], domain.Image{URL: image.Url, Alt: image.Alt})
	}
	variants, err := q.ListVariants(ctx, sqlcgen.ListVariantsParams{TenantID: tenantID, ProductIds: ids, Status: variantStatus})
	if err != nil {
		return parts, fmt.Errorf("list variants: %w", err)
	}
	variantIDs := make([]uuid.UUID, 0, len(variants))
	for _, variant := range variants {
		parts.variants[variant.ProductID] = append(parts.variants[variant.ProductID], variant)
		variantIDs = append(variantIDs, variant.ID)
	}
	parts.stock, err = inventory.Levels(ctx, tx, tenantID, variantIDs)
	return parts, err
}

func adminView(product sqlcgen.CatalogProduct, parts productParts) ProductView {
	view := ProductView{
		ID: product.ID, CategoryID: product.CategoryID, Name: product.Name, Slug: product.Slug,
		Description: product.Description, Brand: product.Brand, Status: product.Status, Featured: product.Featured,
		SEOTitle: product.SeoTitle, SEODescription: product.SeoDescription, Version: product.Version,
		Images: parts.images[product.ID], Variants: []VariantView{},
	}
	if view.Images == nil {
		view.Images = []domain.Image{}
	}
	for _, variant := range parts.variants[product.ID] {
		options := decodeOptions(variant.Options)
		gtin := ""
		if variant.Gtin != nil {
			gtin = *variant.Gtin
		}
		view.Variants = append(view.Variants, VariantView{
			ID: variant.ID, SKU: variant.Sku, GTIN: gtin, Options: options, Label: domain.VariantLabel(options),
			WeightGrams: variant.WeightGrams, Price: variant.PriceAmount, CompareAt: variant.CompareAtAmount,
			Cost: variant.CostAmount, Status: variant.Status, Stock: parts.stock[variant.ID],
		})
	}
	return view
}

// ListFilter selects products.
type ListFilter struct {
	Status     string
	CategoryID *uuid.UUID
	Featured   *bool
	Search     string
	Cursor     string
	Limit      int32
}

func escapeLike(text string) string {
	return strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(strings.TrimSpace(text))
}

func (s *Service) list(ctx context.Context, q *sqlcgen.Queries, tenantID uuid.UUID, filter ListFilter) ([]sqlcgen.CatalogProduct, string, error) {
	cursor, err := pagination.Decode(filter.Cursor)
	if err != nil {
		return nil, "", err
	}
	limit := filter.Limit
	if limit <= 0 || limit > pagination.MaxLimit {
		limit = pagination.DefaultLimit
	}
	params := sqlcgen.ListProductsParams{TenantID: tenantID, CategoryID: filter.CategoryID, Featured: filter.Featured, RowLimit: limit + 1}
	if filter.Status != "" {
		params.Status = &filter.Status
	}
	if search := escapeLike(filter.Search); search != "" {
		if len(search) > 100 {
			search = search[:100]
		}
		params.Search = &search
	}
	if cursor != nil {
		params.AfterAt, params.AfterID = &cursor.At, &cursor.ID
	}
	rows, err := q.ListProducts(ctx, params)
	if err != nil {
		return nil, "", fmt.Errorf("list products: %w", err)
	}
	next := ""
	if len(rows) > int(limit) {
		rows = rows[:limit]
		last := rows[len(rows)-1]
		next = pagination.Cursor{At: last.CreatedAt, ID: last.ID}.Encode()
	}
	return rows, next, nil
}

func ids(products []sqlcgen.CatalogProduct) []uuid.UUID {
	result := make([]uuid.UUID, 0, len(products))
	for _, product := range products {
		result = append(result, product.ID)
	}
	return result
}

// AdminProducts lists products of any status for the store team.
func (s *Service) AdminProducts(ctx context.Context, filter ListFilter) (pagination.Page[ProductView], error) {
	page := pagination.Page[ProductView]{Items: []ProductView{}}
	err := s.in(ctx, func(tx pgx.Tx, q *sqlcgen.Queries, tenantID uuid.UUID) error {
		rows, next, err := s.list(ctx, q, tenantID, filter)
		if err != nil {
			return err
		}
		parts, err := loadParts(ctx, tx, q, tenantID, ids(rows), nil)
		if err != nil {
			return err
		}
		for _, row := range rows {
			page.Items = append(page.Items, adminView(row, parts))
		}
		page.NextCursor = next
		return nil
	})
	return page, err
}

// AdminProduct returns one product for the store team.
func (s *Service) AdminProduct(ctx context.Context, id uuid.UUID) (ProductView, error) {
	var view ProductView
	err := s.in(ctx, func(tx pgx.Tx, q *sqlcgen.Queries, tenantID uuid.UUID) error {
		product, err := q.GetProduct(ctx, sqlcgen.GetProductParams{TenantID: tenantID, ID: id})
		if errors.Is(err, pgx.ErrNoRows) {
			return notFound()
		}
		if err != nil {
			return err
		}
		parts, err := loadParts(ctx, tx, q, tenantID, []uuid.UUID{id}, nil)
		view = adminView(product, parts)
		return err
	})
	return view, err
}

// SaveProduct creates a product, or replaces it when id is given. The version
// prevents one person from silently overwriting the changes of another.
func (s *Service) SaveProduct(ctx context.Context, principal authz.Principal, id *uuid.UUID, version int32, input domain.ProductInput) (uuid.UUID, error) {
	input, err := input.Normalize()
	if err != nil {
		return uuid.Nil, err
	}
	productID := newID()
	if id != nil {
		productID = *id
	}
	err = s.in(ctx, func(tx pgx.Tx, q *sqlcgen.Queries, tenantID uuid.UUID) error {
		if id == nil {
			err = q.InsertProduct(ctx, sqlcgen.InsertProductParams{
				TenantID: tenantID, ID: productID, CategoryID: input.CategoryID, Name: input.Name, Slug: input.Slug,
				Description: input.Description, Brand: input.Brand, Status: input.Status, Featured: input.Featured,
				SeoTitle: input.SEOTitle, SeoDescription: input.SEODescription,
			})
		} else {
			var affected int64
			affected, err = q.UpdateProduct(ctx, sqlcgen.UpdateProductParams{
				TenantID: tenantID, ID: productID, CategoryID: input.CategoryID, Name: input.Name, Slug: input.Slug,
				Description: input.Description, Brand: input.Brand, Status: input.Status, Featured: input.Featured,
				SeoTitle: input.SEOTitle, SeoDescription: input.SEODescription, Version: version,
			})
			if err == nil && affected == 0 {
				if _, getErr := q.GetProduct(ctx, sqlcgen.GetProductParams{TenantID: tenantID, ID: productID}); errors.Is(getErr, pgx.ErrNoRows) {
					return notFound()
				}
				return apperr.New(apperr.KindConflict, "VERSION_CONFLICT", "Someone else changed this product. Reload it and try again.")
			}
		}
		if err != nil {
			return s.translateSaveError(err)
		}
		if err := saveImages(ctx, q, tenantID, productID, input.Images); err != nil {
			return err
		}
		if err := s.saveVariants(ctx, q, tenantID, productID, input.Variants, principal); err != nil {
			return s.translateSaveError(err)
		}
		return recordAudit(ctx, tx, principal, "catalog.product.saved", "product", productID)
	})
	return productID, err
}

func (s *Service) translateSaveError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch {
		case pgErr.Code == "23505" && strings.Contains(pgErr.ConstraintName, "sku"):
			return apperr.New(apperr.KindConflict, "SKU_IN_USE", "Another variant already uses this SKU.")
		case pgErr.Code == "23505":
			return apperr.New(apperr.KindConflict, "SLUG_IN_USE", "Another product already uses this address.")
		case pgErr.Code == "23503":
			return apperr.Validation(apperr.FieldError{Field: "categoryId", Code: "invalid", Message: "Choose a valid category."})
		}
	}
	return err
}

func saveImages(ctx context.Context, q *sqlcgen.Queries, tenantID, productID uuid.UUID, images []domain.Image) error {
	if err := q.DeleteImages(ctx, sqlcgen.DeleteImagesParams{TenantID: tenantID, ProductID: productID}); err != nil {
		return fmt.Errorf("delete images: %w", err)
	}
	for position, image := range images {
		err := q.InsertImage(ctx, sqlcgen.InsertImageParams{
			TenantID: tenantID, ID: newID(), ProductID: productID, Url: image.URL, Alt: image.Alt, Position: int32(position), //nolint:gosec // at most 10 images
		})
		if err != nil {
			return fmt.Errorf("insert image: %w", err)
		}
	}
	return nil
}

// saveVariants updates known variants, creates new ones and archives the rest.
// Variants are never deleted, because past orders refer to them.
func (s *Service) saveVariants(ctx context.Context, q *sqlcgen.Queries, tenantID, productID uuid.UUID, variants []domain.VariantInput, principal authz.Principal) error {
	existing, err := q.ListVariants(ctx, sqlcgen.ListVariantsParams{TenantID: tenantID, ProductIds: []uuid.UUID{productID}})
	if err != nil {
		return fmt.Errorf("list variants: %w", err)
	}
	previous := map[uuid.UUID]sqlcgen.CatalogVariant{}
	for _, variant := range existing {
		previous[variant.ID] = variant
	}

	kept := make([]uuid.UUID, 0, len(variants))
	for position, input := range variants {
		options, err := json.Marshal(input.Options)
		if err != nil {
			return fmt.Errorf("encode options: %w", err)
		}
		var gtin *string
		if input.GTIN != "" {
			gtin = &input.GTIN
		}
		variantID := newID()
		old, known := sqlcgen.CatalogVariant{}, false
		if input.ID != nil {
			old, known = previous[*input.ID]
			if !known {
				return apperr.Validation(apperr.FieldError{Field: "variants.id", Code: "invalid", Message: "A variant does not belong to this product."})
			}
			variantID = *input.ID
			_, err = q.UpdateVariant(ctx, sqlcgen.UpdateVariantParams{
				TenantID: tenantID, ID: variantID, ProductID: productID, Sku: input.SKU, Gtin: gtin, Options: options,
				WeightGrams: input.WeightGrams, PriceAmount: input.Price, CompareAtAmount: input.CompareAt,
				CostAmount: input.Cost, Position: int32(position), //nolint:gosec // at most 50 variants
			})
		} else {
			err = q.InsertVariant(ctx, sqlcgen.InsertVariantParams{
				TenantID: tenantID, ID: variantID, ProductID: productID, Sku: input.SKU, Gtin: gtin, Options: options,
				WeightGrams: input.WeightGrams, PriceAmount: input.Price, CompareAtAmount: input.CompareAt,
				CostAmount: input.Cost, Position: int32(position), //nolint:gosec // at most 50 variants
			})
		}
		if err != nil {
			return err
		}
		kept = append(kept, variantID)

		priceChanged := !known || old.PriceAmount != input.Price || !sameAmount(old.CompareAtAmount, input.CompareAt) || !sameAmount(old.CostAmount, input.Cost)
		if priceChanged {
			if err := q.InsertPriceHistory(ctx, sqlcgen.InsertPriceHistoryParams{
				TenantID: tenantID, ID: newID(), VariantID: variantID, PriceAmount: input.Price,
				CompareAtAmount: input.CompareAt, CostAmount: input.Cost, ChangedBy: principal.UserID.String(),
			}); err != nil {
				return fmt.Errorf("record price history: %w", err)
			}
		}
	}
	return q.ArchiveMissingVariants(ctx, sqlcgen.ArchiveMissingVariantsParams{TenantID: tenantID, ProductID: productID, KeptIds: kept})
}

func sameAmount(a, b *int64) bool {
	return (a == nil && b == nil) || (a != nil && b != nil && *a == *b)
}
