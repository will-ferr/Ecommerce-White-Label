-- name: ListCategories :many
SELECT id, parent_id, name, slug, description, position
FROM catalog.categories
WHERE tenant_id = $1
ORDER BY position, name;

-- name: GetCategoryBySlug :one
SELECT id, parent_id, name, slug, description, position
FROM catalog.categories
WHERE tenant_id = $1 AND slug = $2;

-- name: InsertCategory :exec
INSERT INTO catalog.categories (tenant_id, id, parent_id, name, slug, description, position)
VALUES ($1, $2, $3, $4, $5, $6, $7);

-- name: UpdateCategory :execrows
UPDATE catalog.categories
SET parent_id = $3, name = $4, slug = $5, description = $6, position = $7, updated_at = now()
WHERE tenant_id = $1 AND id = $2;

-- name: CategoryInUse :one
SELECT (EXISTS (SELECT 1 FROM catalog.products p WHERE p.tenant_id = $1 AND p.category_id = $2)
    OR EXISTS (SELECT 1 FROM catalog.categories c WHERE c.tenant_id = $1 AND c.parent_id = $2))::boolean AS in_use;

-- name: DeleteCategory :execrows
DELETE FROM catalog.categories WHERE tenant_id = $1 AND id = $2;

-- name: InsertProduct :exec
INSERT INTO catalog.products (
  tenant_id, id, category_id, name, slug, description, brand, status, featured, seo_title, seo_description
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11);

-- name: UpdateProduct :execrows
UPDATE catalog.products
SET category_id = $3, name = $4, slug = $5, description = $6, brand = $7, status = $8, featured = $9,
    seo_title = $10, seo_description = $11, version = version + 1, updated_at = now()
WHERE tenant_id = $1 AND id = $2 AND version = $12;

-- name: GetProduct :one
SELECT * FROM catalog.products WHERE tenant_id = $1 AND id = $2;

-- name: GetActiveProductBySlug :one
SELECT * FROM catalog.products WHERE tenant_id = $1 AND slug = $2 AND status = 'active';

-- name: ListProducts :many
SELECT *
FROM catalog.products p
WHERE p.tenant_id = $1
  AND (sqlc.narg('status')::text IS NULL OR p.status = sqlc.narg('status'))
  AND (sqlc.narg('category_id')::uuid IS NULL OR p.category_id = sqlc.narg('category_id'))
  AND (sqlc.narg('featured')::boolean IS NULL OR p.featured = sqlc.narg('featured'))
  AND (sqlc.narg('search')::text IS NULL OR p.name ILIKE '%' || sqlc.narg('search') || '%')
  AND (sqlc.narg('after_at')::timestamptz IS NULL
       OR (p.created_at, p.id) < (sqlc.narg('after_at')::timestamptz, sqlc.narg('after_id')::uuid))
ORDER BY p.created_at DESC, p.id DESC
LIMIT sqlc.arg('row_limit');

-- name: ListImages :many
SELECT product_id, url, alt
FROM catalog.product_images
WHERE tenant_id = $1 AND product_id = ANY(sqlc.arg('product_ids')::uuid[])
ORDER BY product_id, position;

-- name: DeleteImages :exec
DELETE FROM catalog.product_images WHERE tenant_id = $1 AND product_id = $2;

-- name: InsertImage :exec
INSERT INTO catalog.product_images (tenant_id, id, product_id, url, alt, position) VALUES ($1, $2, $3, $4, $5, $6);

-- name: ListVariants :many
SELECT *
FROM catalog.variants
WHERE tenant_id = $1 AND product_id = ANY(sqlc.arg('product_ids')::uuid[])
  AND (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status'))
ORDER BY product_id, position;

-- name: InsertVariant :exec
INSERT INTO catalog.variants (
  tenant_id, id, product_id, sku, gtin, options, weight_grams, price_amount, compare_at_amount, cost_amount, position, status
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, 'active');

-- name: UpdateVariant :execrows
UPDATE catalog.variants
SET sku = $4, gtin = $5, options = $6, weight_grams = $7, price_amount = $8, compare_at_amount = $9,
    cost_amount = $10, position = $11, status = 'active'
WHERE tenant_id = $1 AND id = $2 AND product_id = $3;

-- name: ArchiveMissingVariants :exec
UPDATE catalog.variants SET status = 'archived'
WHERE tenant_id = $1 AND product_id = $2 AND NOT (id = ANY(sqlc.arg('kept_ids')::uuid[]));

-- name: InsertPriceHistory :exec
INSERT INTO catalog.price_history (tenant_id, id, variant_id, price_amount, compare_at_amount, cost_amount, changed_by)
VALUES ($1, $2, $3, $4, $5, $6, $7);

-- name: VariantsForSale :many
SELECT v.id, v.product_id, v.sku, v.options, v.price_amount, v.weight_grams, p.name AS product_name, p.slug AS product_slug
FROM catalog.variants v
JOIN catalog.products p ON p.tenant_id = v.tenant_id AND p.id = v.product_id
WHERE v.tenant_id = $1 AND v.id = ANY(sqlc.arg('variant_ids')::uuid[])
  AND v.status = 'active' AND p.status = 'active';

-- name: VariantExists :one
SELECT EXISTS (SELECT 1 FROM catalog.variants WHERE tenant_id = $1 AND id = $2);
