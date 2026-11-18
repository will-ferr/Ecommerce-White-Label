-- name: GetTenantSlug :one
SELECT slug FROM core.tenants WHERE id = $1;

-- name: GetSettings :one
SELECT store_name, currency, locale, timezone, seo_title, seo_description, seo_indexable
FROM core.tenant_settings
WHERE tenant_id = $1;

-- name: GetPublishedTheme :one
SELECT version, tokens
FROM core.theme_versions
WHERE tenant_id = $1 AND status = 'published';

-- name: ListFeatureStates :many
SELECT
  f.key,
  f.enabled AS globally_enabled,
  (pf.feature_key IS NOT NULL)::boolean AS in_plan,
  COALESCE(tf.enabled, false)::boolean AS tenant_enabled
FROM core.feature_flags f
JOIN core.tenants t ON t.id = $1
LEFT JOIN core.plan_features pf ON pf.plan_id = t.plan_id AND pf.feature_key = f.key
LEFT JOIN core.tenant_features tf ON tf.tenant_id = t.id AND tf.feature_key = f.key
ORDER BY f.key;
