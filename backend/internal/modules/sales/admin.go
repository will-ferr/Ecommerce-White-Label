package sales

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/modules/sales/internal/domain"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/modules/sales/internal/sqlcgen"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/authz"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/shared/apperr"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/shared/br"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/shared/pagination"
)

// AdminOrders lists the orders of the store.
func (s *Service) AdminOrders(ctx context.Context, filter OrderFilter) (pagination.Page[OrderView], error) {
	filter.CustomerUserID = nil
	return s.listOrders(ctx, filter)
}

// AdminOrder returns an order with its history, notes and possible next steps.
func (s *Service) AdminOrder(ctx context.Context, id uuid.UUID) (OrderView, error) {
	var view OrderView
	err := s.in(ctx, func(_ pgx.Tx, q *sqlcgen.Queries, tenantID uuid.UUID) error {
		order, err := q.GetOrder(ctx, sqlcgen.GetOrderParams{TenantID: tenantID, ID: id})
		if errors.Is(err, pgx.ErrNoRows) {
			return orderNotFound()
		}
		if err != nil {
			return err
		}
		view, err = s.detail(ctx, q, order)
		if err != nil {
			return err
		}
		view.TestPayment = false
		view.NextStatuses = domain.Next(order.Status)
		notes, err := q.ListOrderNotes(ctx, sqlcgen.ListOrderNotesParams{TenantID: tenantID, OrderID: order.ID})
		for _, note := range notes {
			view.Notes = append(view.Notes, Note{AuthorID: note.AuthorID, Body: note.Body, CreatedAt: note.CreatedAt})
		}
		return err
	})
	return view, err
}

// StatusChange is a decision of the store team about an order.
type StatusChange struct {
	Status       string `json:"status"`
	TrackingCode string `json:"trackingCode"`
	Note         string `json:"note"`
}

// ChangeStatus moves an order forward, or cancels it with its side effects.
func (s *Service) ChangeStatus(ctx context.Context, principal authz.Principal, id uuid.UUID, change StatusChange) error {
	change.TrackingCode, change.Note = strings.TrimSpace(change.TrackingCode), strings.TrimSpace(change.Note)
	if utf8.RuneCountInString(change.TrackingCode) > 60 || utf8.RuneCountInString(change.Note) > 500 {
		return apperr.Validation(apperr.FieldError{Field: "note", Code: "too_long", Message: "The text is too long."})
	}
	return s.in(ctx, func(tx pgx.Tx, q *sqlcgen.Queries, tenantID uuid.UUID) error {
		order, err := q.LockOrder(ctx, sqlcgen.LockOrderParams{TenantID: tenantID, ID: id})
		if errors.Is(err, pgx.ErrNoRows) {
			return orderNotFound()
		}
		if err != nil {
			return err
		}
		if !domain.StaffCanSet(order.Status, change.Status) {
			return apperr.New(apperr.KindConflict, "INVALID_STATUS_CHANGE", "This status change is not allowed.")
		}
		by := actor{kind: "user", id: principal.UserID.String()}
		if change.Status == domain.Canceled {
			reason := change.Note
			if reason == "" {
				reason = "Cancelado pela loja"
			}
			err = s.cancel(ctx, tx, q, order, reason, by)
		} else {
			err = s.changeStatus(ctx, q, order, change.Status, change.TrackingCode, change.Note, by)
		}
		if err != nil {
			return err
		}
		return userAudit(ctx, tx, principal, "orders.order.status_changed", "order", strconv.FormatInt(order.Number, 10),
			map[string]string{"from": order.Status, "to": change.Status})
	})
}

// AddNote stores an internal comment about an order.
func (s *Service) AddNote(ctx context.Context, principal authz.Principal, id uuid.UUID, body string) error {
	body = strings.TrimSpace(body)
	if body == "" || utf8.RuneCountInString(body) > 2000 {
		return apperr.Validation(apperr.FieldError{Field: "body", Code: "invalid", Message: "Write a note with up to 2000 characters."})
	}
	return s.in(ctx, func(_ pgx.Tx, q *sqlcgen.Queries, tenantID uuid.UUID) error {
		if _, err := q.GetOrder(ctx, sqlcgen.GetOrderParams{TenantID: tenantID, ID: id}); errors.Is(err, pgx.ErrNoRows) {
			return orderNotFound()
		} else if err != nil {
			return err
		}
		return q.InsertOrderNote(ctx, sqlcgen.InsertOrderNoteParams{TenantID: tenantID, ID: newID(), OrderID: id, AuthorID: principal.UserID, Body: body})
	})
}

// csvSafe neutralizes values that a spreadsheet would run as a formula.
func csvSafe(value string) string {
	if value != "" && strings.ContainsRune("=+-@\t\r", rune(value[0])) {
		return "'" + value
	}
	return value
}

func money(amount int64) string {
	return fmt.Sprintf("%d.%02d", amount/100, amount%100)
}

// ExportOrders writes the orders matching the filter as CSV, up to 5000 rows.
func (s *Service) ExportOrders(ctx context.Context, filter OrderFilter, out io.Writer) error {
	writer := csv.NewWriter(out)
	if err := writer.Write([]string{"numero", "data", "status", "cliente", "email", "cidade", "uf", "itens", "subtotal", "frete", "total", "moeda"}); err != nil {
		return err
	}
	filter.Limit = pagination.MaxLimit
	for range 50 {
		page, err := s.AdminOrders(ctx, filter)
		if err != nil {
			return err
		}
		for _, order := range page.Items {
			var items int32
			for _, item := range order.Items {
				items += item.Quantity
			}
			if err := writer.Write([]string{
				strconv.FormatInt(order.Number, 10), order.CreatedAt.Format("2006-01-02 15:04"), order.Status,
				csvSafe(order.CustomerName), csvSafe(order.CustomerEmail), csvSafe(order.ShippingAddress.City),
				order.ShippingAddress.State, strconv.Itoa(int(items)), money(order.Subtotal), money(order.ShippingAmount),
				money(order.Total), order.Currency,
			}); err != nil {
				return err
			}
		}
		if page.NextCursor == "" {
			break
		}
		filter.Cursor = page.NextCursor
	}
	writer.Flush()
	return writer.Error()
}

// ShippingInput configures a delivery option.
type ShippingInput struct {
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	Price   int64  `json:"price"`
	MinDays int32  `json:"minDays"`
	MaxDays int32  `json:"maxDays"`
	Active  bool   `json:"active"`
}

func (in ShippingInput) normalize() (ShippingInput, error) {
	in.Name = strings.TrimSpace(in.Name)
	var fields []apperr.FieldError
	if length := utf8.RuneCountInString(in.Name); length == 0 || length > 80 {
		fields = append(fields, apperr.FieldError{Field: "name", Code: "invalid", Message: "Enter a name with up to 80 characters."})
	}
	if !slices.Contains([]string{"pickup", "flat"}, in.Kind) {
		fields = append(fields, apperr.FieldError{Field: "kind", Code: "invalid", Message: "Choose a valid kind."})
	}
	if in.Price < 0 || in.Price > 1_000_000_00 || (in.Kind == "pickup" && in.Price != 0) {
		fields = append(fields, apperr.FieldError{Field: "price", Code: "invalid", Message: "Enter a valid price."})
	}
	if in.MinDays < 0 || in.MaxDays < in.MinDays || in.MaxDays > 120 {
		fields = append(fields, apperr.FieldError{Field: "maxDays", Code: "invalid", Message: "Enter a valid delivery time."})
	}
	if len(fields) > 0 {
		return ShippingInput{}, apperr.Validation(fields...)
	}
	return in, nil
}

// ShippingMethods lists every delivery option of the store.
func (s *Service) ShippingMethods(ctx context.Context) ([]sqlcgen.ShippingMethod, error) {
	var methods []sqlcgen.ShippingMethod
	err := s.in(ctx, func(_ pgx.Tx, q *sqlcgen.Queries, tenantID uuid.UUID) error {
		var err error
		methods, err = q.ListShippingMethods(ctx, sqlcgen.ListShippingMethodsParams{TenantID: tenantID})
		return err
	})
	return methods, err
}

// SaveShippingMethod creates a delivery option, or changes it when id is given.
func (s *Service) SaveShippingMethod(ctx context.Context, principal authz.Principal, id *uuid.UUID, input ShippingInput) (uuid.UUID, error) {
	input, err := input.normalize()
	if err != nil {
		return uuid.Nil, err
	}
	methodID := newID()
	if id != nil {
		methodID = *id
	}
	err = s.in(ctx, func(tx pgx.Tx, q *sqlcgen.Queries, tenantID uuid.UUID) error {
		if id == nil {
			err = q.InsertShippingMethod(ctx, sqlcgen.InsertShippingMethodParams{
				TenantID: tenantID, ID: methodID, Name: input.Name, Kind: input.Kind, PriceAmount: input.Price,
				MinDays: input.MinDays, MaxDays: input.MaxDays, Active: input.Active,
			})
		} else {
			var affected int64
			affected, err = q.UpdateShippingMethod(ctx, sqlcgen.UpdateShippingMethodParams{
				TenantID: tenantID, ID: methodID, Name: input.Name, Kind: input.Kind, PriceAmount: input.Price,
				MinDays: input.MinDays, MaxDays: input.MaxDays, Active: input.Active,
			})
			if err == nil && affected == 0 {
				return apperr.New(apperr.KindNotFound, "SHIPPING_METHOD_NOT_FOUND", "The delivery option does not exist.")
			}
		}
		if err != nil {
			return err
		}
		return userAudit(ctx, tx, principal, "shipping.method.saved", "shipping_method", methodID.String(), nil)
	})
	return methodID, err
}

// SavedAddress is an address of the customer address book.
type SavedAddress struct {
	ID        uuid.UUID `json:"id"`
	Label     string    `json:"label"`
	IsDefault bool      `json:"isDefault"`
	br.Address
}

// Addresses lists the address book of a customer.
func (s *Service) Addresses(ctx context.Context, principal authz.Principal) ([]SavedAddress, error) {
	addresses := []SavedAddress{}
	err := s.in(ctx, func(_ pgx.Tx, q *sqlcgen.Queries, tenantID uuid.UUID) error {
		rows, err := q.ListAddresses(ctx, sqlcgen.ListAddressesParams{TenantID: tenantID, UserID: principal.UserID})
		for _, row := range rows {
			addresses = append(addresses, SavedAddress{
				ID: row.ID, Label: row.Label, IsDefault: row.IsDefault,
				Address: br.Address{Recipient: row.Recipient, PostalCode: row.PostalCode, Street: row.Street, Number: row.Number,
					Complement: row.Complement, District: row.District, City: row.City, State: row.State},
			})
		}
		return err
	})
	return addresses, err
}

// AddressInput is a new entry of the address book.
type AddressInput struct {
	Label     string `json:"label"`
	IsDefault bool   `json:"isDefault"`
	br.Address
}

// AddAddress saves an address. The first one becomes the default.
func (s *Service) AddAddress(ctx context.Context, principal authz.Principal, input AddressInput) (uuid.UUID, error) {
	address, err := input.Normalize()
	if err != nil {
		return uuid.Nil, err
	}
	label := strings.TrimSpace(input.Label)
	if utf8.RuneCountInString(label) > 40 {
		return uuid.Nil, apperr.Validation(apperr.FieldError{Field: "label", Code: "invalid", Message: "Use up to 40 characters."})
	}
	id := newID()
	err = s.in(ctx, func(_ pgx.Tx, q *sqlcgen.Queries, tenantID uuid.UUID) error {
		count, err := q.CountAddresses(ctx, sqlcgen.CountAddressesParams{TenantID: tenantID, UserID: principal.UserID})
		if err != nil {
			return err
		}
		if count >= 20 {
			return apperr.New(apperr.KindUnprocessable, "ADDRESS_LIMIT", "You can save up to 20 addresses.")
		}
		isDefault := input.IsDefault || count == 0
		if isDefault {
			if err := q.ClearDefaultAddress(ctx, sqlcgen.ClearDefaultAddressParams{TenantID: tenantID, UserID: principal.UserID}); err != nil {
				return err
			}
		}
		return q.InsertAddress(ctx, sqlcgen.InsertAddressParams{
			TenantID: tenantID, ID: id, UserID: principal.UserID, Label: label, Recipient: address.Recipient,
			PostalCode: address.PostalCode, Street: address.Street, Number: address.Number, Complement: address.Complement,
			District: address.District, City: address.City, State: address.State, IsDefault: isDefault,
		})
	})
	return id, err
}

// DeleteAddress removes an address of the caller. Addresses of others look nonexistent.
func (s *Service) DeleteAddress(ctx context.Context, principal authz.Principal, id uuid.UUID) error {
	return s.in(ctx, func(_ pgx.Tx, q *sqlcgen.Queries, tenantID uuid.UUID) error {
		affected, err := q.DeleteAddress(ctx, sqlcgen.DeleteAddressParams{TenantID: tenantID, UserID: principal.UserID, ID: id})
		if err == nil && affected == 0 {
			return apperr.New(apperr.KindNotFound, "ADDRESS_NOT_FOUND", "The address does not exist.")
		}
		return err
	})
}
