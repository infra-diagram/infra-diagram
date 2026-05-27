// Package postgres provides a Store implementation backed by PostgreSQL
package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	pool *pgxpool.Pool
}

// New creates a Store with the given connection string.
// The pool is verified with a ping before returning
//
// Example conString:
// postgres://infra_diagram:password@localhost:5432/infra_diagram
func New(ctx context.Context, connString string) (*Store, error) {
	pool, err := pgxpool.New(ctx, connString)

	if err != nil {
		return nil, fmt.Errorf("creating pgx pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("pinging postgres: %w", err)
	}

	return &Store{pool: pool}, nil
}

// Close releases all connections in the pool
func (s *Store) Close() {
	s.pool.Close()
}
