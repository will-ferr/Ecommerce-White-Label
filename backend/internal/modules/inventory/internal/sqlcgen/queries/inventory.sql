-- name: CreateDefaultWarehouse :exec
INSERT INTO inventory.warehouses (tenant_id, id, name, is_default)
VALUES ($1, $2, 'Depósito principal', true)
ON CONFLICT (tenant_id) WHERE is_default DO NOTHING;

-- name: GetDefaultWarehouse :one
SELECT id FROM inventory.warehouses WHERE tenant_id = $1 AND is_default;

-- name: EnsureStockLevel :exec
INSERT INTO inventory.stock_levels (tenant_id, variant_id, warehouse_id)
VALUES ($1, $2, $3)
ON CONFLICT (tenant_id, variant_id, warehouse_id) DO NOTHING;

-- name: AdjustOnHand :one
UPDATE inventory.stock_levels
SET on_hand = on_hand + sqlc.arg('delta')
WHERE tenant_id = $1 AND variant_id = $2 AND warehouse_id = $3
  AND on_hand + sqlc.arg('delta') >= reserved
RETURNING on_hand;

-- name: SetMinQuantity :exec
UPDATE inventory.stock_levels SET min_quantity = $4
WHERE tenant_id = $1 AND variant_id = $2 AND warehouse_id = $3;

-- name: Reserve :execrows
UPDATE inventory.stock_levels
SET reserved = reserved + sqlc.arg('quantity')
WHERE tenant_id = $1 AND variant_id = $2 AND warehouse_id = $3
  AND on_hand - reserved >= sqlc.arg('quantity');

-- name: InsertReservation :exec
INSERT INTO inventory.reservations (tenant_id, id, order_id, variant_id, warehouse_id, quantity, status, expires_at)
VALUES ($1, $2, $3, $4, $5, $6, 'active', $7);

-- name: LockOrderReservations :many
SELECT id, variant_id, warehouse_id, quantity, status
FROM inventory.reservations
WHERE tenant_id = $1 AND order_id = $2
ORDER BY variant_id
FOR UPDATE;

-- name: SetReservationStatus :exec
UPDATE inventory.reservations SET status = $3 WHERE tenant_id = $1 AND id = $2;

-- name: CommitStock :one
UPDATE inventory.stock_levels
SET on_hand = on_hand - sqlc.arg('quantity'), reserved = reserved - sqlc.arg('quantity')
WHERE tenant_id = $1 AND variant_id = $2 AND warehouse_id = $3
RETURNING on_hand;

-- name: ReleaseStock :exec
UPDATE inventory.stock_levels SET reserved = reserved - sqlc.arg('quantity')
WHERE tenant_id = $1 AND variant_id = $2 AND warehouse_id = $3;

-- name: RestockStock :one
UPDATE inventory.stock_levels SET on_hand = on_hand + sqlc.arg('quantity')
WHERE tenant_id = $1 AND variant_id = $2 AND warehouse_id = $3
RETURNING on_hand;

-- name: InsertMovement :exec
INSERT INTO inventory.movements (tenant_id, id, variant_id, warehouse_id, kind, quantity, on_hand_after, reference, actor)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9);

-- name: StockByVariant :many
SELECT variant_id, sum(on_hand)::integer AS on_hand, sum(reserved)::integer AS reserved,
       max(min_quantity)::integer AS min_quantity
FROM inventory.stock_levels
WHERE tenant_id = $1 AND variant_id = ANY(sqlc.arg('variant_ids')::uuid[])
GROUP BY variant_id;

-- name: ListMovements :many
SELECT id, kind, quantity, on_hand_after, reference, actor, created_at
FROM inventory.movements
WHERE tenant_id = $1 AND variant_id = $2
ORDER BY created_at DESC
LIMIT 50;
