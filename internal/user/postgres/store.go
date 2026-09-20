// Package postgres provides PostgreSQL persistence for the user module.
package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/convention-x-treme/lansurfer/internal/user"
	"github.com/convention-x-treme/lansurfer/internal/user/postgres/sqlcgen"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
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

func (s *Store) Create(ctx context.Context, gameNick string, role user.Role) (user.User, error) {
	row, err := s.queries.CreateUser(ctx, sqlcgen.CreateUserParams{
		GameNick: pgtype.Text{String: gameNick, Valid: true},
		Role:     string(role),
	})
	if err != nil {
		return user.User{}, gameNickError(err, "create user")
	}
	return user.User{
		ID:        row.ID,
		GameNick:  row.GameNick.String,
		Role:      user.Role(row.Role),
		CreatedAt: row.CreatedAt.Time,
		UpdatedAt: row.UpdatedAt.Time,
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
	return fromUser(row.ID, row.GameNick, row.Role, row.CreatedAt, row.UpdatedAt), nil
}

func (s *Store) List(ctx context.Context) ([]user.User, error) {
	rows, err := s.queries.ListUsers(ctx)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	result := make([]user.User, 0, len(rows))
	for _, row := range rows {
		result = append(result, fromUser(row.ID, row.GameNick, row.Role, row.CreatedAt, row.UpdatedAt))
	}
	return result, nil
}

func (s *Store) SetRole(ctx context.Context, id string, role user.Role) (user.User, error) {
	databaseID, err := parseUUID(id)
	if err != nil {
		return user.User{}, user.ErrNotFound
	}
	row, err := s.queries.SetUserRole(ctx, sqlcgen.SetUserRoleParams{ID: databaseID, Role: string(role)})
	if errors.Is(err, pgx.ErrNoRows) {
		return user.User{}, user.ErrNotFound
	}
	if err != nil {
		return user.User{}, fmt.Errorf("set user role: %w", err)
	}
	return fromUser(row.ID, row.GameNick, row.Role, row.CreatedAt, row.UpdatedAt), nil
}

func (s *Store) SetGameNick(ctx context.Context, id, gameNick string) (user.User, error) {
	databaseID, err := parseUUID(id)
	if err != nil {
		return user.User{}, user.ErrNotFound
	}
	row, err := s.queries.SetUserGameNick(ctx, sqlcgen.SetUserGameNickParams{ID: databaseID, GameNick: pgtype.Text{String: gameNick, Valid: true}})
	if errors.Is(err, pgx.ErrNoRows) {
		return user.User{}, user.ErrNotFound
	}
	if err != nil {
		return user.User{}, gameNickError(err, "set game nick")
	}
	return fromUser(row.ID, row.GameNick, row.Role, row.CreatedAt, row.UpdatedAt), nil
}

func fromUser(id string, gameNick pgtype.Text, role string, createdAt, updatedAt pgtype.Timestamptz) user.User {
	return user.User{
		ID: id, GameNick: gameNick.String, Role: user.Role(role),
		CreatedAt: createdAt.Time, UpdatedAt: updatedAt.Time,
	}
}

func gameNickError(err error, operation string) error {
	var databaseError *pgconn.PgError
	if errors.As(err, &databaseError) && databaseError.Code == "23505" && databaseError.ConstraintName == "users_game_nick_unique_idx" {
		return user.ErrGameNickTaken
	}
	return fmt.Errorf("%s: %w", operation, err)
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
