package app

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/convention-x-treme/lansurfer/internal/platform/config"
)

// Both guards must fire before any database work: the configured URL points
// nowhere, so reaching Connect would surface a connection error instead.
func TestResetDemoRefusesBeforeTouchingTheDatabase(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	demo := &config.Demo{ResetHour: 4, ResetLocation: time.UTC}
	unreachable := "postgres://nobody@127.0.0.1:1/none?connect_timeout=1"
	write := func(t *testing.T, files map[string]string) string {
		dir := t.TempDir()
		for name, body := range files {
			if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		return dir
	}
	valid := map[string]string{"terms.md": "Terms", "privacy.md": "Privacy", "imprint.md": "Imprint"}

	for _, tc := range []struct {
		name    string
		cfg     config.Config
		files   map[string]string
		wantErr string
	}{
		{"demo mode off", config.Config{DatabaseURL: unreachable}, valid, "LANSURFER_DEMO_MODE"},
		{"missing privacy", config.Config{DatabaseURL: unreachable, Demo: demo}, map[string]string{"terms.md": "Terms"}, "privacy.md"},
		{"blank terms", config.Config{DatabaseURL: unreachable, Demo: demo}, map[string]string{"terms.md": " \n", "privacy.md": "Privacy"}, "must not be empty"},
		{"too long privacy", config.Config{DatabaseURL: unreachable, Demo: demo}, map[string]string{"terms.md": "Terms", "privacy.md": strings.Repeat("x", 20001)}, "characters or fewer"},
		{"imprint required when enabled", config.Config{DatabaseURL: unreachable, Demo: demo, ImprintEnabled: true}, map[string]string{"terms.md": "Terms", "privacy.md": "Privacy"}, "imprint.md"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ResetDemo(t.Context(), tc.cfg, write(t, tc.files), logger)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("ResetDemo() error = %v, want %q", err, tc.wantErr)
			}
		})
	}
}
