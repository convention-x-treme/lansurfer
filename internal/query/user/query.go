// Package user provides read-only user projections that span modules.
package user

import (
	"context"
	"fmt"

	"github.com/convention-x-treme/lansurfer/internal/query/user/sqlcgen"
	domain "github.com/convention-x-treme/lansurfer/internal/user"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	queries *sqlcgen.Queries
}

func NewStore(pool *pgxpool.Pool) *Store {
	return NewStoreDB(pool)
}

func NewStoreDB(db sqlcgen.DBTX) *Store {
	return &Store{queries: sqlcgen.New(db)}
}

// ListGuests returns accounts that still have a guest identity, oldest first.
func (s *Store) ListGuests(ctx context.Context) ([]domain.User, error) {
	rows, err := s.queries.ListGuests(ctx)
	if err != nil {
		return nil, fmt.Errorf("list guests: %w", err)
	}
	guests := make([]domain.User, 0, len(rows))
	for _, row := range rows {
		guests = append(guests, domain.User{
			ID: row.ID, GameNick: row.GameNick.String, Role: domain.Role(row.Role),
			CreatedAt: row.CreatedAt.Time, UpdatedAt: row.UpdatedAt.Time,
		})
	}
	return guests, nil
}
