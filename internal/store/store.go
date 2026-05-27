// Package store defines the persistence layer interface for the API
//
// The Store interface is the contract that the rest of the code consumes
// The postgres subpackage provides the concrete implementation.

package store

import (
	"context"
	"errors"

	"github.com/google/uuid"
)

// Sentinel errors returned by Store implementations.
// Callers use errors.Is to check for these
var (
	ErrAccountNotFound = errors.New("account not found")
)

// Account represents an AWS account registered in the system
type Account struct {
	ID           uuid.UUID
	AWSAccountID string
	Alias        string
	Status       string
}

// Store is the persistence interface consumed by the API layer.
//
// Implementations: internal/store/postgres/
type Store interface {
	//GetAccount returns an account by its internal UUID.
	GetAccount(ctx context.Context, id uuid.UUID) (*Account, error)

	// ListAccounts returns all registered accounts
	ListAccounts(ctx context.Context) ([]*Account, error)
}
