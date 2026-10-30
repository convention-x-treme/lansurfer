package config

import (
	"strings"
	"testing"
	"time"
)

// Generated once for these tests only; not a real operator key.
const (
	validVAPIDPublicKeyForTests    = "BAPXjly0gRlUqDTh973k-L9pQbfuajNHt-75lr18ZrG1R9cW6wU9xZ62F7Ndf_nQW7FSAEP2-LXZldrFzngmFhQ"
	validVAPIDPrivateKeyForTests   = "QUs8zCpDK_x4zKsunw_iz-nahUva_gFm-jekct86r70"
	validDataEncryptionKeyForTests = "o4ZMRhaYDlsJEjb7ZiQp6np7sptQwfpQQ8W8a+WgrGo="
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

func TestLoadDiscordDeliveryDisabledWhenCredentialsAreEmpty(t *testing.T) {
	clearOptionalEnvironment(t)
	t.Setenv("LANSURFER_DATABASE_URL", "postgres://example")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.DiscordDelivery != nil {
		t.Fatalf("DiscordDelivery = %#v, want nil", cfg.DiscordDelivery)
	}
}

func TestLoadDiscordDeliveryValidConfiguration(t *testing.T) {
	clearOptionalEnvironment(t)
	t.Setenv("LANSURFER_DATABASE_URL", "postgres://example")
	t.Setenv("LANSURFER_DISCORD_WEBHOOK_CLIENT_ID", "webhook-client")
	t.Setenv("LANSURFER_DISCORD_WEBHOOK_CLIENT_SECRET", "webhook-secret")
	t.Setenv("LANSURFER_DATA_ENCRYPTION_KEY", validDataEncryptionKeyForTests)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.DiscordDelivery == nil || cfg.DiscordDelivery.ClientID != "webhook-client" ||
		cfg.DiscordDelivery.ClientSecret != "webhook-secret" {
		t.Fatalf("DiscordDelivery = %#v, want populated webhook credentials", cfg.DiscordDelivery)
	}
}

func TestLoadRejectsPartialDiscordDeliveryConfiguration(t *testing.T) {
	clearOptionalEnvironment(t)
	t.Setenv("LANSURFER_DATABASE_URL", "postgres://example")
	t.Setenv("LANSURFER_DISCORD_WEBHOOK_CLIENT_ID", "webhook-client")

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "LANSURFER_DISCORD_WEBHOOK_CLIENT_ID") {
		t.Fatalf("Load() error = %v, want partial Discord delivery error", err)
	}
}

func TestLoadDiscordDeliveryRequiresDataEncryptionKey(t *testing.T) {
	clearOptionalEnvironment(t)
	t.Setenv("LANSURFER_DATABASE_URL", "postgres://example")
	t.Setenv("LANSURFER_DISCORD_WEBHOOK_CLIENT_ID", "webhook-client")
	t.Setenv("LANSURFER_DISCORD_WEBHOOK_CLIENT_SECRET", "webhook-secret")

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "LANSURFER_DATA_ENCRYPTION_KEY") {
		t.Fatalf("Load() error = %v, want a missing-encryption-key error", err)
	}
}

func TestLoadDiscordDeliveryRejectsInvalidDataEncryptionKey(t *testing.T) {
	clearOptionalEnvironment(t)
	t.Setenv("LANSURFER_DATABASE_URL", "postgres://example")
	t.Setenv("LANSURFER_DISCORD_WEBHOOK_CLIENT_ID", "webhook-client")
	t.Setenv("LANSURFER_DISCORD_WEBHOOK_CLIENT_SECRET", "webhook-secret")
	t.Setenv("LANSURFER_DATA_ENCRYPTION_KEY", "not-valid-base64-key")

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "LANSURFER_DATA_ENCRYPTION_KEY") {
		t.Fatalf("Load() error = %v, want an invalid-encryption-key error", err)
	}
}

// Discord login and Discord delivery are independent: configuring one must
// not enable, require, or otherwise affect the other.
func TestLoadDiscordLoginAndDeliveryAreIndependent(t *testing.T) {
	clearOptionalEnvironment(t)
	t.Setenv("LANSURFER_DATABASE_URL", "postgres://example")
	t.Setenv("LANSURFER_DISCORD_CLIENT_ID", "login-client")
	t.Setenv("LANSURFER_DISCORD_CLIENT_SECRET", "login-secret")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Discord == nil {
		t.Fatalf("Discord = nil, want configured login credentials")
	}
	if cfg.DiscordDelivery != nil {
		t.Fatalf("DiscordDelivery = %#v, want nil when only Discord login is configured", cfg.DiscordDelivery)
	}

	clearOptionalEnvironment(t)
	t.Setenv("LANSURFER_DATABASE_URL", "postgres://example")
	t.Setenv("LANSURFER_DISCORD_WEBHOOK_CLIENT_ID", "webhook-client")
	t.Setenv("LANSURFER_DISCORD_WEBHOOK_CLIENT_SECRET", "webhook-secret")
	t.Setenv("LANSURFER_DATA_ENCRYPTION_KEY", validDataEncryptionKeyForTests)

	cfg, err = Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.DiscordDelivery == nil {
		t.Fatalf("DiscordDelivery = nil, want configured delivery credentials")
	}
	if cfg.Discord != nil {
		t.Fatalf("Discord = %#v, want nil when only Discord delivery is configured", cfg.Discord)
	}
}

func TestLoadIGDBConfiguration(t *testing.T) {
	clearOptionalEnvironment(t)
	t.Setenv("LANSURFER_DATABASE_URL", "postgres://example")
	t.Setenv("LANSURFER_TWITCH_CLIENT_ID", "client")
	t.Setenv("LANSURFER_TWITCH_CLIENT_SECRET", "secret")
	t.Setenv("LANSURFER_IGDB_ENABLED", "true")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Twitch == nil || cfg.Twitch.ClientID != "client" || cfg.Twitch.ClientSecret != "secret" || !cfg.Twitch.IGDBEnabled {
		t.Fatalf("Twitch = %#v", cfg.Twitch)
	}
}

func TestLoadTwitchConfigurationEnablesCapabilitiesIndependently(t *testing.T) {
	clearOptionalEnvironment(t)
	t.Setenv("LANSURFER_DATABASE_URL", "postgres://example")
	t.Setenv("LANSURFER_TWITCH_CLIENT_ID", "client")
	t.Setenv("LANSURFER_TWITCH_CLIENT_SECRET", "secret")
	t.Setenv("LANSURFER_TWITCH_AUTH_ENABLED", "true")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Twitch == nil || !cfg.Twitch.AuthEnabled || cfg.Twitch.IGDBEnabled {
		t.Fatalf("Twitch = %#v", cfg.Twitch)
	}
}

func TestLoadTwitchConfigurationAcceptsEachCapabilityCombination(t *testing.T) {
	tests := []struct {
		name, authEnabled, igdbEnabled string
		wantAuth, wantIGDB             bool
	}{
		{name: "both disabled"},
		{name: "IGDB only", igdbEnabled: "true", wantIGDB: true},
		{name: "both enabled", authEnabled: "true", igdbEnabled: "true", wantAuth: true, wantIGDB: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			clearOptionalEnvironment(t)
			t.Setenv("LANSURFER_DATABASE_URL", "postgres://example")
			t.Setenv("LANSURFER_TWITCH_CLIENT_ID", "client")
			t.Setenv("LANSURFER_TWITCH_CLIENT_SECRET", "secret")
			t.Setenv("LANSURFER_TWITCH_AUTH_ENABLED", test.authEnabled)
			t.Setenv("LANSURFER_IGDB_ENABLED", test.igdbEnabled)

			cfg, err := Load()
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Twitch == nil || cfg.Twitch.AuthEnabled != test.wantAuth || cfg.Twitch.IGDBEnabled != test.wantIGDB {
				t.Fatalf("Twitch = %#v", cfg.Twitch)
			}
		})
	}
}

func TestLoadRejectsInvalidTwitchConfiguration(t *testing.T) {
	tests := []struct {
		name, clientID, clientSecret, authEnabled, igdbEnabled string
	}{
		{name: "enabled without credentials", igdbEnabled: "true"},
		{name: "partial credentials", clientID: "client", authEnabled: "true"},
		{name: "invalid auth flag", clientID: "client", clientSecret: "secret", authEnabled: "sometimes"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			clearOptionalEnvironment(t)
			t.Setenv("LANSURFER_DATABASE_URL", "postgres://example")
			t.Setenv("LANSURFER_TWITCH_CLIENT_ID", test.clientID)
			t.Setenv("LANSURFER_TWITCH_CLIENT_SECRET", test.clientSecret)
			t.Setenv("LANSURFER_TWITCH_AUTH_ENABLED", test.authEnabled)
			t.Setenv("LANSURFER_IGDB_ENABLED", test.igdbEnabled)
			if _, err := Load(); err == nil {
				t.Fatal("Load() accepted invalid Twitch configuration")
			}
		})
	}
}

func TestLoadRejectsPartialIGDBConfiguration(t *testing.T) {
	clearOptionalEnvironment(t)
	t.Setenv("LANSURFER_DATABASE_URL", "postgres://example")
	t.Setenv("LANSURFER_TWITCH_CLIENT_ID", "client")
	t.Setenv("LANSURFER_IGDB_ENABLED", "true")

	if _, err := Load(); err == nil {
		t.Fatal("Load() unexpectedly accepted partial IGDB configuration")
	}
}

func TestLoadLeavesIGDBUnsetWhenCredentialsAreEmpty(t *testing.T) {
	clearOptionalEnvironment(t)
	t.Setenv("LANSURFER_DATABASE_URL", "postgres://example")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Twitch != nil {
		t.Fatalf("Twitch = %#v, want nil", cfg.Twitch)
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

func TestLoadWebPushRequiresBothVAPIDKeysTogether(t *testing.T) {
	clearOptionalEnvironment(t)
	t.Setenv("LANSURFER_DATABASE_URL", "postgres://example")
	t.Setenv("LANSURFER_WEBPUSH_PUBLIC_KEY", validVAPIDPublicKeyForTests)
	t.Setenv("LANSURFER_WEBPUSH_PRIVATE_KEY", "")

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "LANSURFER_WEBPUSH_PUBLIC_KEY") {
		t.Fatalf("Load() error = %v, want a paired-configuration error", err)
	}
}

func TestLoadWebPushRejectsMalformedVAPIDKey(t *testing.T) {
	clearOptionalEnvironment(t)
	t.Setenv("LANSURFER_DATABASE_URL", "postgres://example")
	t.Setenv("LANSURFER_WEBPUSH_PUBLIC_KEY", "not-a-valid-key")
	t.Setenv("LANSURFER_WEBPUSH_PRIVATE_KEY", validVAPIDPrivateKeyForTests)
	t.Setenv("LANSURFER_WEBPUSH_SUBJECT", "mailto:ops@example.test")
	t.Setenv("LANSURFER_DATA_ENCRYPTION_KEY", validDataEncryptionKeyForTests)

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "VAPID") {
		t.Fatalf("Load() error = %v, want an invalid VAPID key error", err)
	}
}

func TestLoadWebPushRequiresSubject(t *testing.T) {
	clearOptionalEnvironment(t)
	t.Setenv("LANSURFER_DATABASE_URL", "postgres://example")
	t.Setenv("LANSURFER_WEBPUSH_PUBLIC_KEY", validVAPIDPublicKeyForTests)
	t.Setenv("LANSURFER_WEBPUSH_PRIVATE_KEY", validVAPIDPrivateKeyForTests)
	t.Setenv("LANSURFER_WEBPUSH_SUBJECT", "")
	t.Setenv("LANSURFER_DATA_ENCRYPTION_KEY", validDataEncryptionKeyForTests)

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "LANSURFER_WEBPUSH_SUBJECT") {
		t.Fatalf("Load() error = %v, want a missing-subject error", err)
	}
}

func TestLoadWebPushRequiresDataEncryptionKey(t *testing.T) {
	clearOptionalEnvironment(t)
	t.Setenv("LANSURFER_DATABASE_URL", "postgres://example")
	t.Setenv("LANSURFER_WEBPUSH_PUBLIC_KEY", validVAPIDPublicKeyForTests)
	t.Setenv("LANSURFER_WEBPUSH_PRIVATE_KEY", validVAPIDPrivateKeyForTests)
	t.Setenv("LANSURFER_WEBPUSH_SUBJECT", "mailto:ops@example.test")
	t.Setenv("LANSURFER_DATA_ENCRYPTION_KEY", "")

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "LANSURFER_DATA_ENCRYPTION_KEY") {
		t.Fatalf("Load() error = %v, want a missing-encryption-key error", err)
	}
}

func TestLoadWebPushDisabledWithoutEitherVAPIDKey(t *testing.T) {
	clearOptionalEnvironment(t)
	t.Setenv("LANSURFER_DATABASE_URL", "postgres://example")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.WebPush != nil {
		t.Fatalf("WebPush = %#v, want nil when both VAPID keys are absent", cfg.WebPush)
	}
}

func TestLoadWebPushAvailableWithValidConfiguration(t *testing.T) {
	clearOptionalEnvironment(t)
	t.Setenv("LANSURFER_DATABASE_URL", "postgres://example")
	t.Setenv("LANSURFER_WEBPUSH_PUBLIC_KEY", validVAPIDPublicKeyForTests)
	t.Setenv("LANSURFER_WEBPUSH_PRIVATE_KEY", validVAPIDPrivateKeyForTests)
	t.Setenv("LANSURFER_WEBPUSH_SUBJECT", "mailto:ops@example.test")
	t.Setenv("LANSURFER_DATA_ENCRYPTION_KEY", validDataEncryptionKeyForTests)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.WebPush == nil || cfg.WebPush.Subject != "mailto:ops@example.test" {
		t.Fatalf("WebPush = %#v, want a populated config with the given subject", cfg.WebPush)
	}
	if len(cfg.DataEncryptionKey) != 32 {
		t.Fatalf("DataEncryptionKey length = %d, want 32", len(cfg.DataEncryptionKey))
	}
}

func TestLoadDataEncryptionKeyIndependentOfWebPush(t *testing.T) {
	clearOptionalEnvironment(t)
	t.Setenv("LANSURFER_DATABASE_URL", "postgres://example")
	t.Setenv("LANSURFER_DATA_ENCRYPTION_KEY", validDataEncryptionKeyForTests)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(cfg.DataEncryptionKey) != 32 {
		t.Fatalf("DataEncryptionKey length = %d, want 32 even without Web Push configured", len(cfg.DataEncryptionKey))
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
		"LANSURFER_DISCORD_WEBHOOK_CLIENT_ID",
		"LANSURFER_DISCORD_WEBHOOK_CLIENT_SECRET",
		"LANSURFER_TRUSTED_PROXIES",
		"LANSURFER_CHAT_RATE_LIMIT",
		"LANSURFER_CHAT_RATE_WINDOW",
		"LANSURFER_TWITCH_CLIENT_ID",
		"LANSURFER_TWITCH_CLIENT_SECRET",
		"LANSURFER_TWITCH_AUTH_ENABLED",
		"LANSURFER_IGDB_ENABLED",
		"LANSURFER_INSTANCE_NAME",
		"LANSURFER_WEBPUSH_PUBLIC_KEY",
		"LANSURFER_WEBPUSH_PRIVATE_KEY",
		"LANSURFER_WEBPUSH_SUBJECT",
		"LANSURFER_DATA_ENCRYPTION_KEY",
		"LANSURFER_IMPRINT_ENABLED",
		"LANSURFER_DEMO_MODE",
		"LANSURFER_DEMO_RESET_TIME",
		"LANSURFER_DEMO_RESET_TIMEZONE",
	} {
		t.Setenv(name, "")
	}
}

func TestLoadReadsCoverCacheDir(t *testing.T) {
	t.Setenv("LANSURFER_DATABASE_URL", "postgres://example")
	t.Setenv("LANSURFER_COVER_CACHE_DIR", " /var/cache/lansurfer/covers ")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.CoverCacheDir != "/var/cache/lansurfer/covers" {
		t.Errorf("CoverCacheDir = %q", cfg.CoverCacheDir)
	}
}

func TestLoadChatRate(t *testing.T) {
	clearOptionalEnvironment(t)
	t.Setenv("LANSURFER_DATABASE_URL", "postgres://example")

	cfg, err := Load()
	if err != nil || cfg.ChatRateLimit != 0 || cfg.ChatRateWindow != 0 {
		t.Fatalf("unset = %d, %v, %v; want zero values", cfg.ChatRateLimit, cfg.ChatRateWindow, err)
	}

	t.Setenv("LANSURFER_CHAT_RATE_LIMIT", " 3 ")
	t.Setenv("LANSURFER_CHAT_RATE_WINDOW", " 30s ")
	cfg, err = Load()
	if err != nil || cfg.ChatRateLimit != 3 || cfg.ChatRateWindow != 30*time.Second {
		t.Fatalf("configured = %d, %v, %v", cfg.ChatRateLimit, cfg.ChatRateWindow, err)
	}

	for name, value := range map[string]string{"LANSURFER_CHAT_RATE_LIMIT": "0", "LANSURFER_CHAT_RATE_WINDOW": "fast"} {
		t.Setenv(name, value)
		if _, err := Load(); err == nil {
			t.Errorf("%s=%q accepted", name, value)
		}
		t.Setenv(name, map[string]string{"LANSURFER_CHAT_RATE_LIMIT": "3", "LANSURFER_CHAT_RATE_WINDOW": "30s"}[name])
	}
}

func TestLoadDemo(t *testing.T) {
	clearOptionalEnvironment(t)
	t.Setenv("LANSURFER_DATABASE_URL", "postgres://example")
	// Off: invalid schedule values are ignored.
	t.Setenv("LANSURFER_DEMO_RESET_TIME", "late")
	t.Setenv("LANSURFER_DEMO_RESET_TIMEZONE", "Mars/Olympus")
	if cfg, err := Load(); err != nil || cfg.Demo != nil {
		t.Fatalf("demo off = %+v, %v; want nil", cfg.Demo, err)
	}

	t.Setenv("LANSURFER_DEMO_MODE", "true")
	t.Setenv("LANSURFER_DEMO_RESET_TIME", "")
	t.Setenv("LANSURFER_DEMO_RESET_TIMEZONE", "")
	cfg, err := Load()
	if err != nil || cfg.Demo == nil || cfg.Demo.ResetHour != 4 || cfg.Demo.ResetMinute != 0 || cfg.Demo.ResetLocation.String() != "Europe/Berlin" {
		t.Fatalf("demo defaults = %+v, %v", cfg.Demo, err)
	}

	for name, value := range map[string]string{
		"LANSURFER_DEMO_MODE":           "yes",
		"LANSURFER_DEMO_RESET_TIME":     "4:00",
		"LANSURFER_DEMO_RESET_TIMEZONE": "Mars/Olympus",
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv(name, value)
			if _, err := Load(); err == nil || !strings.Contains(err.Error(), name) {
				t.Errorf("%s=%q error = %v, want rejection", name, value, err)
			}
		})
	}
}
