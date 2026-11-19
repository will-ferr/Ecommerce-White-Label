-- name: ListPages :many
SELECT * FROM content.pages WHERE tenant_id = $1 ORDER BY title, id;

-- name: ListFooterPages :many
SELECT slug, title FROM content.pages
WHERE tenant_id = $1 AND status = 'published' AND show_in_footer
ORDER BY title, id;

-- name: ListPublishedPages :many
SELECT slug, updated_at FROM content.pages WHERE tenant_id = $1 AND status = 'published' ORDER BY slug;

-- name: GetPage :one
SELECT * FROM content.pages WHERE tenant_id = $1 AND id = $2;

-- name: GetPublishedPage :one
SELECT * FROM content.pages WHERE tenant_id = $1 AND slug = $2 AND status = 'published';

-- name: InsertPage :exec
INSERT INTO content.pages (tenant_id, id, slug, title, body, seo_description, status, show_in_footer)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8);

-- name: UpdatePage :execrows
UPDATE content.pages
SET slug = $3, title = $4, body = $5, seo_description = $6, status = $7, show_in_footer = $8,
    version = version + 1, updated_at = now()
WHERE tenant_id = $1 AND id = $2 AND version = sqlc.arg('version');

-- name: DeletePage :execrows
DELETE FROM content.pages WHERE tenant_id = $1 AND id = $2;
