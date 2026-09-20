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
	t.Setenv("LANSURFER_OIDC_DISPLAY_NAME", " CXT account ")
	t.Setenv("LANSURFER_OIDC_GAME_NICK_CLAIM", " nickname ")
	t.Setenv("LANSURFER_DISCORD_CLIENT_ID", " discord-client ")
	t.Setenv("LANSURFER_DISCORD_CLIENT_SECRET", " discord-secret ")
	t.Setenv("LANSURFER_TRUSTED_PROXIES", " 10.20.0.5, 10.21.0.0/16 ")

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
		cfg.OIDC.ClientID != "lansurfer" || cfg.OIDC.ClientSecret != "secret" ||
		cfg.OIDC.DisplayName != "CXT account" || cfg.OIDC.GameNickClaim != "nickname" {
		t.Errorf("OIDC = %#v", cfg.OIDC)
	}
	if cfg.Discord == nil || cfg.Discord.ClientID != "discord-client" || cfg.Discord.ClientSecret != "discord-secret" {
		t.Errorf("Discord = %#v", cfg.Discord)
	}
	if len(cfg.TrustedProxies) != 2 || cfg.TrustedProxies[0].String() != "10.20.0.5/32" || cfg.TrustedProxies[1].String() != "10.21.0.0/16" {
		t.Errorf("TrustedProxies = %#v", cfg.TrustedProxies)
	}
}

func TestLoadRejectsInvalidTrustedProxy(t *testing.T) {
	clearOptionalEnvironment(t)
	t.Setenv("LANSURFER_DATABASE_URL", "postgres://example")
	t.Setenv("LANSURFER_TRUSTED_PROXIES", "10.0.0.1,not-an-address")

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "LANSURFER_TRUSTED_PROXIES") {
		t.Fatalf("Load() error = %v, want invalid trusted proxy error", err)
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
		cfg.OIDC.ClientID != "lansurver" || cfg.OIDC.ClientSecret != "" || cfg.OIDC.DisplayName != "OpenID Connect" {
		t.Fatalf("OIDC = %#v", cfg.OIDC)
	}
}

func TestLoadRejectsPartialDiscordConfiguration(t *testing.T) {
	clearOptionalEnvironment(t)
	t.Setenv("LANSURFER_DATABASE_URL", "postgres://example")
	t.Setenv("LANSURFER_DISCORD_CLIENT_ID", "client")

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "LANSURFER_DISCORD_CLIENT_ID") {
		t.Fatalf("Load() error = %v, want partial Discord error", err)
	}
}

func TestLoadLeavesDiscordUnsetWhenCredentialsAreEmpty(t *testing.T) {
	clearOptionalEnvironment(t)
	t.Setenv("LANSURFER_DATABASE_URL", "postgres://example")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Discord != nil {
		t.Fatalf("Discord = %#v, want nil", cfg.Discord)
	}
}

func TestLoadIGDBConfiguration(t *testing.T) {
	clearOptionalEnvironment(t)
	t.Setenv("LANSURFER_DATABASE_URL", "postgres://example")
	t.Setenv("LANSURFER_IGDB_CLIENT_ID", "client")
	t.Setenv("LANSURFER_IGDB_CLIENT_SECRET", "secret")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.IGDB == nil || cfg.IGDB.ClientID != "client" || cfg.IGDB.ClientSecret != "secret" {
		t.Fatalf("IGDB = %#v", cfg.IGDB)
	}
}

func TestLoadRejectsPartialIGDBConfiguration(t *testing.T) {
	clearOptionalEnvironment(t)
	t.Setenv("LANSURFER_DATABASE_URL", "postgres://example")
	t.Setenv("LANSURFER_IGDB_CLIENT_ID", "client")
	t.Setenv("LANSURFER_IGDB_CLIENT_SECRET", "")

	if _, err := Load(); err == nil {
		t.Fatal("Load() unexpectedly accepted partial IGDB configuration")
	}
}

func TestLoadLeavesIGDBUnsetWhenCredentialsAreEmpty(t *testing.T) {
	clearOptionalEnvironment(t)
	t.Setenv("LANSURFER_DATABASE_URL", "postgres://example")
	t.Setenv("LANSURFER_IGDB_CLIENT_ID", "")
	t.Setenv("LANSURFER_IGDB_CLIENT_SECRET", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.IGDB != nil {
		t.Fatalf("IGDB = %#v, want nil", cfg.IGDB)
	}
}

func TestLoadInstanceNameDefaultsUnset(t *testing.T) {
	clearOptionalEnvironment(t)
	t.Setenv("LANSURFER_DATABASE_URL", "postgres://example")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.InstanceName != "" {
		t.Errorf("InstanceName = %q, want empty", cfg.InstanceName)
	}
}

func TestLoadInstanceNameTrimsWhitespace(t *testing.T) {
	clearOptionalEnvironment(t)
	t.Setenv("LANSURFER_DATABASE_URL", "postgres://example")
	t.Setenv("LANSURFER_INSTANCE_NAME", "  Camp Nova  ")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.InstanceName != "Camp Nova" {
		t.Errorf("InstanceName = %q, want %q", cfg.InstanceName, "Camp Nova")
	}
}

func TestLoadInstanceNameWhitespaceOnlyIsUnset(t *testing.T) {
	clearOptionalEnvironment(t)
	t.Setenv("LANSURFER_DATABASE_URL", "postgres://example")
	t.Setenv("LANSURFER_INSTANCE_NAME", "   ")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.InstanceName != "" {
		t.Errorf("InstanceName = %q, want empty", cfg.InstanceName)
	}
}

func TestLoadRejectsInstanceNameWithControlCharacter(t *testing.T) {
	clearOptionalEnvironment(t)
	t.Setenv("LANSURFER_DATABASE_URL", "postgres://example")
	t.Setenv("LANSURFER_INSTANCE_NAME", "Camp\tNova")

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "LANSURFER_INSTANCE_NAME") {
		t.Fatalf("Load() error = %v, want instance name control-character error", err)
	}
}

func TestLoadRejectsInstanceNameWithNewline(t *testing.T) {
	clearOptionalEnvironment(t)
	t.Setenv("LANSURFER_DATABASE_URL", "postgres://example")
	t.Setenv("LANSURFER_INSTANCE_NAME", "Camp\nNova")

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "LANSURFER_INSTANCE_NAME") {
		t.Fatalf("Load() error = %v, want instance name newline error", err)
	}
}

func TestLoadRejectsInstanceNameWithUnicodeLineSeparator(t *testing.T) {
	clearOptionalEnvironment(t)
	t.Setenv("LANSURFER_DATABASE_URL", "postgres://example")
	t.Setenv("LANSURFER_INSTANCE_NAME", "Camp Nova")

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "LANSURFER_INSTANCE_NAME") {
		t.Fatalf("Load() error = %v, want instance name line-separator error", err)
	}
}

func TestLoadRejectsInstanceNameWithUnicodeParagraphSeparator(t *testing.T) {
	clearOptionalEnvironment(t)
	t.Setenv("LANSURFER_DATABASE_URL", "postgres://example")
	t.Setenv("LANSURFER_INSTANCE_NAME", "Camp Nova")

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "LANSURFER_INSTANCE_NAME") {
		t.Fatalf("Load() error = %v, want instance name paragraph-separator error", err)
	}
}

func TestLoadRejectsInstanceNameOverMaxLength(t *testing.T) {
	clearOptionalEnvironment(t)
	t.Setenv("LANSURFER_DATABASE_URL", "postgres://example")
	t.Setenv("LANSURFER_INSTANCE_NAME", strings.Repeat("a", 65))

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "LANSURFER_INSTANCE_NAME") {
		t.Fatalf("Load() error = %v, want instance name length error", err)
	}
}

func TestLoadAllowsInstanceNameAtMaxLength(t *testing.T) {
	clearOptionalEnvironment(t)
	t.Setenv("LANSURFER_DATABASE_URL", "postgres://example")
	// 64 Unicode code points, using a multi-byte rune to prove the limit counts
	// runes rather than bytes.
	name := strings.Repeat("é", 64)
	t.Setenv("LANSURFER_INSTANCE_NAME", name)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.InstanceName != name {
		t.Errorf("InstanceName = %q, want %q", cfg.InstanceName, name)
	}
}

func clearOptionalEnvironment(t *testing.T) {
	t.Helper()
	for _, name := range []string{
		"LANSURFER_BASE_URL",
		"LANSURFER_OIDC_ISSUER",
		"LANSURFER_OIDC_CLIENT_ID",
		"LANSURFER_OIDC_CLIENT_SECRET",
		"LANSURFER_OIDC_DISPLAY_NAME",
		"LANSURFER_OIDC_GAME_NICK_CLAIM",
		"LANSURFER_DISCORD_CLIENT_ID",
		"LANSURFER_DISCORD_CLIENT_SECRET",
		"LANSURFER_TRUSTED_PROXIES",
		"LANSURFER_IGDB_CLIENT_ID",
		"LANSURFER_IGDB_CLIENT_SECRET",
		"LANSURFER_INSTANCE_NAME",
	} {
		t.Setenv(name, "")
	}
}
