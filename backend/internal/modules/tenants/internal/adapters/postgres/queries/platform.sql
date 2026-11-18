-- name: GetPlanIDByCode :one
SELECT id FROM core.plans WHERE code = $1;

-- name: InsertTenant :exec
INSERT INTO core.tenants (id, slug, plan_id) VALUES ($1, $2, $3);

-- name: InsertDomain :exec
INSERT INTO core.tenant_domains (host, tenant_id, kind, verification_token, verified_at)
VALUES ($1, $2, $3, $4, $5);

-- name: InsertSettings :exec
INSERT INTO core.tenant_settings (
  tenant_id, store_name, currency, locale, timezone, seo_title, seo_description, seo_indexable
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8);

-- name: EnablePlanFeatures :exec
INSERT INTO core.tenant_features (tenant_id, feature_key, enabled)
SELECT $1, feature_key, true FROM core.plan_features WHERE plan_id = $2;

-- name: InsertThemeVersion :exec
INSERT INTO core.theme_versions (tenant_id, id, version, status, tokens)
VALUES ($1, $2, $3, 'published', $4);

-- name: LockTenantBySlug :one
SELECT t.id, t.plan_id
FROM core.tenants t
WHERE t.slug = $1
FOR UPDATE;

-- name: TenantExists :one
SELECT EXISTS (SELECT 1 FROM core.tenants WHERE slug = $1);

-- name: ListTenants :many
SELECT t.slug, t.status, p.code AS plan_code, s.store_name
FROM core.tenants t
JOIN core.plans p ON p.id = t.plan_id
JOIN core.tenant_settings s ON s.tenant_id = t.id
ORDER BY t.slug;

-- name: SetTenantStatus :execrows
UPDATE core.tenants SET status = $2, updated_at = now() WHERE slug = $1;

-- name: GetDomain :one
SELECT host, tenant_id, kind, verification_token, verified_at
FROM core.tenant_domains
WHERE host = $1;

-- name: MarkDomainVerified :exec
UPDATE core.tenant_domains
SET verified_at = now(), verification_token = NULL
WHERE host = $1;

-- name: DeleteCustomDomain :execrows
DELETE FROM core.tenant_domains WHERE host = $1 AND kind = 'custom';

-- name: GetSettingsForUpdate :one
SELECT store_name, currency, locale, timezone, seo_title, seo_description, seo_indexable
FROM core.tenant_settings
WHERE tenant_id = $1
FOR UPDATE;

-- name: UpdateSettings :exec
UPDATE core.tenant_settings
SET store_name = $2,
    currency = $3,
    locale = $4,
    timezone = $5,
    seo_title = $6,
    seo_description = $7,
    seo_indexable = $8,
    updated_at = now()
WHERE tenant_id = $1;

-- name: PlanIncludesFeature :one
SELECT EXISTS (
  SELECT 1 FROM core.plan_features WHERE plan_id = $1 AND feature_key = $2
);

-- name: UpsertTenantFeature :exec
INSERT INTO core.tenant_features (tenant_id, feature_key, enabled)
VALUES ($1, $2, $3)
ON CONFLICT (tenant_id, feature_key) DO UPDATE SET enabled = EXCLUDED.enabled;

-- name: NextThemeVersion :one
SELECT (COALESCE(MAX(version), 0) + 1)::integer AS next_version
FROM core.theme_versions
WHERE tenant_id = $1;

-- name: ArchivePublishedTheme :exec
UPDATE core.theme_versions SET status = 'archived'
WHERE tenant_id = $1 AND status = 'published';

-- name: RepublishThemeVersion :execrows
UPDATE core.theme_versions SET status = 'published'
WHERE tenant_id = $1 AND version = $2;
