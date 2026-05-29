package inspector

import "errors"

// Errors returned by insights during Execute, particularly for parameter
// validation. The API layer maps these to appropriate HTTP status codes.
var (
	// ErrMissingParam: a required parameter was not provided.
	ErrMissingParam = errors.New("missing required parameter")

	// ErrInvalidParam: a parameter was provided but has invalid format or value.
	ErrInvalidParam = errors.New("invalid parameter")
)
