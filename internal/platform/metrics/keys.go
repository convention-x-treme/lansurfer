// Package metrics exposes Prometheus metrics behind one rotatable bearer key.
package metrics

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// keyLength is the encoded length of a 32-byte key in unpadded base64url.
const keyLength = 43

var keyEncoding = base64.RawURLEncoding.Strict()

// Store persists the single metrics access key as its SHA-256 hash only.
type Store struct{ pool *pgxpool.Pool }

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

// Configured reports whether a key hash exists.
func (s *Store) Configured(ctx context.Context) (bool, error) {
	var configured bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM metrics_access_key)`).Scan(&configured)
	return configured, err
}

// Rotate atomically replaces the active key and returns the new plaintext once.
func (s *Store) Rotate(ctx context.Context) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate metrics key: %w", err)
	}
	hash := sha256.Sum256(raw)
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO metrics_access_key (singleton, key_hash) VALUES (true, $1)
		ON CONFLICT (singleton) DO UPDATE SET key_hash = EXCLUDED.key_hash`, hash[:]); err != nil {
		return "", fmt.Errorf("store metrics key hash: %w", err)
	}
	return keyEncoding.EncodeToString(raw), nil
}

// Valid reports whether token is the active key. The hash is read on every
// call (no cache) so a rotation by any process applies to the next request.
// Malformed tokens are rejected before touching the database.
func (s *Store) Valid(ctx context.Context, token string) (bool, error) {
	if len(token) != keyLength {
		return false, nil
	}
	raw, err := keyEncoding.DecodeString(token)
	if err != nil || len(raw) != 32 {
		return false, nil
	}
	var stored []byte
	err = s.pool.QueryRow(ctx, `SELECT key_hash FROM metrics_access_key WHERE singleton`).Scan(&stored)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read metrics key hash: %w", err)
	}
	hash := sha256.Sum256(raw)
	return subtle.ConstantTimeCompare(hash[:], stored) == 1, nil
}
