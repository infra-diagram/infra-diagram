// Package service contains the API business logic layer.
//
// Handlers in internal/api/handlers call into this package to execute
// operations. The service layer translates between HTTP concerns (params,
// errors) and the underlying domain (inspector, store).
package service

import "errors"

// Sentinel errors that handlers map to HTTP status codes.
var (
	// ErrAccountNotFound: requested account_id does not exist in the store.
	// Maps to 404.
	ErrAccountNotFound = errors.New("account not found")

	// ErrInsightNotFound: requested insight type is not registered.
	// Maps to 404.
	ErrInsightNotFound = errors.New("insight not found")

	// ErrMissingParam: a required parameter for the insight is missing.
	// Maps to 400.
	ErrMissingParam = errors.New("missing required parameter")

	// ErrInvalidParam: a parameter has invalid format.
	// Maps to 400.
	ErrInvalidParam = errors.New("invalid parameter")

	// ErrFindingNotFound: a finding has not found.
	// Maps to 404.
	ErrFindingNotFound = errors.New("finding not found")
)
