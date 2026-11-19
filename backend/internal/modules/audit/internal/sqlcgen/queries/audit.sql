-- name: InsertEntry :exec
INSERT INTO audit.entries (
  tenant_id, id, actor_type, actor_id, action, target_type, target_id, ip, trace_id, metadata
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10);

-- name: ListEntries :many
SELECT id, actor_type, actor_id, action, target_type, target_id, ip, metadata, occurred_at
FROM audit.entries
WHERE tenant_id = $1
ORDER BY occurred_at DESC, id DESC
LIMIT $2;
