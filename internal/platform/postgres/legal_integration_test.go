//go:build integration

package postgres

import (
	"errors"
	"strings"
	"testing"

	"github.com/convention-x-treme/lansurfer/internal/platform/legal"
)

// A fresh instance publishes all three documents as marked samples; saving
// one replaces it and clears only its own marker.
func TestLegalDocumentsSeedSamplesAndSaveClearsMarker(t *testing.T) {
	pool := newTestPool(t)
	if err := Migrate(t.Context(), pool, testLogger()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	store := legal.NewStore(pool)
	for _, kind := range []legal.Kind{legal.Terms, legal.Privacy, legal.Imprint} {
		document, err := store.Document(t.Context(), kind)
		if err != nil || !document.Sample || strings.TrimSpace(document.Body) == "" {
			t.Fatalf("seeded %s = %+v, %v; want non-empty sample", kind, document, err)
		}
	}

	if err := store.Save(t.Context(), legal.Privacy, "  ## Controller\n\nClub e.V.\n  "); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	saved, err := store.Document(t.Context(), legal.Privacy)
	if err != nil || saved.Sample || saved.Body != "## Controller\n\nClub e.V." {
		t.Fatalf("saved privacy = %+v, %v", saved, err)
	}
	if terms, err := store.Document(t.Context(), legal.Terms); err != nil || !terms.Sample {
		t.Fatalf("terms after saving privacy = %+v, %v; want still sample", terms, err)
	}

	for body, want := range map[string]error{
		" \n\t ": legal.ErrEmpty,
		strings.Repeat("ä", legal.MaxBodyLength+1): legal.ErrTooLong,
	} {
		if err := store.Save(t.Context(), legal.Privacy, body); !errors.Is(err, want) {
			t.Errorf("Save(%d chars) error = %v, want %v", len(body), err, want)
		}
	}
	// The database enforces the same bound the store checks.
	if _, err := pool.Exec(t.Context(), `UPDATE legal_documents SET body = $1 WHERE kind = 'privacy'`,
		strings.Repeat("ä", legal.MaxBodyLength+1)); err == nil {
		t.Error("database accepted a body over MaxBodyLength")
	}
	if err := store.Save(t.Context(), legal.Kind("cookies"), "text"); !errors.Is(err, legal.ErrNotFound) {
		t.Errorf("Save(unknown kind) error = %v, want ErrNotFound", err)
	}
}
