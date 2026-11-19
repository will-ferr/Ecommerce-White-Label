-- Shipping methods.

-- name: ListShippingMethods :many
SELECT * FROM shipping.methods
WHERE tenant_id = $1 AND (sqlc.narg('active')::boolean IS NULL OR active = sqlc.narg('active'))
ORDER BY price_amount, name;

-- name: GetShippingMethod :one
SELECT * FROM shipping.methods WHERE tenant_id = $1 AND id = $2;

-- name: InsertShippingMethod :exec
INSERT INTO shipping.methods (tenant_id, id, name, kind, price_amount, min_days, max_days, active)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8);

-- name: UpdateShippingMethod :execrows
UPDATE shipping.methods SET name = $3, kind = $4, price_amount = $5, min_days = $6, max_days = $7, active = $8
WHERE tenant_id = $1 AND id = $2;

-- Carts.

-- name: GetActiveCartByToken :one
SELECT * FROM cart.carts WHERE tenant_id = $1 AND token_hash = $2 AND status = 'active';

-- name: GetActiveCartByUser :one
SELECT * FROM cart.carts WHERE tenant_id = $1 AND user_id = $2 AND status = 'active';

-- name: InsertCart :exec
INSERT INTO cart.carts (tenant_id, id, token_hash, user_id, status) VALUES ($1, $2, $3, $4, 'active');

-- name: LockCart :one
SELECT * FROM cart.carts WHERE tenant_id = $1 AND id = $2 AND status = 'active' FOR UPDATE;

-- name: SetCartStatus :exec
UPDATE cart.carts SET status = $3, updated_at = now() WHERE tenant_id = $1 AND id = $2;

-- name: ListCartItems :many
SELECT variant_id, quantity FROM cart.items WHERE tenant_id = $1 AND cart_id = $2 ORDER BY added_at, variant_id;

-- name: AddCartItem :exec
INSERT INTO cart.items (tenant_id, cart_id, variant_id, quantity) VALUES ($1, $2, $3, sqlc.arg('quantity'))
ON CONFLICT (tenant_id, cart_id, variant_id)
DO UPDATE SET quantity = least(cart.items.quantity + EXCLUDED.quantity, 99);

-- name: SetCartItem :execrows
UPDATE cart.items SET quantity = $4 WHERE tenant_id = $1 AND cart_id = $2 AND variant_id = $3;

-- name: DeleteCartItem :exec
DELETE FROM cart.items WHERE tenant_id = $1 AND cart_id = $2 AND variant_id = $3;

-- name: CountCartLines :one
SELECT count(*)::integer FROM cart.items WHERE tenant_id = $1 AND cart_id = $2;

-- name: TouchCart :exec
UPDATE cart.carts SET updated_at = now() WHERE tenant_id = $1 AND id = $2;

-- Orders.

-- name: NextOrderNumber :one
INSERT INTO orders.counters (tenant_id, next_number) VALUES ($1, 1001)
ON CONFLICT (tenant_id) DO UPDATE SET next_number = orders.counters.next_number + 1
RETURNING next_number;

-- name: InsertOrder :exec
INSERT INTO orders.orders (
  tenant_id, id, number, status, customer_user_id, customer_name, customer_email, shipping_address,
  shipping_method, shipping_amount, subtotal_amount, discount_amount, tax_amount, total_amount, currency, payment_due_at
) VALUES ($1, $2, $3, 'pending_payment', $4, $5, $6, $7, $8, $9, $10, 0, 0, $11, $12, $13);

-- name: InsertOrderItem :exec
INSERT INTO orders.items (tenant_id, id, order_id, variant_id, product_name, variant_label, sku, quantity, unit_amount, total_amount)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10);

-- name: InsertStatusHistory :exec
INSERT INTO orders.status_history (tenant_id, id, order_id, from_status, to_status, actor_type, actor_id, note)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8);

-- name: GetOrder :one
SELECT * FROM orders.orders WHERE tenant_id = $1 AND id = $2;

-- name: GetOrderByNumber :one
SELECT * FROM orders.orders WHERE tenant_id = $1 AND number = $2;

-- name: LockOrder :one
SELECT * FROM orders.orders WHERE tenant_id = $1 AND id = $2 FOR UPDATE;

-- name: UpdateOrderStatus :exec
UPDATE orders.orders
SET status = $3, tracking_code = $4, cancel_reason = $5, version = version + 1, updated_at = now()
WHERE tenant_id = $1 AND id = $2;

-- name: ListOrders :many
SELECT *
FROM orders.orders o
WHERE o.tenant_id = $1
  AND (sqlc.narg('status')::text IS NULL OR o.status = sqlc.narg('status'))
  AND (sqlc.narg('customer_user_id')::uuid IS NULL OR o.customer_user_id = sqlc.narg('customer_user_id'))
  AND (sqlc.narg('search')::text IS NULL
       OR o.customer_email ILIKE '%' || sqlc.narg('search') || '%'
       OR o.number::text = sqlc.narg('search'))
  AND (sqlc.narg('after_at')::timestamptz IS NULL
       OR (o.created_at, o.id) < (sqlc.narg('after_at')::timestamptz, sqlc.narg('after_id')::uuid))
ORDER BY o.created_at DESC, o.id DESC
LIMIT sqlc.arg('row_limit');

-- name: ListOrderItems :many
SELECT * FROM orders.items WHERE tenant_id = $1 AND order_id = ANY(sqlc.arg('order_ids')::uuid[]) ORDER BY order_id, id;

-- name: ListStatusHistory :many
SELECT * FROM orders.status_history WHERE tenant_id = $1 AND order_id = $2 ORDER BY created_at, id;

-- name: InsertOrderNote :exec
INSERT INTO orders.notes (tenant_id, id, order_id, author_id, body) VALUES ($1, $2, $3, $4, $5);

-- name: ListOrderNotes :many
SELECT * FROM orders.notes WHERE tenant_id = $1 AND order_id = $2 ORDER BY created_at, id;

-- Payments.

-- name: InsertPayment :exec
INSERT INTO payments.payments (tenant_id, id, order_id, provider, method, status, amount, currency)
VALUES ($1, $2, $3, $4, $5, 'pending', $6, $7);

-- name: GetPaymentByOrder :one
SELECT * FROM payments.payments WHERE tenant_id = $1 AND order_id = $2;

-- name: LockPaymentByOrder :one
SELECT * FROM payments.payments WHERE tenant_id = $1 AND order_id = $2 FOR UPDATE;

-- name: UpdatePayment :exec
UPDATE payments.payments SET status = $3, provider_reference = $4, updated_at = now() WHERE tenant_id = $1 AND id = $2;

-- name: InsertPaymentTransaction :exec
INSERT INTO payments.transactions (tenant_id, id, payment_id, kind, amount, provider_reference)
VALUES ($1, $2, $3, $4, $5, $6);

-- Idempotency.

-- name: ClaimIdempotencyKey :execrows
INSERT INTO checkout.idempotency_keys (tenant_id, scope, key, request_hash) VALUES ($1, $2, $3, $4)
ON CONFLICT (tenant_id, scope, key) DO NOTHING;

-- name: GetIdempotencyKey :one
SELECT request_hash, response FROM checkout.idempotency_keys WHERE tenant_id = $1 AND scope = $2 AND key = $3;

-- name: SaveIdempotencyResponse :exec
UPDATE checkout.idempotency_keys SET response = $4 WHERE tenant_id = $1 AND scope = $2 AND key = $3;

-- Addresses.

-- name: ListAddresses :many
SELECT * FROM customers.addresses WHERE tenant_id = $1 AND user_id = $2 ORDER BY is_default DESC, created_at;

-- name: InsertAddress :exec
INSERT INTO customers.addresses (
  tenant_id, id, user_id, label, recipient, postal_code, street, number, complement, district, city, state, is_default
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13);

-- name: ClearDefaultAddress :exec
UPDATE customers.addresses SET is_default = false WHERE tenant_id = $1 AND user_id = $2;

-- name: DeleteAddress :execrows
DELETE FROM customers.addresses WHERE tenant_id = $1 AND user_id = $2 AND id = $3;

-- name: CountAddresses :one
SELECT count(*)::integer FROM customers.addresses WHERE tenant_id = $1 AND user_id = $2;
