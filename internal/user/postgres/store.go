// Package postgres provides PostgreSQL persistence for the user module.
package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/convention-x-treme/lansurfer/internal/user"
	"github.com/convention-x-treme/lansurfer/internal/user/postgres/sqlcgen"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	queries *sqlcgen.Queries
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{queries: sqlcgen.New(pool)}
}

func (s *Store) Create(ctx context.Context, displayName string, role user.Role) (user.User, error) {
	row, err := s.queries.CreateUser(ctx, sqlcgen.CreateUserParams{
		DisplayName: displayName,
		Role:        string(role),
	})
	if err != nil {
		return user.User{}, fmt.Errorf("create user: %w", err)
	}
	return user.User{
		ID:          row.ID,
		DisplayName: row.DisplayName,
		Role:        user.Role(row.Role),
		CreatedAt:   row.CreatedAt.Time,
		UpdatedAt:   row.UpdatedAt.Time,
	}, nil
}

func (s *Store) ByID(ctx context.Context, id string) (user.User, error) {
	databaseID, err := parseUUID(id)
	if err != nil {
		return user.User{}, user.ErrNotFound
	}
	row, err := s.queries.GetUser(ctx, databaseID)
	if errors.Is(err, pgx.ErrNoRows) {
		return user.User{}, user.ErrNotFound
	}
	if err != nil {
		return user.User{}, fmt.Errorf("get user: %w", err)
	}
	return user.User{
		ID:          row.ID,
		DisplayName: row.DisplayName,
		Role:        user.Role(row.Role),
		CreatedAt:   row.CreatedAt.Time,
		UpdatedAt:   row.UpdatedAt.Time,
	}, nil
}

func (s *Store) Delete(ctx context.Context, id string) error {
	databaseID, err := parseUUID(id)
	if err != nil {
		return user.ErrNotFound
	}
	deleted, err := s.queries.DeleteUser(ctx, databaseID)
	if err != nil {
		return fmt.Errorf("delete user: %w", err)
	}
	if deleted == 0 {
		return user.ErrNotFound
	}
	return nil
}

func parseUUID(value string) (pgtype.UUID, error) {
	var id pgtype.UUID
	if err := id.Scan(value); err != nil {
		return pgtype.UUID{}, err
	}
	return id, nil
}
