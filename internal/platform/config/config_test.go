package config

import (
	"strings"
	"testing"
)

func TestLoadRequiresDatabaseURL(t *testing.T) {
	clearOptionalEnvironment(t)
	t.Setenv("LANSURFER_DATABASE_URL", "")
	t.Setenv("LANSURFER_LISTEN_ADDR", "")

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "LANSURFER_DATABASE_URL") {
		t.Fatalf("Load() error = %v, want missing database URL error", err)
	}
}

func TestLoadUsesDefaultListenAddress(t *testing.T) {
	clearOptionalEnvironment(t)
	t.Setenv("LANSURFER_DATABASE_URL", "postgres://example")
	t.Setenv("LANSURFER_LISTEN_ADDR", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.ListenAddr != defaultListenAddr {
		t.Errorf("ListenAddr = %q, want %q", cfg.ListenAddr, defaultListenAddr)
	}
	if cfg.BaseURL != defaultBaseURL {
		t.Errorf("BaseURL = %q, want %q", cfg.BaseURL, defaultBaseURL)
	}
	if cfg.OIDC != nil {
		t.Errorf("OIDC = %#v, want nil", cfg.OIDC)
	}
}

func TestLoadReadsConfiguredValues(t *testing.T) {
	clearOptionalEnvironment(t)
	t.Setenv("LANSURFER_DATABASE_URL", " postgres://example/lansurfer ")
	t.Setenv("LANSURFER_LISTEN_ADDR", " 127.0.0.1:9090 ")
	t.Setenv("LANSURFER_BASE_URL", " https://lan.example.test/ ")
	t.Setenv("LANSURFER_OIDC_ISSUER", " https://id.example.test/realms/lan ")
	t.Setenv("LANSURFER_OIDC_CLIENT_ID", " lansurfer ")
	t.Setenv("LANSURFER_OIDC_CLIENT_SECRET", " secret ")

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
	if cfg.BaseURL != "https://lan.example.test" {
		t.Errorf("BaseURL = %q", cfg.BaseURL)
	}
	if cfg.OIDC == nil || cfg.OIDC.Issuer != "https://id.example.test/realms/lan" ||
		cfg.OIDC.ClientID != "lansurfer" || cfg.OIDC.ClientSecret != "secret" {
		t.Errorf("OIDC = %#v", cfg.OIDC)
	}
}

func TestLoadRejectsInvalidBaseURL(t *testing.T) {
	clearOptionalEnvironment(t)
	t.Setenv("LANSURFER_DATABASE_URL", "postgres://example")
	t.Setenv("LANSURFER_BASE_URL", "https://lan.example.test/a/path")

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "LANSURFER_BASE_URL") {
		t.Fatalf("Load() error = %v, want invalid base URL error", err)
	}
}

func TestLoadRejectsPartialOIDCConfiguration(t *testing.T) {
	clearOptionalEnvironment(t)
	t.Setenv("LANSURFER_DATABASE_URL", "postgres://example")
	t.Setenv("LANSURFER_OIDC_ISSUER", "https://id.example.test")

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "must be configured together") {
		t.Fatalf("Load() error = %v, want partial OIDC error", err)
	}
}

func TestLoadAllowsPublicOIDCClientWithoutSecret(t *testing.T) {
	clearOptionalEnvironment(t)
	t.Setenv("LANSURFER_DATABASE_URL", "postgres://example")
	t.Setenv("LANSURFER_OIDC_ISSUER", "https://auth.convention-x-treme.de/realms/local-dev")
	t.Setenv("LANSURFER_OIDC_CLIENT_ID", "lansurver")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.OIDC == nil || cfg.OIDC.Issuer != "https://auth.convention-x-treme.de/realms/local-dev" ||
		cfg.OIDC.ClientID != "lansurver" || cfg.OIDC.ClientSecret != "" {
		t.Fatalf("OIDC = %#v", cfg.OIDC)
	}
}

func clearOptionalEnvironment(t *testing.T) {
	t.Helper()
	for _, name := range []string{
		"LANSURFER_BASE_URL",
		"LANSURFER_OIDC_ISSUER",
		"LANSURFER_OIDC_CLIENT_ID",
		"LANSURFER_OIDC_CLIENT_SECRET",
	} {
		t.Setenv(name, "")
	}
}
