package config

import (
	"strings"
	"testing"
)

func TestLoadRequiresDatabaseURL(t *testing.T) {
	t.Setenv("LANSURFER_DATABASE_URL", "")
	t.Setenv("LANSURFER_LISTEN_ADDR", "")

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "LANSURFER_DATABASE_URL") {
		t.Fatalf("Load() error = %v, want missing database URL error", err)
	}
}

func TestLoadUsesDefaultListenAddress(t *testing.T) {
	t.Setenv("LANSURFER_DATABASE_URL", "postgres://example")
	t.Setenv("LANSURFER_LISTEN_ADDR", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.ListenAddr != defaultListenAddr {
		t.Errorf("ListenAddr = %q, want %q", cfg.ListenAddr, defaultListenAddr)
	}
}

func TestLoadReadsConfiguredValues(t *testing.T) {
	t.Setenv("LANSURFER_DATABASE_URL", " postgres://example/lansurfer ")
	t.Setenv("LANSURFER_LISTEN_ADDR", " 127.0.0.1:9090 ")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.DatabaseURL != "postgres://example/lansurfer" {
		t.Errorf("DatabaseURL = %q", cfg.DatabaseURL)
	}
	if cfg.ListenAddr != "127.0.0.1:9090" {
		t.Errorf("ListenAddr = %q", cfg.ListenAddr)
	}
}
