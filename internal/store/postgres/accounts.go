package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"infra-diagram/internal/store"
)

// GetAccount returns an account by UUID.
func (s *Store) GetAccount(ctx context.Context, id uuid.UUID) (*store.Account, error) {
	const query = `
		SELECT id, aws_account_id, alias, status
		FROM aws_accounts
		WHERE id = $1
	`

	var acc store.Account
	err := s.pool.QueryRow(ctx, query, id).Scan(
		&acc.ID,
		&acc.AWSAccountID,
		&acc.Alias,
		&acc.Status,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, store.ErrAccountNotFound
		}
		return nil, fmt.Errorf("querying account %s: %w", id, err)
	}
	return &acc, nil
}

// ListAccounts returns all accounts.
func (s *Store) ListAccounts(ctx context.Context) ([]*store.Account, error) {
	const query = `
		SELECT id, aws_account_id, alias, status
		FROM aws_accounts
		ORDER BY created_at DESC
	`

	rows, err := s.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("listing accounts: %w", err)
	}
	defer rows.Close()

	var accounts []*store.Account
	for rows.Next() {
		var acc store.Account
		if err := rows.Scan(&acc.ID, &acc.AWSAccountID, &acc.Alias, &acc.Status); err != nil {
			return nil, fmt.Errorf("scanning account row: %w", err)
		}
		accounts = append(accounts, &acc)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating account rows: %w", err)
	}
	return accounts, nil
}
