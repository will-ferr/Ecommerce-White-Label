package httpx

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/shared/apperr"
)

// DecodeJSON reads a single JSON object and refuses unknown fields, so a client
// can never set an attribute the endpoint does not explicitly accept.
func DecodeJSON(r *http.Request, target any) error {
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	invalid := apperr.New(apperr.KindInvalid, "INVALID_BODY", "The request body is not valid.")
	if err := decoder.Decode(target); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return err
		}
		return invalid
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return invalid
	}
	return nil
}
