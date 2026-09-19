// Package postgres provides PostgreSQL persistence for the auth module.
package postgres

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"time"

	"github.com/convention-x-treme/lansurfer/internal/auth"
	"github.com/convention-x-treme/lansurfer/internal/auth/postgres/sqlcgen"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

type IdentityStore struct {
	queries *sqlcgen.Queries
}

type SessionStore struct {
	queries *sqlcgen.Queries
}

func NewIdentityStore(pool *pgxpool.Pool) *IdentityStore {
	return &IdentityStore{queries: sqlcgen.New(pool)}
}

func NewSessionStore(pool *pgxpool.Pool) *SessionStore {
	return &SessionStore{queries: sqlcgen.New(pool)}
}

func (s *IdentityStore) ByProviderSubject(ctx context.Context, providerKind, providerKey, subject string) (auth.Identity, error) {
	row, err := s.queries.GetIdentity(ctx, sqlcgen.GetIdentityParams{ProviderKind: providerKind, ProviderKey: providerKey, Subject: subject})
	if errors.Is(err, pgx.ErrNoRows) {
		return auth.Identity{}, auth.ErrIdentityNotFound
	}
	if err != nil {
		return auth.Identity{}, fmt.Errorf("get identity: %w", err)
	}
	return identityFromGetRow(row), nil
}

func (s *IdentityStore) ListByUser(ctx context.Context, userID string) ([]auth.Identity, error) {
	databaseID, err := parseUUID(userID)
	if err != nil {
		return nil, fmt.Errorf("parse identity user ID: %w", err)
	}
	rows, err := s.queries.ListIdentitiesByUser(ctx, databaseID)
	if err != nil {
		return nil, fmt.Errorf("list identities: %w", err)
	}
	identities := make([]auth.Identity, 0, len(rows))
	for _, row := range rows {
		identities = append(identities, auth.Identity{ID: row.ID, UserID: row.UserID, ProviderKind: row.ProviderKind, ProviderKey: row.ProviderKey, Subject: row.Subject, DisplayName: row.DisplayName.String, Email: row.Email.String, CreatedAt: row.CreatedAt.Time})
	}
	return identities, nil
}

func (s *IdentityStore) Create(ctx context.Context, identity auth.Identity) (auth.Identity, error) {
	userID, err := parseUUID(identity.UserID)
	if err != nil {
		return auth.Identity{}, fmt.Errorf("parse identity user ID: %w", err)
	}
	row, err := s.queries.CreateIdentity(ctx, sqlcgen.CreateIdentityParams{
		UserID:       userID,
		ProviderKind: identity.ProviderKind,
		ProviderKey:  identity.ProviderKey,
		Subject:      identity.Subject,
		DisplayName:  nullableText(identity.DisplayName),
		Email:        nullableText(identity.Email),
	})
	if isUniqueViolation(err) {
		return auth.Identity{}, auth.ErrIdentityExists
	}
	if err != nil {
		return auth.Identity{}, fmt.Errorf("create identity: %w", err)
	}
	return auth.Identity{
		ID:           row.ID,
		UserID:       row.UserID,
		ProviderKind: row.ProviderKind,
		ProviderKey:  row.ProviderKey,
		Subject:      row.Subject,
		DisplayName:  row.DisplayName.String,
		Email:        row.Email.String,
		CreatedAt:    row.CreatedAt.Time,
	}, nil
}

func (s *IdentityStore) UpdateMetadata(ctx context.Context, providerKind, providerKey, subject, displayName, email string) error {
	if err := s.queries.UpdateIdentityMetadata(ctx, sqlcgen.UpdateIdentityMetadataParams{
		ProviderKind: providerKind,
		ProviderKey:  providerKey,
		Subject:      subject,
		DisplayName:  nullableText(displayName),
		Email:        nullableText(email),
	}); err != nil {
		return fmt.Errorf("update identity metadata: %w", err)
	}
	return nil
}

func (s *IdentityStore) Unlink(ctx context.Context, userID, identityID string) error {
	databaseUserID, err := parseUUID(userID)
	if err != nil {
		return auth.ErrIdentityNotFound
	}
	databaseIdentityID, err := parseUUID(identityID)
	if err != nil {
		return auth.ErrIdentityNotFound
	}
	deleted, err := s.queries.DeleteIdentityUnlessLast(ctx, sqlcgen.DeleteIdentityUnlessLastParams{ID: databaseIdentityID, UserID: databaseUserID})
	if err != nil {
		return fmt.Errorf("unlink identity: %w", err)
	}
	if deleted == 0 {
		return auth.ErrIdentityNotFound
	}
	return nil
}

func (s *SessionStore) Create(ctx context.Context, idHash [sha256.Size]byte, userID string, expiresAt time.Time) error {
	databaseUserID, err := parseUUID(userID)
	if err != nil {
		return fmt.Errorf("parse session user ID: %w", err)
	}
	if err := s.queries.CreateSession(ctx, sqlcgen.CreateSessionParams{
		IDHash:    idHash[:],
		UserID:    databaseUserID,
		ExpiresAt: pgtype.Timestamptz{Time: expiresAt, Valid: true},
	}); err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	return nil
}

func (s *SessionStore) Resolve(ctx context.Context, idHash [sha256.Size]byte) (auth.Session, error) {
	row, err := s.queries.ResolveSession(ctx, idHash[:])
	if errors.Is(err, pgx.ErrNoRows) {
		return auth.Session{}, auth.ErrSessionNotFound
	}
	if err != nil {
		return auth.Session{}, fmt.Errorf("resolve session: %w", err)
	}
	return auth.Session{
		UserID:     row.UserID,
		ExpiresAt:  row.ExpiresAt.Time,
		CreatedAt:  row.CreatedAt.Time,
		LastSeenAt: row.LastSeenAt.Time,
	}, nil
}

func (s *SessionStore) Delete(ctx context.Context, idHash [sha256.Size]byte) error {
	deleted, err := s.queries.DeleteSession(ctx, idHash[:])
	if err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	if deleted == 0 {
		return auth.ErrSessionNotFound
	}
	return nil
}

func identityFromGetRow(row sqlcgen.GetIdentityRow) auth.Identity {
	return auth.Identity{
		ID:           row.ID,
		UserID:       row.UserID,
		ProviderKind: row.ProviderKind,
		ProviderKey:  row.ProviderKey,
		Subject:      row.Subject,
		DisplayName:  row.DisplayName.String,
		Email:        row.Email.String,
		CreatedAt:    row.CreatedAt.Time,
	}
}

func nullableText(value string) pgtype.Text {
	return pgtype.Text{String: value, Valid: value != ""}
}

func parseUUID(value string) (pgtype.UUID, error) {
	var id pgtype.UUID
	if err := id.Scan(value); err != nil {
		return pgtype.UUID{}, err
	}
	return id, nil
}

func isUniqueViolation(err error) bool {
	var databaseError *pgconn.PgError
	return errors.As(err, &databaseError) && databaseError.Code == "23505"
}
