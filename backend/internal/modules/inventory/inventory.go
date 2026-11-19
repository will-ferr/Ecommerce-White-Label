// Package inventory controls stock levels, reservations and their history.
// Other modules call these functions inside their own transaction, so a
// reservation and the order that needs it are committed together.
package inventory

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/modules/inventory/internal/sqlcgen"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/shared/apperr"
)

// Line is a quantity of one variant.
type Line struct {
	VariantID uuid.UUID
	Quantity  int32
}

// Stock is the situation of one variant across warehouses.
type Stock struct {
	OnHand      int32 `json:"onHand"`
	Reserved    int32 `json:"reserved"`
	Available   int32 `json:"available"`
	MinQuantity int32 `json:"minQuantity"`
	Low         bool  `json:"low"`
}

// OutOfStockError lists the variants that could not be reserved.
type OutOfStockError struct {
	VariantIDs []uuid.UUID
}

func (e *OutOfStockError) Error() string { return "insufficient stock" }

// Unwrap maps the failure to the API error format.
func (e *OutOfStockError) Unwrap() error {
	return apperr.New(apperr.KindConflict, "OUT_OF_STOCK", "Some items are no longer available in the requested quantity.")
}

func newID() uuid.UUID {
	id, err := uuid.NewV7()
	if err != nil {
		return uuid.New()
	}
	return id
}

func defaultWarehouse(ctx context.Context, q *sqlcgen.Queries, tenantID uuid.UUID) (uuid.UUID, error) {
	if err := q.CreateDefaultWarehouse(ctx, sqlcgen.CreateDefaultWarehouseParams{TenantID: tenantID, ID: newID()}); err != nil {
		return uuid.Nil, fmt.Errorf("create default warehouse: %w", err)
	}
	id, err := q.GetDefaultWarehouse(ctx, tenantID)
	if err != nil {
		return uuid.Nil, fmt.Errorf("get default warehouse: %w", err)
	}
	return id, nil
}

// Levels returns the stock of each variant. Variants without stock records are absent.
func Levels(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, variantIDs []uuid.UUID) (map[uuid.UUID]Stock, error) {
	rows, err := sqlcgen.New(tx).StockByVariant(ctx, sqlcgen.StockByVariantParams{TenantID: tenantID, VariantIds: variantIDs})
	if err != nil {
		return nil, fmt.Errorf("load stock: %w", err)
	}
	levels := make(map[uuid.UUID]Stock, len(rows))
	for _, row := range rows {
		available := row.OnHand - row.Reserved
		levels[row.VariantID] = Stock{
			OnHand: row.OnHand, Reserved: row.Reserved, Available: available, MinQuantity: row.MinQuantity,
			Low: row.MinQuantity > 0 && available <= row.MinQuantity,
		}
	}
	return levels, nil
}

// Reserve holds stock for an order. Each update succeeds only if enough stock
// remains, so two buyers can never take the same last unit. Variants are locked
// in a fixed order to prevent deadlocks between simultaneous checkouts.
func Reserve(ctx context.Context, tx pgx.Tx, tenantID, orderID uuid.UUID, lines []Line, expires time.Time) error {
	q := sqlcgen.New(tx)
	warehouseID, err := defaultWarehouse(ctx, q, tenantID)
	if err != nil {
		return err
	}
	sorted := slices.Clone(lines)
	slices.SortFunc(sorted, func(a, b Line) int { return slices.Compare(a.VariantID[:], b.VariantID[:]) })

	var missing []uuid.UUID
	for _, line := range sorted {
		affected, err := q.Reserve(ctx, sqlcgen.ReserveParams{
			TenantID: tenantID, VariantID: line.VariantID, WarehouseID: warehouseID, Quantity: line.Quantity,
		})
		if err != nil {
			return fmt.Errorf("reserve stock: %w", err)
		}
		if affected == 0 {
			missing = append(missing, line.VariantID)
			continue
		}
		if err := q.InsertReservation(ctx, sqlcgen.InsertReservationParams{
			TenantID: tenantID, ID: newID(), OrderID: orderID, VariantID: line.VariantID,
			WarehouseID: warehouseID, Quantity: line.Quantity, ExpiresAt: expires,
		}); err != nil {
			return fmt.Errorf("insert reservation: %w", err)
		}
	}
	if len(missing) > 0 {
		return &OutOfStockError{VariantIDs: missing}
	}
	return nil
}

func settle(ctx context.Context, tx pgx.Tx, tenantID, orderID uuid.UUID, from, to string,
	apply func(q *sqlcgen.Queries, row sqlcgen.LockOrderReservationsRow) error,
) error {
	q := sqlcgen.New(tx)
	rows, err := q.LockOrderReservations(ctx, sqlcgen.LockOrderReservationsParams{TenantID: tenantID, OrderID: orderID})
	if err != nil {
		return fmt.Errorf("lock reservations: %w", err)
	}
	for _, row := range rows {
		if row.Status != from {
			continue
		}
		if err := apply(q, row); err != nil {
			return err
		}
		if err := q.SetReservationStatus(ctx, sqlcgen.SetReservationStatusParams{TenantID: tenantID, ID: row.ID, Status: to}); err != nil {
			return fmt.Errorf("update reservation: %w", err)
		}
	}
	return nil
}

func movement(ctx context.Context, q *sqlcgen.Queries, tenantID uuid.UUID, row sqlcgen.LockOrderReservationsRow, kind string, quantity, onHand int32, reference string) error {
	err := q.InsertMovement(ctx, sqlcgen.InsertMovementParams{
		TenantID: tenantID, ID: newID(), VariantID: row.VariantID, WarehouseID: row.WarehouseID, Kind: kind,
		Quantity: quantity, OnHandAfter: onHand, Reference: reference, Actor: "system",
	})
	if err != nil {
		return fmt.Errorf("record movement: %w", err)
	}
	return nil
}

// Commit turns the reservations of a paid order into a sale.
func Commit(ctx context.Context, tx pgx.Tx, tenantID, orderID uuid.UUID, reference string) error {
	return settle(ctx, tx, tenantID, orderID, "active", "committed", func(q *sqlcgen.Queries, row sqlcgen.LockOrderReservationsRow) error {
		onHand, err := q.CommitStock(ctx, sqlcgen.CommitStockParams{
			TenantID: tenantID, VariantID: row.VariantID, WarehouseID: row.WarehouseID, Quantity: row.Quantity,
		})
		if err != nil {
			return fmt.Errorf("commit stock: %w", err)
		}
		return movement(ctx, q, tenantID, row, "sale", -row.Quantity, onHand, reference)
	})
}

// Release frees the stock held for an order that was not paid.
func Release(ctx context.Context, tx pgx.Tx, tenantID, orderID uuid.UUID) error {
	return settle(ctx, tx, tenantID, orderID, "active", "released", func(q *sqlcgen.Queries, row sqlcgen.LockOrderReservationsRow) error {
		err := q.ReleaseStock(ctx, sqlcgen.ReleaseStockParams{
			TenantID: tenantID, VariantID: row.VariantID, WarehouseID: row.WarehouseID, Quantity: row.Quantity,
		})
		if err != nil {
			return fmt.Errorf("release stock: %w", err)
		}
		return nil
	})
}

// Restock returns the items of a canceled paid order to the shelf.
func Restock(ctx context.Context, tx pgx.Tx, tenantID, orderID uuid.UUID, reference string) error {
	return settle(ctx, tx, tenantID, orderID, "committed", "released", func(q *sqlcgen.Queries, row sqlcgen.LockOrderReservationsRow) error {
		onHand, err := q.RestockStock(ctx, sqlcgen.RestockStockParams{
			TenantID: tenantID, VariantID: row.VariantID, WarehouseID: row.WarehouseID, Quantity: row.Quantity,
		})
		if err != nil {
			return fmt.Errorf("restock: %w", err)
		}
		return movement(ctx, q, tenantID, row, "return", row.Quantity, onHand, reference)
	})
}

// Adjustment is a manual change of stock made by the store team.
type Adjustment struct {
	VariantID   uuid.UUID `json:"variantId"`
	Delta       int32     `json:"delta"`
	MinQuantity *int32    `json:"minQuantity"`
	Reason      string    `json:"reason"`
}

// Adjust changes the stock on hand. Stock never goes below what is reserved.
func Adjust(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, adjustment Adjustment, actor string) (Stock, error) {
	if adjustment.Delta < -100_000 || adjustment.Delta > 100_000 || len(adjustment.Reason) > 200 {
		return Stock{}, apperr.Validation(apperr.FieldError{Field: "delta", Code: "invalid", Message: "Enter a valid quantity."})
	}
	q := sqlcgen.New(tx)
	warehouseID, err := defaultWarehouse(ctx, q, tenantID)
	if err != nil {
		return Stock{}, err
	}
	if err := q.EnsureStockLevel(ctx, sqlcgen.EnsureStockLevelParams{TenantID: tenantID, VariantID: adjustment.VariantID, WarehouseID: warehouseID}); err != nil {
		return Stock{}, fmt.Errorf("ensure stock level: %w", err)
	}
	if adjustment.MinQuantity != nil {
		if *adjustment.MinQuantity < 0 {
			return Stock{}, apperr.Validation(apperr.FieldError{Field: "minQuantity", Code: "invalid", Message: "Enter a valid minimum."})
		}
		err := q.SetMinQuantity(ctx, sqlcgen.SetMinQuantityParams{
			TenantID: tenantID, VariantID: adjustment.VariantID, WarehouseID: warehouseID, MinQuantity: *adjustment.MinQuantity,
		})
		if err != nil {
			return Stock{}, fmt.Errorf("set minimum: %w", err)
		}
	}
	if adjustment.Delta != 0 {
		onHand, err := q.AdjustOnHand(ctx, sqlcgen.AdjustOnHandParams{
			TenantID: tenantID, VariantID: adjustment.VariantID, WarehouseID: warehouseID, Delta: adjustment.Delta,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return Stock{}, apperr.New(apperr.KindConflict, "STOCK_BELOW_RESERVED", "Stock cannot be lower than the quantity reserved by open orders.")
		}
		if err != nil {
			return Stock{}, fmt.Errorf("adjust stock: %w", err)
		}
		reason := adjustment.Reason
		if reason == "" {
			reason = "Ajuste manual"
		}
		if err := q.InsertMovement(ctx, sqlcgen.InsertMovementParams{
			TenantID: tenantID, ID: newID(), VariantID: adjustment.VariantID, WarehouseID: warehouseID, Kind: "adjustment",
			Quantity: adjustment.Delta, OnHandAfter: onHand, Reference: reason, Actor: actor,
		}); err != nil {
			return Stock{}, fmt.Errorf("record movement: %w", err)
		}
	}
	levels, err := Levels(ctx, tx, tenantID, []uuid.UUID{adjustment.VariantID})
	if err != nil {
		return Stock{}, err
	}
	return levels[adjustment.VariantID], nil
}

// Movement is one entry of the stock history.
type Movement struct {
	ID          uuid.UUID `json:"id"`
	Kind        string    `json:"kind"`
	Quantity    int32     `json:"quantity"`
	OnHandAfter int32     `json:"onHandAfter"`
	Reference   string    `json:"reference"`
	Actor       string    `json:"actor"`
	CreatedAt   time.Time `json:"createdAt"`
}

// Movements returns the recent history of a variant.
func Movements(ctx context.Context, tx pgx.Tx, tenantID, variantID uuid.UUID) ([]Movement, error) {
	rows, err := sqlcgen.New(tx).ListMovements(ctx, sqlcgen.ListMovementsParams{TenantID: tenantID, VariantID: variantID})
	if err != nil {
		return nil, fmt.Errorf("list movements: %w", err)
	}
	movements := make([]Movement, 0, len(rows))
	for _, row := range rows {
		movements = append(movements, Movement{
			ID: row.ID, Kind: row.Kind, Quantity: row.Quantity, OnHandAfter: row.OnHandAfter,
			Reference: row.Reference, Actor: row.Actor, CreatedAt: row.CreatedAt,
		})
	}
	return movements, nil
}
