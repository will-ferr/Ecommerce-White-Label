// Package apperr defines the errors that use cases return to their callers.
// It has no HTTP dependency, so domain code can use it freely.
package apperr

import "errors"

// Kind classifies an error independently of the transport.
type Kind int

// Supported error kinds.
const (
	KindInternal Kind = iota
	KindInvalid
	KindUnauthenticated
	KindForbidden
	KindNotFound
	KindConflict
	KindUnprocessable
	KindRateLimited
	KindUnavailable
)

// FieldError describes one invalid input field.
type FieldError struct {
	Field   string `json:"field"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Error is an expected failure. Code and Message are safe to show to clients.
// Cause is kept for logs only.
type Error struct {
	Kind    Kind
	Code    string
	Message string
	Fields  []FieldError
	Cause   error
}

// Error implements the error interface without exposing the cause to clients
// that only render the public message.
func (e *Error) Error() string {
	if e.Cause != nil {
		return e.Code + ": " + e.Cause.Error()
	}
	return e.Code + ": " + e.Message
}

// Unwrap exposes the cause to errors.Is and errors.As.
func (e *Error) Unwrap() error { return e.Cause }

// New creates an expected error.
func New(kind Kind, code, message string) *Error {
	return &Error{Kind: kind, Code: code, Message: message}
}

// Wrap creates an expected error that keeps an internal cause for logging.
func Wrap(kind Kind, code, message string, cause error) *Error {
	return &Error{Kind: kind, Code: code, Message: message, Cause: cause}
}

// Validation creates an error listing the invalid fields.
func Validation(fields ...FieldError) *Error {
	return &Error{
		Kind:    KindInvalid,
		Code:    "VALIDATION_FAILED",
		Message: "One or more fields are invalid.",
		Fields:  fields,
	}
}

// As returns the Error in err's chain, if there is one.
func As(err error) (*Error, bool) {
	var target *Error
	ok := errors.As(err, &target)
	return target, ok
}
