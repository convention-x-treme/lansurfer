package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/convention-x-treme/lansurfer/internal/auth"
	"github.com/convention-x-treme/lansurfer/internal/auth/postgres/sqlcgen"
	"github.com/jackc/pgx/v5/pgxpool"
)

// GuestAccessStore persists the auth-owned Guest Access singleton.
type GuestAccessStore struct {
	queries *sqlcgen.Queries
}

func NewGuestAccessStore(pool *pgxpool.Pool) *GuestAccessStore {
	return NewGuestAccessStoreDB(pool)
}

func NewGuestAccessStoreDB(db sqlcgen.DBTX) *GuestAccessStore {
	return &GuestAccessStore{queries: sqlcgen.New(db)}
}

// Lock takes the singleton row lock until the surrounding transaction ends.
func (s *GuestAccessStore) Lock(ctx context.Context) error {
	if _, err := s.queries.LockGuestAccess(ctx); err != nil {
		return fmt.Errorf("lock guest access: %w", err)
	}
	return nil
}

func (s *GuestAccessStore) State(ctx context.Context) (auth.GuestAccessRecord, error) {
	row, err := s.queries.GetGuestAccess(ctx)
	if err != nil {
		return auth.GuestAccessRecord{}, fmt.Errorf("get guest access: %w", err)
	}
	return auth.GuestAccessRecord{Enabled: row.Enabled, ShowOnLogin: row.ShowOnLogin, InviteCiphertext: row.InviteCiphertext}, nil
}

func (s *GuestAccessStore) SetShowOnLogin(ctx context.Context, show bool) error {
	rows, err := s.queries.SetGuestShowOnLogin(ctx, show)
	if err != nil {
		return fmt.Errorf("set guest login option: %w", err)
	}
	if rows == 0 {
		return errors.New("guest access row missing")
	}
	return nil
}

func (s *GuestAccessStore) SetEnabled(ctx context.Context, enabled bool, ciphertext []byte) error {
	rows, err := s.queries.SetGuestAccessEnabled(ctx, sqlcgen.SetGuestAccessEnabledParams{Enabled: enabled, InviteCiphertext: ciphertext})
	if err != nil {
		return fmt.Errorf("set guest access: %w", err)
	}
	if rows == 0 {
		return errors.New("guest access row missing")
	}
	return nil
}

func (s *GuestAccessStore) Rotate(ctx context.Context, ciphertext []byte) error {
	rows, err := s.queries.RotateGuestInvite(ctx, ciphertext)
	if err != nil {
		return fmt.Errorf("rotate guest invite: %w", err)
	}
	if rows == 0 {
		return errors.New("guest access row missing")
	}
	return nil
}
