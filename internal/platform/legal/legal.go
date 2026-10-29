// Package legal stores the operator's Terms of Service, Privacy Policy, and
// Imprint. They are instance-operator content edited by administrators, not
// player data, so like the metrics key they live outside the business modules.
package legal

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Kind string

const (
	Terms   Kind = "terms"
	Privacy Kind = "privacy"
	Imprint Kind = "imprint"
)

// MaxBodyLength matches the legal_documents body CHECK, in Unicode characters.
// ponytail: bounded by the 64 KiB URL-encoded form limit (middleware.CSRF);
// raise both together with a per-route form limit if operators need longer texts.
const MaxBodyLength = 20000

var (
	ErrNotFound = errors.New("legal document not found")
	ErrEmpty    = errors.New("legal document must not be empty")
	ErrTooLong  = fmt.Errorf("legal document must be %d characters or fewer", MaxBodyLength)
)

// Document is one legal text as Markdown. Sample means it is still the
// generic text seeded by the migration, never saved by an administrator.
type Document struct {
	Kind      Kind
	Body      string
	Sample    bool
	UpdatedAt time.Time
}

type Store struct{ pool *pgxpool.Pool }

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

func (s *Store) Document(ctx context.Context, kind Kind) (Document, error) {
	document := Document{Kind: kind}
	err := s.pool.QueryRow(ctx, `SELECT body, sample, updated_at FROM legal_documents WHERE kind = $1`, string(kind)).
		Scan(&document.Body, &document.Sample, &document.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Document{}, ErrNotFound
	}
	if err != nil {
		return Document{}, fmt.Errorf("read legal document %s: %w", kind, err)
	}
	return document, nil
}

// Normalize trims a document body and checks it against the stored bounds.
func Normalize(body string) (string, error) {
	body = strings.TrimSpace(body)
	if body == "" {
		return "", ErrEmpty
	}
	if utf8.RuneCountInString(body) > MaxBodyLength {
		return "", ErrTooLong
	}
	return body, nil
}

// Save replaces a document's text and clears its sample marker.
func (s *Store) Save(ctx context.Context, kind Kind, body string) error {
	body, err := Normalize(body)
	if err != nil {
		return err
	}
	tag, err := s.pool.Exec(ctx, `UPDATE legal_documents SET body = $2, sample = false, updated_at = now() WHERE kind = $1`, string(kind), body)
	if err != nil {
		return fmt.Errorf("save legal document %s: %w", kind, err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
