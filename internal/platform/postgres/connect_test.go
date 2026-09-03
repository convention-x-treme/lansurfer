package postgres

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
)

func TestConnectRejectsMalformedDatabaseURL(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	_, err := Connect(context.Background(), "://not-a-database-url", logger)
	if err == nil || !strings.Contains(err.Error(), "parse database URL") {
		t.Fatalf("Connect() error = %v, want malformed database URL error", err)
	}
}
