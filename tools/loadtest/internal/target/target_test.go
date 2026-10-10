package target

import (
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestValidateHTTPURLAllowsOnlyKnownOrigins(t *testing.T) {
	for _, raw := range []string{"http://127.0.0.1:18080", "http://127.0.0.1:18081", "http://lansurfer:8080", "http://proxy:8080"} {
		if err := ValidateHTTPURL(raw); err != nil {
			t.Errorf("%q rejected: %v", raw, err)
		}
	}
	for _, raw := range []string{"https://127.0.0.1:18080", "http://localhost:18080", "http://127.0.0.1:18080/evil", "http://user@proxy:8080", "http://proxy:8080#fragment"} {
		if err := ValidateHTTPURL(raw); err == nil {
			t.Errorf("%q accepted", raw)
		}
	}
}

func TestDatabaseConfigEnforcesIsolatedEndpoint(t *testing.T) {
	t.Setenv("LANSURFER_LOADTEST_DATABASE_URL", "postgres://lansurfer_loadtest:secret@127.0.0.1:55433/lansurfer_loadtest?sslmode=disable")
	if _, err := DatabaseConfig(); err != nil {
		t.Fatalf("isolated DSN rejected: %v", err)
	}
	for _, dsn := range []string{
		"postgres://lansurfer_loadtest:secret@127.0.0.1:5432/lansurfer_loadtest",
		"postgres://lansurfer_loadtest:secret@localhost:55433/lansurfer_loadtest",
		"postgres://lansurfer_loadtest:secret@127.0.0.1:55433/production",
		"postgres://other:secret@127.0.0.1:55433/lansurfer_loadtest",
	} {
		t.Setenv("LANSURFER_LOADTEST_DATABASE_URL", dsn)
		if _, err := DatabaseConfig(); err == nil || strings.Contains(err.Error(), "secret") {
			t.Errorf("DSN rejected unsafely: error=%v", err)
		}
	}
}

func TestDatabaseConfigRejectsFallbackHosts(t *testing.T) {
	dsn := "host=127.0.0.1,localhost port=55433,5432 user=lansurfer_loadtest password=secret dbname=lansurfer_loadtest sslmode=disable"
	parsed, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse fallback DSN: %v", err)
	}
	if len(parsed.ConnConfig.Fallbacks) == 0 {
		t.Fatal("test DSN does not produce fallback hosts")
	}

	t.Setenv("LANSURFER_LOADTEST_DATABASE_URL", dsn)
	if _, err := DatabaseConfig(); err == nil {
		t.Fatal("multi-host DSN accepted")
	}
}
