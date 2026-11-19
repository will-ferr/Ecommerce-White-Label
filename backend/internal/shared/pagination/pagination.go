// Package pagination implements opaque cursors for keyset pagination, which
// stays fast on large tables and does not skip rows when data changes.
package pagination

import (
	"encoding/base64"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/shared/apperr"
)

// DefaultLimit and MaxLimit bound the size of a page.
const (
	DefaultLimit = 24
	MaxLimit     = 100
)

// Cursor points to the last row of the previous page.
type Cursor struct {
	At time.Time `json:"t"`
	ID uuid.UUID `json:"i"`
}

// Encode returns the opaque form of the cursor.
func (c Cursor) Encode() string {
	raw, err := json.Marshal(c)
	if err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

// Decode parses a cursor received from a client. An empty value means the first page.
func Decode(raw string) (*Cursor, error) {
	if raw == "" {
		return nil, nil
	}
	invalid := apperr.New(apperr.KindInvalid, "INVALID_CURSOR", "The page cursor is not valid.")
	if len(raw) > 200 {
		return nil, invalid
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return nil, invalid
	}
	var cursor Cursor
	if err := json.Unmarshal(decoded, &cursor); err != nil || cursor.ID == uuid.Nil {
		return nil, invalid
	}
	return &cursor, nil
}

// Page is a slice of results and the cursor of the next page.
type Page[T any] struct {
	Items      []T    `json:"items"`
	NextCursor string `json:"nextCursor,omitempty"`
}
