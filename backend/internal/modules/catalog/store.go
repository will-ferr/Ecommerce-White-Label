package catalog

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/modules/catalog/internal/domain"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/modules/catalog/internal/sqlcgen"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/shared/apperr"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/shared/pagination"
)

// ProductCard is a product in a listing of the store.
type ProductCard struct {
	ID        uuid.UUID     `json:"id"`
	Name      string        `json:"name"`
	Slug      string        `json:"slug"`
	Brand     string        `json:"brand"`
	Image     *domain.Image `json:"image"`
	Price     int64         `json:"price"`
	CompareAt *int64        `json:"compareAtPrice"`
	HasRange  bool          `json:"hasPriceRange"`
	Available bool          `json:"available"`
}

// StoreVariant is a variant as shown to buyers. Cost and stock counts stay private.
type StoreVariant struct {
	ID        uuid.UUID         `json:"id"`
	Label     string            `json:"label"`
	Options   map[string]string `json:"options"`
	Price     int64             `json:"price"`
	CompareAt *int64            `json:"compareAtPrice"`
	Available bool              `json:"available"`
}

// ProductDetail is the product page of the store.
type ProductDetail struct {
	ID             uuid.UUID      `json:"id"`
	Name           string         `json:"name"`
	Slug           string         `json:"slug"`
	Brand          string         `json:"brand"`
	Description    string         `json:"description"`
	SEOTitle       string         `json:"seoTitle"`
	SEODescription string         `json:"seoDescription"`
	Category       *Category      `json:"category"`
	Images         []domain.Image `json:"images"`
	Variants       []StoreVariant `json:"variants"`
}

func storeVariants(variants []sqlcgen.CatalogVariant, parts productParts) []StoreVariant {
	result := make([]StoreVariant, 0, len(variants))
	for _, variant := range variants {
		options := decodeOptions(variant.Options)
		result = append(result, StoreVariant{
			ID: variant.ID, Label: domain.VariantLabel(options), Options: options, Price: variant.PriceAmount,
			CompareAt: variant.CompareAtAmount, Available: parts.stock[variant.ID].Available > 0,
		})
	}
	return result
}

func card(product sqlcgen.CatalogProduct, parts productParts) (ProductCard, bool) {
	variants := storeVariants(parts.variants[product.ID], parts)
	if len(variants) == 0 {
		return ProductCard{}, false
	}
	result := ProductCard{ID: product.ID, Name: product.Name, Slug: product.Slug, Brand: product.Brand}
	if images := parts.images[product.ID]; len(images) > 0 {
		result.Image = &images[0]
	}
	cheapest := variants[0]
	for _, variant := range variants {
		if variant.Price < cheapest.Price {
			cheapest = variant
		}
		if variant.Price != variants[0].Price {
			result.HasRange = true
		}
		result.Available = result.Available || variant.Available
	}
	result.Price, result.CompareAt = cheapest.Price, cheapest.CompareAt
	return result, true
}

// StoreFilter selects products of the store.
type StoreFilter struct {
	CategorySlug string
	Featured     bool
	Search       string
	Cursor       string
	Limit        int32
}

// StoreProducts lists the active products that have something to sell.
func (s *Service) StoreProducts(ctx context.Context, filter StoreFilter) (pagination.Page[ProductCard], error) {
	page := pagination.Page[ProductCard]{Items: []ProductCard{}}
	err := s.in(ctx, func(tx pgx.Tx, q *sqlcgen.Queries, tenantID uuid.UUID) error {
		list := ListFilter{Status: domain.StatusActive, Search: filter.Search, Cursor: filter.Cursor, Limit: filter.Limit}
		if filter.Featured {
			featured := true
			list.Featured = &featured
		}
		if filter.CategorySlug != "" {
			category, err := q.GetCategoryBySlug(ctx, sqlcgen.GetCategoryBySlugParams{TenantID: tenantID, Slug: filter.CategorySlug})
			if errors.Is(err, pgx.ErrNoRows) {
				return apperr.New(apperr.KindNotFound, "CATEGORY_NOT_FOUND", "The category does not exist.")
			}
			if err != nil {
				return err
			}
			list.CategoryID = &category.ID
		}
		var (
			rows []sqlcgen.CatalogProduct
			next string
			err  error
		)
		if query := strings.TrimSpace(filter.Search); query != "" {
			rows, err = s.search(ctx, q, tenantID, query, list.CategoryID)
		} else {
			rows, next, err = s.list(ctx, q, tenantID, list)
		}
		if err != nil {
			return err
		}
		active := domain.StatusActive
		parts, err := loadParts(ctx, tx, q, tenantID, ids(rows), &active)
		if err != nil {
			return err
		}
		for _, row := range rows {
			if item, ok := card(row, parts); ok {
				page.Items = append(page.Items, item)
			}
		}
		page.NextCursor = next
		return nil
	})
	return page, err
}

// searchLimit bounds search results. They are ranked, so later pages would
// rarely be useful, and a refined query finds the rest.
const searchLimit = 48

func (s *Service) search(ctx context.Context, q *sqlcgen.Queries, tenantID uuid.UUID, query string, categoryID *uuid.UUID) ([]sqlcgen.CatalogProduct, error) {
	if utf8.RuneCountInString(query) > 100 {
		return nil, apperr.Validation(apperr.FieldError{Field: "q", Code: "too_long", Message: "Use up to 100 characters."})
	}
	found, err := q.SearchProducts(ctx, sqlcgen.SearchProductsParams{TenantID: tenantID, CategoryID: categoryID, Query: query, RowLimit: searchLimit})
	if err != nil {
		return nil, fmt.Errorf("search products: %w", err)
	}
	rows := make([]sqlcgen.CatalogProduct, 0, len(found))
	for _, row := range found {
		rows = append(rows, row.CatalogProduct)
	}
	return rows, nil
}

// StoreProduct returns the page of an active product.
func (s *Service) StoreProduct(ctx context.Context, slug string) (ProductDetail, error) {
	var detail ProductDetail
	err := s.in(ctx, func(tx pgx.Tx, q *sqlcgen.Queries, tenantID uuid.UUID) error {
		product, err := q.GetActiveProductBySlug(ctx, sqlcgen.GetActiveProductBySlugParams{TenantID: tenantID, Slug: slug})
		if errors.Is(err, pgx.ErrNoRows) {
			return notFound()
		}
		if err != nil {
			return err
		}
		active := domain.StatusActive
		parts, err := loadParts(ctx, tx, q, tenantID, []uuid.UUID{product.ID}, &active)
		if err != nil {
			return err
		}
		detail = ProductDetail{
			ID: product.ID, Name: product.Name, Slug: product.Slug, Brand: product.Brand, Description: product.Description,
			SEOTitle: product.SeoTitle, SEODescription: product.SeoDescription, Images: parts.images[product.ID],
			Variants: storeVariants(parts.variants[product.ID], parts),
		}
		if detail.Images == nil {
			detail.Images = []domain.Image{}
		}
		if len(detail.Variants) == 0 {
			return notFound()
		}
		if product.CategoryID != nil {
			rows, err := q.ListCategories(ctx, tenantID)
			if err != nil {
				return err
			}
			for _, row := range rows {
				if row.ID == *product.CategoryID {
					detail.Category = &Category{ID: row.ID, ParentID: row.ParentID, Name: row.Name, Slug: row.Slug, Description: row.Description}
				}
			}
		}
		return nil
	})
	return detail, err
}
