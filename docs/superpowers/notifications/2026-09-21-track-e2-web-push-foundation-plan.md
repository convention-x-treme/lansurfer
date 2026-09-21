# Track E2 Foundation — Web Push Subscriptions & Settings Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the persistence, encryption, and account-settings foundation Track E2 (Web Push) needs — VAPID/data-encryption configuration, encrypted `PushSubscription` storage, preferences, the settings HTTP surface, and the device cookie — without sending a single push notification. Actual delivery (recipient materialization, the `notification.Transport` adapter, payload, worker wiring) is a separate follow-up plan; the browser-side service-worker `push`/`notificationclick` handlers and the settings page's enable/disable JavaScript are a second follow-up plan after that. This plan produces backend behavior fully covered by Go tests; nothing here can be exercised by a real browser yet.

**Architecture:** New `internal/platform/crypto` package (AES-256-GCM + HMAC-SHA256, stdlib only) backs new `notification` module persistence (`notification_push_subscription`, `notification_push_preference`) and a new `SubscriptionService` that is the only place plaintext subscription material and the encryption key meet — `Store` only ever sees ciphertext. A new `/account/notifications` settings page and its mutation routes reuse the existing CSRF/session/`RequireGameNick` middleware exactly like `/account` does today.

**Tech Stack:** Go 1.27, stdlib `crypto/aes`, `crypto/cipher`, `crypto/hmac`, `crypto/ecdh`; PostgreSQL via pgx/v5 + sqlc; templ/HTMX; no new dependency (the `go-webpush/v2` adapter and its API are the Delivery plan's concern, not this one).

**Spec:** `docs/superpowers/notifications/2026-09-20-track-e-web-push-design.md` (Status: Approved) is the requirements source for everything Web-Push-shaped in this plan. `docs/ARCHITECTURE.md` §15 already names the exact environment variables (`LANSURFER_DATA_ENCRYPTION_KEY`, `LANSURFER_WEBPUSH_PUBLIC_KEY`, `LANSURFER_WEBPUSH_PRIVATE_KEY`, `LANSURFER_WEBPUSH_SUBJECT`) this plan must produce.

## Global Constraints

- This plan implements **only**: config/crypto, `PushSubscription`/`PushPreference` persistence, `SubscriptionService`, the `/account/notifications` HTTP surface, the device cookie, and the logout hook. No recipient materialization, no `notification.Transport`, no actual push send, no service-worker JS. Do not add the `go-webpush/v2` dependency in this plan.
- VAPID is optional at the config level: absent (both keys empty) → Web Push disabled, application starts normally. Partial (one key set) → startup error. Both set but malformed → startup error. Both set and valid → available; `LANSURFER_WEBPUSH_SUBJECT` becomes required and must start with `mailto:` or `https://`.
- `LANSURFER_DATA_ENCRYPTION_KEY` is required whenever VAPID is configured, and validated as a 32-byte key. It is independent infrastructure (Track E3/Discord will reuse it later) — load and validate it whenever present, not only when VAPID is present.
- `Store` (the existing `*notificationpostgres.Store` from Track E1, extended in this plan) must never see plaintext endpoint/`p256dh`/`auth` or the encryption key. Only `SubscriptionService` encrypts/decrypts.
- One push endpoint belongs to at most one user; reassignment across users is atomic and happens only inside the explicit subscribe/enable flow, via a single `ON CONFLICT (endpoint_fingerprint)` upsert — never a separate read-then-write.
- `web_push_enabled=false` retains subscriptions; removing the last device does not change `web_push_enabled`. These are independent pieces of state, matching the design's explicit "distinct states" requirement.
- HTTP mutations are same-origin POSTs reusing the existing CSRF middleware (header or form field); no new `/api` surface.
- Follow existing repository conventions throughout: sqlc for all SQL, `internal/notification`'s existing narrow-interface style, `internal/web/handler`+`internal/web/pages` templ conventions (model directly on `internal/web/handler/account.go` / `account.templ`), and the composition-root wiring style in `internal/app/app.go`.
- Do not hand-edit generated sqlc files; run `task generate`.

## Decisions made in this plan (state if you'd override any before executing)

1. **Two new tables under the existing `notification` module**, not a new module: `notification_push_subscription` and `notification_push_preference`. Web Push subscriptions/preferences are `notification`-owned per `docs/ARCHITECTURE.md` §7's `notification` module ownership list.
2. **`Store` (not a new type) grows the new methods.** Track E1's `*notificationpostgres.Store` already owns all of `notification`'s persistence; splitting subscriptions into a second store type would be a premature abstraction with only one caller.
3. **A new `SubscriptionService` type** (not folded into Track E1's `Service`), because it owns a different concern (subscription/preference management + encryption) with a different caller (account settings HTTP) than `Service` (admin diagnostics). Both live in package `notification`.
4. **Encryption key encoding: standard base64** (`LANSURFER_DATA_ENCRYPTION_KEY`, decodes to exactly 32 bytes). **VAPID key encoding: base64url, padding optional**, matching the Web Push ecosystem convention; validated with stdlib `crypto/ecdh` (P-256 point/scalar validity) without needing the webpush library itself.
5. **No decrypt path in this plan's public surface.** The settings page never displays raw endpoint/`p256dh`/`auth`; only the later Delivery plan needs to decrypt subscriptions to actually send, and it can call `internal/platform/crypto.Decrypt` directly then.

---

### Task 1: Encryption primitives and VAPID/data-key configuration

**Files:**
- Create: `internal/platform/crypto/crypto.go`
- Test: `internal/platform/crypto/crypto_test.go`
- Modify: `internal/platform/config/config.go`
- Modify: `internal/platform/config/config_test.go`

**Interfaces:**
- Produces: `crypto.KeySize`, `crypto.ErrInvalidKey`, `crypto.ParseKey(value string) ([]byte, error)`, `crypto.Encrypt(key []byte, plaintext string) ([]byte, error)`, `crypto.Decrypt(key []byte, ciphertext []byte) (string, error)`, `crypto.Fingerprint(key []byte, data string) string` — consumed by Task 4's `SubscriptionService`. `config.WebPush{PublicKey, PrivateKey, Subject string}`, `config.Config.WebPush *WebPush`, `config.Config.DataEncryptionKey []byte` — consumed by Task 5's composition-root wiring.

- [ ] **Step 1: Write the failing crypto tests**

Create `internal/platform/crypto/crypto_test.go`:

```go
package crypto

import (
	"encoding/base64"
	"testing"
)

func TestParseKeyRequiresExactly32Bytes(t *testing.T) {
	key := make([]byte, KeySize)
	for i := range key {
		key[i] = byte(i)
	}
	encoded := base64.StdEncoding.EncodeToString(key)
	parsed, err := ParseKey(encoded)
	if err != nil {
		t.Fatalf("ParseKey() error = %v", err)
	}
	if len(parsed) != KeySize {
		t.Fatalf("ParseKey() length = %d, want %d", len(parsed), KeySize)
	}

	if _, err := ParseKey(base64.StdEncoding.EncodeToString(key[:16])); err == nil {
		t.Fatal("ParseKey() with a 16-byte key succeeded, want an error")
	}
	if _, err := ParseKey("not base64!!"); err == nil {
		t.Fatal("ParseKey() with invalid base64 succeeded, want an error")
	}
}

func TestEncryptDecryptRoundTrips(t *testing.T) {
	key := make([]byte, KeySize)
	for i := range key {
		key[i] = byte(i * 3)
	}
	ciphertext, err := Encrypt(key, "https://push.example/endpoint/abc123")
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}
	plaintext, err := Decrypt(key, ciphertext)
	if err != nil {
		t.Fatalf("Decrypt() error = %v", err)
	}
	if plaintext != "https://push.example/endpoint/abc123" {
		t.Fatalf("Decrypt() = %q, want the original plaintext", plaintext)
	}
}

func TestEncryptProducesDistinctCiphertextsForTheSamePlaintext(t *testing.T) {
	key := make([]byte, KeySize)
	first, err := Encrypt(key, "same plaintext")
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}
	second, err := Encrypt(key, "same plaintext")
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}
	if string(first) == string(second) {
		t.Fatal("Encrypt() produced identical ciphertext twice; the nonce is not being randomized")
	}
}

func TestDecryptRejectsTamperedCiphertext(t *testing.T) {
	key := make([]byte, KeySize)
	ciphertext, err := Encrypt(key, "authentic")
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}
	tampered := append([]byte{}, ciphertext...)
	tampered[len(tampered)-1] ^= 0xFF
	if _, err := Decrypt(key, tampered); err == nil {
		t.Fatal("Decrypt() accepted tampered ciphertext, want authentication failure")
	}
}

func TestFingerprintIsDeterministicAndKeyed(t *testing.T) {
	keyA := make([]byte, KeySize)
	keyB := make([]byte, KeySize)
	keyB[0] = 1
	first := Fingerprint(keyA, "https://push.example/endpoint/abc123")
	second := Fingerprint(keyA, "https://push.example/endpoint/abc123")
	if first != second {
		t.Fatalf("Fingerprint() is not deterministic: %q != %q", first, second)
	}
	if Fingerprint(keyA, "https://push.example/endpoint/abc123") == Fingerprint(keyB, "https://push.example/endpoint/abc123") {
		t.Fatal("Fingerprint() did not change with the key")
	}
	if Fingerprint(keyA, "https://push.example/endpoint/xyz789") == first {
		t.Fatal("Fingerprint() did not change with the input")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/platform/crypto/... -v`
Expected: FAIL to compile — package does not exist yet.

- [ ] **Step 3: Implement the crypto primitives**

Create `internal/platform/crypto/crypto.go`:

```go
// Package crypto provides the narrow AES-GCM/HMAC primitives Track E uses to
// store external credentials (Web Push subscription material, later Discord
// webhook tokens) at rest, and to derive a stable, non-reversible fingerprint
// for enforcing endpoint uniqueness without persisting the raw endpoint.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
)

// KeySize is the required length, in bytes, of the operator-provided
// data-encryption key (AES-256).
const KeySize = 32

var ErrInvalidKey = fmt.Errorf("encryption key must decode to exactly %d bytes", KeySize)

// ParseKey decodes an operator-provided LANSURFER_DATA_ENCRYPTION_KEY value
// (standard base64) into a raw key suitable for Encrypt/Decrypt/Fingerprint.
func ParseKey(value string) ([]byte, error) {
	key, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		return nil, fmt.Errorf("decode encryption key: %w", err)
	}
	if len(key) != KeySize {
		return nil, ErrInvalidKey
	}
	return key, nil
}

// Encrypt seals plaintext with AES-256-GCM under key, prepending a random
// nonce. Two calls with identical plaintext never produce identical output.
func Encrypt(key []byte, plaintext string) ([]byte, error) {
	aead, err := newAEAD(key)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("generate nonce: %w", err)
	}
	return aead.Seal(nonce, nonce, []byte(plaintext), nil), nil
}

// Decrypt reverses Encrypt. It returns an error if ciphertext was truncated
// or has been tampered with (GCM authentication failure).
func Decrypt(key []byte, ciphertext []byte) (string, error) {
	aead, err := newAEAD(key)
	if err != nil {
		return "", err
	}
	if len(ciphertext) < aead.NonceSize() {
		return "", errors.New("ciphertext is shorter than the nonce")
	}
	nonce, sealed := ciphertext[:aead.NonceSize()], ciphertext[aead.NonceSize():]
	plaintext, err := aead.Open(nil, nonce, sealed, nil)
	if err != nil {
		return "", fmt.Errorf("decrypt: %w", err)
	}
	return string(plaintext), nil
}

// Fingerprint derives a deterministic, keyed, non-reversible identifier for
// data (a Push endpoint URL) so a unique index can enforce "one endpoint
// belongs to at most one user" without ever storing the raw endpoint next to
// its own fingerprint.
func Fingerprint(key []byte, data string) string {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(data))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func newAEAD(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create AES cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create GCM: %w", err)
	}
	return aead, nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/platform/crypto/... -v`
Expected: PASS.

- [ ] **Step 5: Write the failing config tests**

Read `internal/platform/config/config_test.go` first (in particular `clearOptionalEnvironment` and the existing `loadIGDB`/`loadDiscord`-style tests) and append, following its exact style:

```go
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
```

Add these package-level test fixtures near the top of the file (real, valid values — generate them once with a short throwaway Go program using `crypto/ecdh` for the VAPID pair and `crypto/rand`+base64 for the data key, then hardcode the resulting strings here so the tests are deterministic and don't depend on runtime key generation):

```go
// Generated once for these tests only; not a real operator key.
const (
	validVAPIDPublicKeyForTests    = "<fill in with a real base64url-encoded P-256 public key, 65 raw bytes>"
	validVAPIDPrivateKeyForTests   = "<fill in with the matching base64url-encoded P-256 private scalar, 32 raw bytes>"
	validDataEncryptionKeyForTests = "<fill in with a real base64-encoded 32-byte key>"
)
```

To generate real values for the three placeholders above, run this once (throwaway, do not commit as a file — run it with `go run` from a scratch file or `go run -` if your shell supports heredocs, then paste the three printed strings into the constants and delete the scratch file):

```go
package main

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"fmt"
)

func main() {
	key, _ := ecdh.P256().GenerateKey(rand.Reader)
	fmt.Println("public:", base64.RawURLEncoding.EncodeToString(key.PublicKey().Bytes()))
	fmt.Println("private:", base64.RawURLEncoding.EncodeToString(key.Bytes()))
	dataKey := make([]byte, 32)
	rand.Read(dataKey)
	fmt.Println("data key:", base64.StdEncoding.EncodeToString(dataKey))
}
```

Update `clearOptionalEnvironment` in the same test file to also clear: `LANSURFER_WEBPUSH_PUBLIC_KEY`, `LANSURFER_WEBPUSH_PRIVATE_KEY`, `LANSURFER_WEBPUSH_SUBJECT`, `LANSURFER_DATA_ENCRYPTION_KEY`.

- [ ] **Step 6: Run the tests to verify they fail**

Run: `go test ./internal/platform/config/... -v -run TestLoadWebPush`
Expected: FAIL to compile — `cfg.WebPush`/`cfg.DataEncryptionKey` undefined.

- [ ] **Step 7: Implement the config additions**

Edit `internal/platform/config/config.go`. Add the import `"github.com/convention-x-treme/lansurfer/internal/platform/crypto"` and `"crypto/ecdh"`, add a `WebPush` type, add `WebPush *WebPush` and `DataEncryptionKey []byte` fields to `Config`, and wire loading exactly like the existing `loadIGDB`/`loadDiscord` calls:

```go
type WebPush struct {
	PublicKey  string
	PrivateKey string
	Subject    string
}
```

```go
func loadWebPush() (*WebPush, error) {
	publicKey := strings.TrimSpace(os.Getenv("LANSURFER_WEBPUSH_PUBLIC_KEY"))
	privateKey := strings.TrimSpace(os.Getenv("LANSURFER_WEBPUSH_PRIVATE_KEY"))
	if publicKey == "" && privateKey == "" {
		return nil, nil
	}
	if publicKey == "" || privateKey == "" {
		return nil, errors.New("LANSURFER_WEBPUSH_PUBLIC_KEY and LANSURFER_WEBPUSH_PRIVATE_KEY must be configured together")
	}
	if err := validateVAPIDKeyPair(publicKey, privateKey); err != nil {
		return nil, fmt.Errorf("invalid VAPID key configuration: %w", err)
	}
	subject := strings.TrimSpace(os.Getenv("LANSURFER_WEBPUSH_SUBJECT"))
	if subject == "" {
		return nil, errors.New("LANSURFER_WEBPUSH_SUBJECT is required when Web Push is configured")
	}
	if !strings.HasPrefix(subject, "mailto:") && !strings.HasPrefix(subject, "https://") {
		return nil, errors.New(`LANSURFER_WEBPUSH_SUBJECT must start with "mailto:" or "https://"`)
	}
	return &WebPush{PublicKey: publicKey, PrivateKey: privateKey, Subject: subject}, nil
}

func validateVAPIDKeyPair(publicKey, privateKey string) error {
	publicBytes, err := decodeVAPIDComponent(publicKey)
	if err != nil {
		return fmt.Errorf("public key: %w", err)
	}
	privateBytes, err := decodeVAPIDComponent(privateKey)
	if err != nil {
		return fmt.Errorf("private key: %w", err)
	}
	if _, err := ecdh.P256().NewPublicKey(publicBytes); err != nil {
		return fmt.Errorf("public key is not a valid VAPID (P-256) key: %w", err)
	}
	if _, err := ecdh.P256().NewPrivateKey(privateBytes); err != nil {
		return fmt.Errorf("private key is not a valid VAPID (P-256) key: %w", err)
	}
	return nil
}

func decodeVAPIDComponent(value string) ([]byte, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(value, "="))
	if err != nil {
		return nil, fmt.Errorf("not valid base64url: %w", err)
	}
	return decoded, nil
}

func loadDataEncryptionKey() ([]byte, error) {
	value := strings.TrimSpace(os.Getenv("LANSURFER_DATA_ENCRYPTION_KEY"))
	if value == "" {
		return nil, nil
	}
	key, err := crypto.ParseKey(value)
	if err != nil {
		return nil, fmt.Errorf("LANSURFER_DATA_ENCRYPTION_KEY: %w", err)
	}
	return key, nil
}
```

In `Load()`, after the existing `igdb, err := loadIGDB()` block, add:

```go
	webPush, err := loadWebPush()
	if err != nil {
		return Config{}, err
	}
	dataEncryptionKey, err := loadDataEncryptionKey()
	if err != nil {
		return Config{}, err
	}
	if webPush != nil && dataEncryptionKey == nil {
		return Config{}, errors.New("LANSURFER_DATA_ENCRYPTION_KEY is required when Web Push is configured")
	}
```

and add `WebPush: webPush, DataEncryptionKey: dataEncryptionKey,` to the returned `Config{...}` literal. Add `"encoding/base64"` to the import block if not already present (it is not, in this file).

- [ ] **Step 8: Run the tests to verify they pass**

Run: `go test ./internal/platform/config/... -v` and `go test ./internal/platform/crypto/... -v`
Expected: PASS for all tests, old and new.

- [ ] **Step 9: Commit**

```bash
git add internal/platform/crypto internal/platform/config/config.go internal/platform/config/config_test.go
git commit -m "feat(platform): add data-encryption primitives and Web Push/VAPID configuration"
```

---

### Task 2: Schema and domain types for push subscriptions/preferences

**Files:**
- Create: `internal/platform/postgres/migrations/00012_notification_push_subscriptions.sql`
- Create: `internal/notification/postgres/queries/push_subscriptions.sql`
- Modify: `internal/notification/notification.go`
- Modify: `sqlc.yaml` (no change needed — the existing `internal/notification/postgres/queries` entry already globs this new file; confirm this, don't add a duplicate entry)

**Interfaces:**
- Produces: `PushSubscriptionRecord`, `PushPreference`, `SubscriptionRepository`, `PreferenceRepository` — consumed by Task 3's `Store` and Task 4's `SubscriptionService`. Generated `sqlcgen` types for Task 3.

- [ ] **Step 1: Write the migration**

Create `internal/platform/postgres/migrations/00012_notification_push_subscriptions.sql`:

```sql
-- +goose Up
-- One row per LANsurfer user's Web Push preferences. Absent row means the
-- defaults below (disabled, followed-only audience); a row is created only
-- when a user actually changes something, not eagerly for every account.
CREATE TABLE notification_push_preference (
    user_id uuid PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    web_push_enabled boolean NOT NULL DEFAULT false,
    new_request_audience text NOT NULL DEFAULT 'followed'
        CHECK (new_request_audience IN ('followed', 'all')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

-- One row per active browser Push subscription. endpoint/p256dh/auth are
-- stored only as AES-GCM ciphertext (see internal/platform/crypto); the
-- unique endpoint_fingerprint (a keyed HMAC of the plaintext endpoint) is
-- what lets "one endpoint belongs to at most one user" be enforced by a
-- constraint without ever persisting the raw endpoint.
CREATE TABLE notification_push_subscription (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    label text NOT NULL,
    device_id_hash text NOT NULL,
    endpoint_fingerprint text NOT NULL UNIQUE,
    endpoint_ciphertext bytea NOT NULL,
    p256dh_ciphertext bytea NOT NULL,
    auth_ciphertext bytea NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX notification_push_subscription_user_idx ON notification_push_subscription (user_id);
CREATE INDEX notification_push_subscription_device_idx ON notification_push_subscription (user_id, device_id_hash);

-- +goose Down
DROP TABLE notification_push_subscription;
DROP TABLE notification_push_preference;
```

- [ ] **Step 2: Write the sqlc queries**

Create `internal/notification/postgres/queries/push_subscriptions.sql`:

```sql
-- name: UpsertPushSubscription :one
INSERT INTO notification_push_subscription
    (user_id, label, device_id_hash, endpoint_fingerprint, endpoint_ciphertext, p256dh_ciphertext, auth_ciphertext)
VALUES (
    sqlc.arg(user_id)::uuid,
    sqlc.arg(label),
    sqlc.arg(device_id_hash),
    sqlc.arg(endpoint_fingerprint),
    sqlc.arg(endpoint_ciphertext),
    sqlc.arg(p256dh_ciphertext),
    sqlc.arg(auth_ciphertext)
)
ON CONFLICT (endpoint_fingerprint) DO UPDATE SET
    user_id = EXCLUDED.user_id,
    label = EXCLUDED.label,
    device_id_hash = EXCLUDED.device_id_hash,
    endpoint_ciphertext = EXCLUDED.endpoint_ciphertext,
    p256dh_ciphertext = EXCLUDED.p256dh_ciphertext,
    auth_ciphertext = EXCLUDED.auth_ciphertext,
    updated_at = now()
RETURNING id, created_at, updated_at;

-- name: ListPushSubscriptionsByUser :many
SELECT id, label, device_id_hash, endpoint_fingerprint, created_at, updated_at
FROM notification_push_subscription
WHERE user_id = sqlc.arg(user_id)::uuid
ORDER BY created_at;

-- name: DeletePushSubscription :execrows
DELETE FROM notification_push_subscription
WHERE id = sqlc.arg(id) AND user_id = sqlc.arg(user_id)::uuid;

-- name: DeletePushSubscriptionByDeviceHash :execrows
DELETE FROM notification_push_subscription
WHERE user_id = sqlc.arg(user_id)::uuid AND device_id_hash = sqlc.arg(device_id_hash);

-- name: GetPushPreference :one
SELECT web_push_enabled, new_request_audience
FROM notification_push_preference
WHERE user_id = sqlc.arg(user_id)::uuid;

-- name: UpsertPushPreference :exec
INSERT INTO notification_push_preference (user_id, web_push_enabled, new_request_audience)
VALUES (sqlc.arg(user_id)::uuid, sqlc.arg(web_push_enabled), sqlc.arg(new_request_audience))
ON CONFLICT (user_id) DO UPDATE SET
    web_push_enabled = EXCLUDED.web_push_enabled,
    new_request_audience = EXCLUDED.new_request_audience,
    updated_at = now();
```

Note deliberately excluded from `ListPushSubscriptionsByUser`: `endpoint_ciphertext`/`p256dh_ciphertext`/`auth_ciphertext`. The settings page only ever needs id/label/device/timestamps; do not select ciphertext columns that are never used outside the enable flow and the (not-yet-built) Delivery plan's send path.

- [ ] **Step 3: Add the domain types and interfaces**

Edit `internal/notification/notification.go`, adding near the existing `DiagnosticsRepository` interface:

```go
// PushSubscriptionRecord is one stored browser Push subscription.
// EndpointCiphertext/P256dhCiphertext/AuthCiphertext are opaque AES-GCM
// ciphertext: Store never sees, and this type never carries, plaintext
// subscription material. Only SubscriptionService encrypts/decrypts it.
type PushSubscriptionRecord struct {
	ID                  int64
	UserID              string
	Label               string
	DeviceIDHash        string
	EndpointFingerprint string
	EndpointCiphertext  []byte
	P256dhCiphertext    []byte
	AuthCiphertext      []byte
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

// PushPreference is one user's Web Push preferences. The zero value is the
// correct default for a user who has never touched Push settings: disabled,
// followed-only audience.
type PushPreference struct {
	WebPushEnabled     bool
	NewRequestAudience string // "followed" or "all"
}

const (
	AudienceFollowed = "followed"
	AudienceAll      = "all"
)

// SubscriptionRepository is the durable state SubscriptionService needs for
// managing one user's Push subscriptions.
type SubscriptionRepository interface {
	// UpsertSubscription persists sub, atomically reassigning ownership if
	// EndpointFingerprint already belongs to a different user. Only
	// UserID/Label/DeviceIDHash/EndpointFingerprint/EndpointCiphertext/
	// P256dhCiphertext/AuthCiphertext are read from sub; ID/CreatedAt/
	// UpdatedAt are populated in the returned record.
	UpsertSubscription(ctx context.Context, sub PushSubscriptionRecord) (PushSubscriptionRecord, error)
	ListSubscriptions(ctx context.Context, userID string) ([]PushSubscriptionRecord, error)
	// DeleteSubscription removes one subscription scoped to its owner; it
	// reports whether a row was actually deleted.
	DeleteSubscription(ctx context.Context, subscriptionID int64, userID string) (bool, error)
	DeleteSubscriptionByDeviceHash(ctx context.Context, userID, deviceIDHash string) (bool, error)
}

// PreferenceRepository is the durable state SubscriptionService needs for
// one user's Web Push preferences.
type PreferenceRepository interface {
	// GetPreference returns the zero-value PushPreference (disabled,
	// followed-only) when the user has no stored row yet.
	GetPreference(ctx context.Context, userID string) (PushPreference, error)
	SetPreference(ctx context.Context, userID string, preference PushPreference) error
}
```

- [ ] **Step 4: Generate and confirm it compiles**

Run: `task generate`
Expected: succeeds; `internal/notification/postgres/sqlcgen` gains `UpsertPushSubscription`, `ListPushSubscriptionsByUser`, `DeletePushSubscription`, `DeletePushSubscriptionByDeviceHash`, `GetPushPreference`, `UpsertPushPreference` and their `Params`/`Row` types. Read the generated file afterward and note the exact field names for Task 3 (they follow the same `sqlc.arg(name)::uuid` → PascalCase pattern already used throughout `internal/notification/postgres/queries/notification.sql`).

Then run: `go build ./...`
Expected: succeeds.

- [ ] **Step 5: Commit**

```bash
git add internal/platform/postgres/migrations/00012_notification_push_subscriptions.sql \
        internal/notification/postgres/queries/push_subscriptions.sql \
        internal/notification/postgres/sqlcgen \
        internal/notification/notification.go
git commit -m "feat(notification): add push subscription/preference schema and domain types"
```

---

### Task 3: Postgres store for subscriptions and preferences

**Files:**
- Modify: `internal/notification/postgres/store.go`
- Test: `internal/platform/postgres/notification_push_subscription_integration_test.go` (new file, package `postgres`, `//go:build integration`)

**Interfaces:**
- Consumes: `sqlcgen` types from Task 2.
- Produces: `Store` satisfying `notification.SubscriptionRepository` and `notification.PreferenceRepository`, consumed by Task 4's `SubscriptionService`.

- [ ] **Step 1: Write the failing integration tests**

Read `internal/platform/postgres/notification_integration_test.go` first for the existing `newNotificationFixture`/`newTestPool`/`testLogger` helpers this new file reuses (do not redefine them — they live in the `postgres` package already).

Create `internal/platform/postgres/notification_push_subscription_integration_test.go`:

```go
//go:build integration

package postgres

import (
	"testing"

	notificationpostgres "github.com/convention-x-treme/lansurfer/internal/notification/postgres"
	"github.com/convention-x-treme/lansurfer/internal/user"
	userpostgres "github.com/convention-x-treme/lansurfer/internal/user/postgres"
	"github.com/convention-x-treme/lansurfer/internal/notification"
)

func TestPushSubscriptionUpsertCreatesThenUpdatesInPlace(t *testing.T) {
	pool := newTestPool(t)
	if err := Migrate(t.Context(), pool, testLogger()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	users := user.NewService(userpostgres.NewStore(pool))
	owner, err := users.Register(t.Context(), "Push Owner")
	if err != nil {
		t.Fatalf("register user: %v", err)
	}
	store := notificationpostgres.NewStore(pool)

	created, err := store.UpsertSubscription(t.Context(), notification.PushSubscriptionRecord{
		UserID: owner.ID, Label: "This device", DeviceIDHash: "device-hash-1",
		EndpointFingerprint: "fingerprint-1", EndpointCiphertext: []byte("endpoint-cipher-1"),
		P256dhCiphertext: []byte("p256dh-1"), AuthCiphertext: []byte("auth-1"),
	})
	if err != nil {
		t.Fatalf("UpsertSubscription() create error = %v", err)
	}
	if created.ID == 0 {
		t.Fatal("UpsertSubscription() returned a zero ID")
	}

	updated, err := store.UpsertSubscription(t.Context(), notification.PushSubscriptionRecord{
		UserID: owner.ID, Label: "Renamed device", DeviceIDHash: "device-hash-1",
		EndpointFingerprint: "fingerprint-1", EndpointCiphertext: []byte("endpoint-cipher-2"),
		P256dhCiphertext: []byte("p256dh-2"), AuthCiphertext: []byte("auth-2"),
	})
	if err != nil {
		t.Fatalf("UpsertSubscription() update error = %v", err)
	}
	if updated.ID != created.ID {
		t.Fatalf("UpsertSubscription() on the same fingerprint changed the row ID: %d != %d", updated.ID, created.ID)
	}

	subs, err := store.ListSubscriptions(t.Context(), owner.ID)
	if err != nil || len(subs) != 1 || subs[0].Label != "Renamed device" {
		t.Fatalf("ListSubscriptions() = %#v, %v, want exactly one row labeled %q", subs, err, "Renamed device")
	}
}

func TestPushSubscriptionUpsertReassignsOwnershipAcrossUsers(t *testing.T) {
	pool := newTestPool(t)
	if err := Migrate(t.Context(), pool, testLogger()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	users := user.NewService(userpostgres.NewStore(pool))
	first, err := users.Register(t.Context(), "First Owner")
	if err != nil {
		t.Fatalf("register first user: %v", err)
	}
	second, err := users.Register(t.Context(), "Second Owner")
	if err != nil {
		t.Fatalf("register second user: %v", err)
	}
	store := notificationpostgres.NewStore(pool)

	if _, err := store.UpsertSubscription(t.Context(), notification.PushSubscriptionRecord{
		UserID: first.ID, Label: "First's device", DeviceIDHash: "device-hash-shared",
		EndpointFingerprint: "shared-fingerprint", EndpointCiphertext: []byte("c1"),
		P256dhCiphertext: []byte("p1"), AuthCiphertext: []byte("a1"),
	}); err != nil {
		t.Fatalf("UpsertSubscription() for first owner error = %v", err)
	}

	if _, err := store.UpsertSubscription(t.Context(), notification.PushSubscriptionRecord{
		UserID: second.ID, Label: "Second's device", DeviceIDHash: "device-hash-shared",
		EndpointFingerprint: "shared-fingerprint", EndpointCiphertext: []byte("c2"),
		P256dhCiphertext: []byte("p2"), AuthCiphertext: []byte("a2"),
	}); err != nil {
		t.Fatalf("UpsertSubscription() reassigning to second owner error = %v", err)
	}

	firstSubs, err := store.ListSubscriptions(t.Context(), first.ID)
	if err != nil || len(firstSubs) != 0 {
		t.Fatalf("ListSubscriptions(first) = %#v, %v, want none (reassigned away)", firstSubs, err)
	}
	secondSubs, err := store.ListSubscriptions(t.Context(), second.ID)
	if err != nil || len(secondSubs) != 1 || secondSubs[0].Label != "Second's device" {
		t.Fatalf("ListSubscriptions(second) = %#v, %v, want exactly the reassigned row", secondSubs, err)
	}
}

func TestPushSubscriptionDeleteIsScopedToOwner(t *testing.T) {
	pool := newTestPool(t)
	if err := Migrate(t.Context(), pool, testLogger()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	users := user.NewService(userpostgres.NewStore(pool))
	owner, err := users.Register(t.Context(), "Delete Owner")
	if err != nil {
		t.Fatalf("register owner: %v", err)
	}
	intruder, err := users.Register(t.Context(), "Delete Intruder")
	if err != nil {
		t.Fatalf("register intruder: %v", err)
	}
	store := notificationpostgres.NewStore(pool)
	sub, err := store.UpsertSubscription(t.Context(), notification.PushSubscriptionRecord{
		UserID: owner.ID, Label: "Owned device", DeviceIDHash: "device-hash-2",
		EndpointFingerprint: "fingerprint-2", EndpointCiphertext: []byte("c"),
		P256dhCiphertext: []byte("p"), AuthCiphertext: []byte("a"),
	})
	if err != nil {
		t.Fatalf("UpsertSubscription() error = %v", err)
	}

	deleted, err := store.DeleteSubscription(t.Context(), sub.ID, intruder.ID)
	if err != nil || deleted {
		t.Fatalf("DeleteSubscription() by a non-owner = %v, %v, want false, nil", deleted, err)
	}
	deleted, err = store.DeleteSubscription(t.Context(), sub.ID, owner.ID)
	if err != nil || !deleted {
		t.Fatalf("DeleteSubscription() by the owner = %v, %v, want true, nil", deleted, err)
	}
	subs, err := store.ListSubscriptions(t.Context(), owner.ID)
	if err != nil || len(subs) != 0 {
		t.Fatalf("ListSubscriptions() after delete = %#v, %v, want none", subs, err)
	}
}

func TestPushSubscriptionDeleteByDeviceHash(t *testing.T) {
	pool := newTestPool(t)
	if err := Migrate(t.Context(), pool, testLogger()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	users := user.NewService(userpostgres.NewStore(pool))
	owner, err := users.Register(t.Context(), "Logout Owner")
	if err != nil {
		t.Fatalf("register owner: %v", err)
	}
	store := notificationpostgres.NewStore(pool)
	if _, err := store.UpsertSubscription(t.Context(), notification.PushSubscriptionRecord{
		UserID: owner.ID, Label: "This device", DeviceIDHash: "device-hash-3",
		EndpointFingerprint: "fingerprint-3", EndpointCiphertext: []byte("c"),
		P256dhCiphertext: []byte("p"), AuthCiphertext: []byte("a"),
	}); err != nil {
		t.Fatalf("UpsertSubscription() error = %v", err)
	}

	deleted, err := store.DeleteSubscriptionByDeviceHash(t.Context(), owner.ID, "device-hash-3")
	if err != nil || !deleted {
		t.Fatalf("DeleteSubscriptionByDeviceHash() = %v, %v, want true, nil", deleted, err)
	}
	deleted, err = store.DeleteSubscriptionByDeviceHash(t.Context(), owner.ID, "device-hash-3")
	if err != nil || deleted {
		t.Fatalf("second DeleteSubscriptionByDeviceHash() = %v, %v, want false, nil (already gone)", deleted, err)
	}
}

func TestPushPreferenceDefaultsToDisabledFollowedWhenUnset(t *testing.T) {
	pool := newTestPool(t)
	if err := Migrate(t.Context(), pool, testLogger()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	users := user.NewService(userpostgres.NewStore(pool))
	owner, err := users.Register(t.Context(), "Preference Owner")
	if err != nil {
		t.Fatalf("register owner: %v", err)
	}
	store := notificationpostgres.NewStore(pool)

	preference, err := store.GetPreference(t.Context(), owner.ID)
	if err != nil {
		t.Fatalf("GetPreference() error = %v", err)
	}
	if preference.WebPushEnabled || preference.NewRequestAudience != notification.AudienceFollowed {
		t.Fatalf("GetPreference() default = %#v, want disabled/followed", preference)
	}

	if err := store.SetPreference(t.Context(), owner.ID, notification.PushPreference{
		WebPushEnabled: true, NewRequestAudience: notification.AudienceAll,
	}); err != nil {
		t.Fatalf("SetPreference() error = %v", err)
	}
	preference, err = store.GetPreference(t.Context(), owner.ID)
	if err != nil || !preference.WebPushEnabled || preference.NewRequestAudience != notification.AudienceAll {
		t.Fatalf("GetPreference() after SetPreference() = %#v, %v, want enabled/all", preference, err)
	}

	if err := store.SetPreference(t.Context(), owner.ID, notification.PushPreference{
		WebPushEnabled: false, NewRequestAudience: notification.AudienceAll,
	}); err != nil {
		t.Fatalf("second SetPreference() error = %v", err)
	}
	preference, err = store.GetPreference(t.Context(), owner.ID)
	if err != nil || preference.WebPushEnabled || preference.NewRequestAudience != notification.AudienceAll {
		t.Fatalf("GetPreference() after disabling = %#v, %v, want disabled/all", preference, err)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Start the isolated test database if not already running: `docker compose -p lansurfer-test -f compose.test.yaml up -d --wait`

Run: `LANSURFER_TEST_DATABASE_URL='postgres://lansurfer_test:lansurfer_test@127.0.0.1:55432/lansurfer_test?sslmode=disable' go test -race -tags=integration ./internal/platform/postgres -run TestPushSubscription -v` and `-run TestPushPreference`
Expected: FAIL — `Store.UpsertSubscription` etc. undefined.

- [ ] **Step 3: Implement the store methods**

Add to `internal/notification/postgres/store.go` (reuse the existing `parseUUID` helper already in this file from Track E1):

```go
func (s *Store) UpsertSubscription(ctx context.Context, sub notification.PushSubscriptionRecord) (notification.PushSubscriptionRecord, error) {
	userID, err := parseUUID(sub.UserID)
	if err != nil {
		return notification.PushSubscriptionRecord{}, fmt.Errorf("parse user ID: %w", err)
	}
	row, err := s.queries.UpsertPushSubscription(ctx, sqlcgen.UpsertPushSubscriptionParams{
		UserID: userID, Label: sub.Label, DeviceIDHash: sub.DeviceIDHash,
		EndpointFingerprint: sub.EndpointFingerprint, EndpointCiphertext: sub.EndpointCiphertext,
		P256dhCiphertext: sub.P256dhCiphertext, AuthCiphertext: sub.AuthCiphertext,
	})
	if err != nil {
		return notification.PushSubscriptionRecord{}, fmt.Errorf("upsert push subscription: %w", err)
	}
	sub.ID, sub.CreatedAt, sub.UpdatedAt = row.ID, row.CreatedAt.Time, row.UpdatedAt.Time
	return sub, nil
}

func (s *Store) ListSubscriptions(ctx context.Context, userID string) ([]notification.PushSubscriptionRecord, error) {
	parsedUserID, err := parseUUID(userID)
	if err != nil {
		return nil, fmt.Errorf("parse user ID: %w", err)
	}
	rows, err := s.queries.ListPushSubscriptionsByUser(ctx, parsedUserID)
	if err != nil {
		return nil, fmt.Errorf("list push subscriptions: %w", err)
	}
	subs := make([]notification.PushSubscriptionRecord, 0, len(rows))
	for _, row := range rows {
		subs = append(subs, notification.PushSubscriptionRecord{
			ID: row.ID, UserID: userID, Label: row.Label, DeviceIDHash: row.DeviceIDHash,
			EndpointFingerprint: row.EndpointFingerprint, CreatedAt: row.CreatedAt.Time, UpdatedAt: row.UpdatedAt.Time,
		})
	}
	return subs, nil
}

func (s *Store) DeleteSubscription(ctx context.Context, subscriptionID int64, userID string) (bool, error) {
	parsedUserID, err := parseUUID(userID)
	if err != nil {
		return false, fmt.Errorf("parse user ID: %w", err)
	}
	deleted, err := s.queries.DeletePushSubscription(ctx, sqlcgen.DeletePushSubscriptionParams{
		ID: subscriptionID, UserID: parsedUserID,
	})
	if err != nil {
		return false, fmt.Errorf("delete push subscription: %w", err)
	}
	return deleted > 0, nil
}

func (s *Store) DeleteSubscriptionByDeviceHash(ctx context.Context, userID, deviceIDHash string) (bool, error) {
	parsedUserID, err := parseUUID(userID)
	if err != nil {
		return false, fmt.Errorf("parse user ID: %w", err)
	}
	deleted, err := s.queries.DeletePushSubscriptionByDeviceHash(ctx, sqlcgen.DeletePushSubscriptionByDeviceHashParams{
		UserID: parsedUserID, DeviceIDHash: deviceIDHash,
	})
	if err != nil {
		return false, fmt.Errorf("delete push subscription by device hash: %w", err)
	}
	return deleted > 0, nil
}

func (s *Store) GetPreference(ctx context.Context, userID string) (notification.PushPreference, error) {
	parsedUserID, err := parseUUID(userID)
	if err != nil {
		return notification.PushPreference{}, fmt.Errorf("parse user ID: %w", err)
	}
	row, err := s.queries.GetPushPreference(ctx, parsedUserID)
	if errors.Is(err, pgx.ErrNoRows) {
		return notification.PushPreference{NewRequestAudience: notification.AudienceFollowed}, nil
	}
	if err != nil {
		return notification.PushPreference{}, fmt.Errorf("get push preference: %w", err)
	}
	return notification.PushPreference{WebPushEnabled: row.WebPushEnabled, NewRequestAudience: row.NewRequestAudience}, nil
}

func (s *Store) SetPreference(ctx context.Context, userID string, preference notification.PushPreference) error {
	parsedUserID, err := parseUUID(userID)
	if err != nil {
		return fmt.Errorf("parse user ID: %w", err)
	}
	if err := s.queries.UpsertPushPreference(ctx, sqlcgen.UpsertPushPreferenceParams{
		UserID: parsedUserID, WebPushEnabled: preference.WebPushEnabled, NewRequestAudience: preference.NewRequestAudience,
	}); err != nil {
		return fmt.Errorf("set push preference: %w", err)
	}
	return nil
}
```

Add the import `"github.com/convention-x-treme/lansurfer/internal/notification"` to `store.go` if not already present (Track E1's `store.go` already imports it).

Adjust field names to whatever Task 2 Step 4 actually generated if they differ from the names used above.

- [ ] **Step 4: Run the tests to verify they pass**

Run the same commands as Step 2.
Expected: PASS for all six tests.

- [ ] **Step 5: Commit**

```bash
git add internal/notification/postgres/store.go internal/platform/postgres/notification_push_subscription_integration_test.go
git commit -m "feat(notification): add push subscription and preference persistence"
```

---

### Task 4: SubscriptionService — encryption, enable/disable, device management

**Files:**
- Create: `internal/notification/subscription.go`
- Create: `internal/notification/subscription_test.go`

**Interfaces:**
- Consumes: `SubscriptionRepository`, `PreferenceRepository` from Task 2/3; `internal/platform/crypto` from Task 1.
- Produces: `SubscriptionService`, `NewSubscriptionService(subscriptions SubscriptionRepository, preferences PreferenceRepository, encryptionKey []byte) *SubscriptionService`, `Enable`, `Disable`, `SetAudience`, `RemoveDevice`, `RemoveDeviceByHash`, `ListDevices`, `Preference` — consumed by Task 5's HTTP handler.

- [ ] **Step 1: Write the failing unit tests**

Create `internal/notification/subscription_test.go`:

```go
package notification

import (
	"context"
	"errors"
	"testing"
)

type fakeSubscriptionRepository struct {
	upserted PushSubscriptionRecord
	upsertErr error
	subs     []PushSubscriptionRecord
	deleted  map[int64]bool
	deleteByHashCalled bool
}

func (f *fakeSubscriptionRepository) UpsertSubscription(ctx context.Context, sub PushSubscriptionRecord) (PushSubscriptionRecord, error) {
	f.upserted = sub
	if f.upsertErr != nil {
		return PushSubscriptionRecord{}, f.upsertErr
	}
	sub.ID = 1
	return sub, nil
}

func (f *fakeSubscriptionRepository) ListSubscriptions(context.Context, string) ([]PushSubscriptionRecord, error) {
	return f.subs, nil
}

func (f *fakeSubscriptionRepository) DeleteSubscription(ctx context.Context, id int64, userID string) (bool, error) {
	if f.deleted == nil {
		f.deleted = map[int64]bool{}
	}
	f.deleted[id] = true
	return true, nil
}

func (f *fakeSubscriptionRepository) DeleteSubscriptionByDeviceHash(context.Context, string, string) (bool, error) {
	f.deleteByHashCalled = true
	return true, nil
}

type fakePreferenceRepository struct {
	preference PushPreference
	setCalls   []PushPreference
}

func (f *fakePreferenceRepository) GetPreference(context.Context, string) (PushPreference, error) {
	return f.preference, nil
}

func (f *fakePreferenceRepository) SetPreference(ctx context.Context, userID string, preference PushPreference) error {
	f.setCalls = append(f.setCalls, preference)
	f.preference = preference
	return nil
}

func testEncryptionKey() []byte { return make([]byte, 32) }

func TestSubscriptionServiceEnablePersistsEncryptedSubscriptionAndSetsEnabled(t *testing.T) {
	subs := &fakeSubscriptionRepository{}
	prefs := &fakePreferenceRepository{}
	service := NewSubscriptionService(subs, prefs, testEncryptionKey())

	if err := service.Enable(t.Context(), "user-1", "device-hash-1", "This device", "https://push.example/endpoint", "p256dh-value", "auth-value"); err != nil {
		t.Fatalf("Enable() error = %v", err)
	}
	if subs.upserted.UserID != "user-1" || subs.upserted.Label != "This device" || subs.upserted.DeviceIDHash != "device-hash-1" {
		t.Fatalf("upserted subscription = %#v, want the given identity fields", subs.upserted)
	}
	if len(subs.upserted.EndpointCiphertext) == 0 || string(subs.upserted.EndpointCiphertext) == "https://push.example/endpoint" {
		t.Fatal("Enable() stored the endpoint in plaintext, want ciphertext")
	}
	if subs.upserted.EndpointFingerprint == "" {
		t.Fatal("Enable() did not compute an endpoint fingerprint")
	}
	if len(prefs.setCalls) != 1 || !prefs.setCalls[0].WebPushEnabled {
		t.Fatalf("SetPreference() calls = %#v, want exactly one call enabling Web Push", prefs.setCalls)
	}
}

func TestSubscriptionServiceEnablePreservesExistingAudiencePreference(t *testing.T) {
	subs := &fakeSubscriptionRepository{}
	prefs := &fakePreferenceRepository{preference: PushPreference{WebPushEnabled: false, NewRequestAudience: AudienceAll}}
	service := NewSubscriptionService(subs, prefs, testEncryptionKey())

	if err := service.Enable(t.Context(), "user-1", "device-hash-1", "This device", "https://push.example/endpoint", "p256dh-value", "auth-value"); err != nil {
		t.Fatalf("Enable() error = %v", err)
	}
	if len(prefs.setCalls) != 1 || prefs.setCalls[0].NewRequestAudience != AudienceAll {
		t.Fatalf("SetPreference() call = %#v, want the pre-existing audience preference kept", prefs.setCalls)
	}
}

func TestSubscriptionServiceDisableKeepsSubscriptionsAndAudience(t *testing.T) {
	subs := &fakeSubscriptionRepository{subs: []PushSubscriptionRecord{{ID: 1, Label: "This device"}}}
	prefs := &fakePreferenceRepository{preference: PushPreference{WebPushEnabled: true, NewRequestAudience: AudienceAll}}
	service := NewSubscriptionService(subs, prefs, testEncryptionKey())

	if err := service.Disable(t.Context(), "user-1"); err != nil {
		t.Fatalf("Disable() error = %v", err)
	}
	if len(prefs.setCalls) != 1 || prefs.setCalls[0].WebPushEnabled || prefs.setCalls[0].NewRequestAudience != AudienceAll {
		t.Fatalf("SetPreference() call = %#v, want disabled with audience unchanged", prefs.setCalls)
	}
	devices, err := service.ListDevices(t.Context(), "user-1")
	if err != nil || len(devices) != 1 {
		t.Fatalf("ListDevices() after Disable() = %#v, %v, want the subscription kept", devices, err)
	}
}

func TestSubscriptionServiceRemoveDeviceDoesNotChangeEnabledPreference(t *testing.T) {
	subs := &fakeSubscriptionRepository{}
	prefs := &fakePreferenceRepository{preference: PushPreference{WebPushEnabled: true, NewRequestAudience: AudienceFollowed}}
	service := NewSubscriptionService(subs, prefs, testEncryptionKey())

	if err := service.RemoveDevice(t.Context(), "user-1", 1); err != nil {
		t.Fatalf("RemoveDevice() error = %v", err)
	}
	if len(prefs.setCalls) != 0 {
		t.Fatalf("RemoveDevice() called SetPreference() %d times, want 0 (removing a device must not touch web_push_enabled)", len(prefs.setCalls))
	}
	if !subs.deleted[1] {
		t.Fatal("RemoveDevice() did not delete the subscription")
	}
}

func TestSubscriptionServiceSetAudienceRejectsUnknownValue(t *testing.T) {
	subs := &fakeSubscriptionRepository{}
	prefs := &fakePreferenceRepository{}
	service := NewSubscriptionService(subs, prefs, testEncryptionKey())

	if err := service.SetAudience(t.Context(), "user-1", "everyone"); !errors.Is(err, ErrInvalidAudience) {
		t.Fatalf("SetAudience() with an unknown value error = %v, want %v", err, ErrInvalidAudience)
	}
	if len(prefs.setCalls) != 0 {
		t.Fatal("SetAudience() with an invalid value must not call SetPreference()")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/notification/... -run TestSubscriptionService -v`
Expected: FAIL to compile — `NewSubscriptionService` undefined.

- [ ] **Step 3: Implement SubscriptionService**

Create `internal/notification/subscription.go`:

```go
package notification

import (
	"context"
	"errors"
	"fmt"

	platformcrypto "github.com/convention-x-treme/lansurfer/internal/platform/crypto"
)

var ErrInvalidAudience = errors.New("audience must be \"followed\" or \"all\"")

// SubscriptionService is the only place plaintext Push subscription material
// and the data-encryption key meet: it encrypts on the way into
// SubscriptionRepository and never decrypts, since the settings surface
// never displays raw endpoint/p256dh/auth. It also owns the product rules
// around enable/disable/device removal that keep "web_push_enabled",
// "has at least one device", and "audience preference" independent states.
type SubscriptionService struct {
	subscriptions SubscriptionRepository
	preferences   PreferenceRepository
	encryptionKey []byte
}

func NewSubscriptionService(subscriptions SubscriptionRepository, preferences PreferenceRepository, encryptionKey []byte) *SubscriptionService {
	return &SubscriptionService{subscriptions: subscriptions, preferences: preferences, encryptionKey: encryptionKey}
}

// Enable runs the complete enable sequence for one browser subscription:
// persist (or reassign) it, and set web_push_enabled=true while preserving
// whatever audience preference the user already had.
func (s *SubscriptionService) Enable(ctx context.Context, userID, deviceIDHash, label, endpoint, p256dh, auth string) error {
	endpointCiphertext, err := platformcrypto.Encrypt(s.encryptionKey, endpoint)
	if err != nil {
		return fmt.Errorf("encrypt endpoint: %w", err)
	}
	p256dhCiphertext, err := platformcrypto.Encrypt(s.encryptionKey, p256dh)
	if err != nil {
		return fmt.Errorf("encrypt p256dh: %w", err)
	}
	authCiphertext, err := platformcrypto.Encrypt(s.encryptionKey, auth)
	if err != nil {
		return fmt.Errorf("encrypt auth: %w", err)
	}
	fingerprint := platformcrypto.Fingerprint(s.encryptionKey, endpoint)

	if _, err := s.subscriptions.UpsertSubscription(ctx, PushSubscriptionRecord{
		UserID: userID, Label: label, DeviceIDHash: deviceIDHash, EndpointFingerprint: fingerprint,
		EndpointCiphertext: endpointCiphertext, P256dhCiphertext: p256dhCiphertext, AuthCiphertext: authCiphertext,
	}); err != nil {
		return fmt.Errorf("upsert push subscription: %w", err)
	}

	current, err := s.preferences.GetPreference(ctx, userID)
	if err != nil {
		return fmt.Errorf("get push preference: %w", err)
	}
	current.WebPushEnabled = true
	return s.preferences.SetPreference(ctx, userID, current)
}

// Disable pauses Push delivery without deleting any device subscription, so
// re-enabling can reuse them.
func (s *SubscriptionService) Disable(ctx context.Context, userID string) error {
	current, err := s.preferences.GetPreference(ctx, userID)
	if err != nil {
		return fmt.Errorf("get push preference: %w", err)
	}
	current.WebPushEnabled = false
	return s.preferences.SetPreference(ctx, userID, current)
}

// SetAudience updates only the new-request audience preference, leaving
// web_push_enabled untouched.
func (s *SubscriptionService) SetAudience(ctx context.Context, userID, audience string) error {
	if audience != AudienceFollowed && audience != AudienceAll {
		return ErrInvalidAudience
	}
	current, err := s.preferences.GetPreference(ctx, userID)
	if err != nil {
		return fmt.Errorf("get push preference: %w", err)
	}
	current.NewRequestAudience = audience
	return s.preferences.SetPreference(ctx, userID, current)
}

// RemoveDevice deletes one subscription the caller owns. It never changes
// web_push_enabled: "enabled with zero devices" is a valid, distinct state.
func (s *SubscriptionService) RemoveDevice(ctx context.Context, userID string, subscriptionID int64) error {
	if _, err := s.subscriptions.DeleteSubscription(ctx, subscriptionID, userID); err != nil {
		return fmt.Errorf("delete push subscription: %w", err)
	}
	return nil
}

// RemoveDeviceByHash deletes the caller's subscription for one browser
// device, identified by the opaque device cookie's hash. Used by logout.
func (s *SubscriptionService) RemoveDeviceByHash(ctx context.Context, userID, deviceIDHash string) error {
	if _, err := s.subscriptions.DeleteSubscriptionByDeviceHash(ctx, userID, deviceIDHash); err != nil {
		return fmt.Errorf("delete push subscription by device hash: %w", err)
	}
	return nil
}

func (s *SubscriptionService) ListDevices(ctx context.Context, userID string) ([]PushSubscriptionRecord, error) {
	return s.subscriptions.ListSubscriptions(ctx, userID)
}

func (s *SubscriptionService) Preference(ctx context.Context, userID string) (PushPreference, error) {
	return s.preferences.GetPreference(ctx, userID)
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/notification/... -v`
Expected: PASS for every test in the package, old and new.

- [ ] **Step 5: Commit**

```bash
git add internal/notification/subscription.go internal/notification/subscription_test.go
git commit -m "feat(notification): add SubscriptionService for enable/disable/device management"
```

---

### Task 5: Device cookie, HTTP handler, settings page, composition-root wiring, logout hook

**Files:**
- Create: `internal/web/pages/notification_settings.templ` (+ generated `_templ.go`)
- Create: `internal/web/handler/notification_settings.go`
- Create: `internal/web/handler/notification_settings_test.go`
- Modify: `internal/web/middleware/authentication.go` (device cookie helpers, alongside the existing `SessionCookie`/`ExpiredSessionCookie`)
- Modify: `internal/web/router.go`
- Modify: `internal/web/pages/account.templ` (link to the new settings page)
- Modify: `internal/web/handler/authentication.go` (logout hook)
- Modify: `internal/app/app.go` and/or a new `internal/app/notification.go` addition (wire `SubscriptionService`)

**Interfaces:**
- Consumes: `notification.SubscriptionService` from Task 4.
- Produces: `GET/POST /account/notifications*` routes; `webmiddleware.DeviceCookie`/`ExpiredDeviceCookie`/`DeviceCookieName`.

- [ ] **Step 1: Read the existing patterns first**

Read `internal/web/handler/account.go`, `internal/web/pages/account.templ`, `internal/web/router.go` (the `/account` route block around line 138), and `internal/web/middleware/authentication.go` in full before writing anything — this task's HTTP/page/cookie code must match their exact conventions (render helper, `webmiddleware.CurrentUser`, `components.NavigationState`, CSRF token rendering via `webmiddleware.CSRFToken(ctx)`, redirect-with-notice query param style).

- [ ] **Step 2: Add the device cookie helpers**

Add to `internal/web/middleware/authentication.go`, alongside `SessionCookie`/`ExpiredSessionCookie`:

```go
const DeviceCookieName = "lansurfer_device"

const deviceCookieLifetime = 400 * 24 * time.Hour

// DeviceCookie identifies one browser across authentication sessions so
// server-side logout can remove exactly that browser's Push subscription.
// deviceID is a random opaque token generated once when Push is first
// enabled; only its hash (via internal/platform/crypto.Fingerprint) is ever
// persisted with a subscription.
func DeviceCookie(deviceID string, secure bool) *http.Cookie {
	return &http.Cookie{
		Name: DeviceCookieName, Value: deviceID, Path: "/", Expires: time.Now().Add(deviceCookieLifetime),
		MaxAge: int(deviceCookieLifetime.Seconds()), HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode,
	}
}

func ExpiredDeviceCookie(secure bool) *http.Cookie {
	return &http.Cookie{
		Name: DeviceCookieName, Path: "/", Expires: time.Unix(1, 0),
		MaxAge: -1, HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode,
	}
}

// CurrentOrNewDeviceID returns the existing device cookie's value, or
// generates and returns a fresh 32-byte random one if none is present yet.
// It never sets the cookie itself; the caller does that only once it has
// successfully persisted a subscription keyed on the returned ID.
func CurrentOrNewDeviceID(r *http.Request) (string, error) {
	if cookie, err := r.Cookie(DeviceCookieName); err == nil && cookie.Value != "" {
		return cookie.Value, nil
	}
	random := make([]byte, 32)
	if _, err := rand.Read(random); err != nil {
		return "", fmt.Errorf("generate device ID: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(random), nil
}
```

Add `"crypto/rand"`, `"encoding/base64"`, and `"fmt"` to the file's import block if not already present.

Write a focused test in the existing `internal/web/middleware` test file (find it, e.g. `authentication_test.go`; if none exists, create `internal/web/middleware/authentication_test.go` following the package's existing test style) covering: `CurrentOrNewDeviceID` returns the existing cookie's value when present, and returns a non-empty, freshly-generated value when absent. Run: `go test ./internal/web/middleware/... -v`.

- [ ] **Step 3: Decide the device-ID-hash scheme and document it**

The device cookie's raw value must never be persisted; only its hash is (`notification_push_subscription.device_id_hash`). Use `internal/platform/crypto.Fingerprint(encryptionKey, deviceID)` for this hash too — it is already exactly "keyed HMAC-SHA256, base64url" and reusing it avoids a second hashing primitive for the same job. The HTTP handler (Step 4) computes this hash once per request from the cookie value and the configured encryption key, and passes only the hash to `SubscriptionService`.

- [ ] **Step 4: Implement the HTTP handler**

Create `internal/web/handler/notification_settings.go` (model its structure directly on `internal/web/handler/account.go`):

```go
package handler

import (
	"context"
	"net/http"
	"strconv"

	"github.com/convention-x-treme/lansurfer/internal/notification"
	platformcrypto "github.com/convention-x-treme/lansurfer/internal/platform/crypto"
	"github.com/convention-x-treme/lansurfer/internal/web/branding"
	"github.com/convention-x-treme/lansurfer/internal/web/components"
	webmiddleware "github.com/convention-x-treme/lansurfer/internal/web/middleware"
	"github.com/convention-x-treme/lansurfer/internal/web/pages"
	"github.com/go-chi/chi/v5"
)

// SubscriptionManager is the narrow interface this handler needs from
// *notification.SubscriptionService.
type SubscriptionManager interface {
	Enable(ctx context.Context, userID, deviceIDHash, label, endpoint, p256dh, auth string) error
	Disable(ctx context.Context, userID string) error
	SetAudience(ctx context.Context, userID, audience string) error
	RemoveDevice(ctx context.Context, userID string, subscriptionID int64) error
	ListDevices(ctx context.Context, userID string) ([]notification.PushSubscriptionRecord, error)
	Preference(ctx context.Context, userID string) (notification.PushPreference, error)
}

type NotificationSettingsHandler struct {
	subscriptions   SubscriptionManager
	encryptionKey   []byte
	vapidPublicKey  string
	webPushEnabled  bool // whether Web Push is configured at all (VAPID present)
	brand           branding.Brand
	secureCookies   bool
}

func NewNotificationSettings(subscriptions SubscriptionManager, encryptionKey []byte, vapidPublicKey string, webPushConfigured bool, brand branding.Brand, secureCookies bool) *NotificationSettingsHandler {
	return &NotificationSettingsHandler{
		subscriptions: subscriptions, encryptionKey: encryptionKey, vapidPublicKey: vapidPublicKey,
		webPushEnabled: webPushConfigured, brand: brand, secureCookies: secureCookies,
	}
}

func (h *NotificationSettingsHandler) Show(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, http.StatusOK, "")
}

// Subscribe is the complete enable action: persist the browser-created
// PushSubscription and set web_push_enabled=true.
func (h *NotificationSettingsHandler) Subscribe(w http.ResponseWriter, r *http.Request) {
	account, ok := webmiddleware.CurrentUser(r.Context())
	if !ok {
		http.Error(w, "authentication required", http.StatusUnauthorized)
		return
	}
	if err := r.ParseForm(); err != nil {
		h.render(w, r, http.StatusBadRequest, "The subscription request could not be read.")
		return
	}
	endpoint, p256dh, auth := r.FormValue("endpoint"), r.FormValue("p256dh"), r.FormValue("auth")
	if endpoint == "" || p256dh == "" || auth == "" {
		h.render(w, r, http.StatusUnprocessableEntity, "The browser subscription was incomplete.")
		return
	}
	label := r.FormValue("label")
	if label == "" {
		label = "This device"
	}
	deviceID, err := webmiddleware.CurrentOrNewDeviceID(r)
	if err != nil {
		webmiddleware.LogError(r, "generate device ID", err)
		http.Error(w, "enable Web Push", http.StatusInternalServerError)
		return
	}
	deviceHash := platformcrypto.Fingerprint(h.encryptionKey, deviceID)
	if err := h.subscriptions.Enable(r.Context(), account.ID, deviceHash, label, endpoint, p256dh, auth); err != nil {
		webmiddleware.LogError(r, "enable Web Push", err)
		http.Error(w, "enable Web Push", http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, webmiddleware.DeviceCookie(deviceID, h.secureCookies))
	h.redirect(w, r, "/account/notifications?notice=push-enabled")
}

func (h *NotificationSettingsHandler) Disable(w http.ResponseWriter, r *http.Request) {
	account, ok := webmiddleware.CurrentUser(r.Context())
	if !ok {
		http.Error(w, "authentication required", http.StatusUnauthorized)
		return
	}
	if err := h.subscriptions.Disable(r.Context(), account.ID); err != nil {
		webmiddleware.LogError(r, "disable Web Push", err)
		http.Error(w, "disable Web Push", http.StatusInternalServerError)
		return
	}
	h.redirect(w, r, "/account/notifications?notice=push-disabled")
}

func (h *NotificationSettingsHandler) SetAudience(w http.ResponseWriter, r *http.Request) {
	account, ok := webmiddleware.CurrentUser(r.Context())
	if !ok {
		http.Error(w, "authentication required", http.StatusUnauthorized)
		return
	}
	if err := r.ParseForm(); err != nil {
		h.render(w, r, http.StatusBadRequest, "The form could not be read.")
		return
	}
	if err := h.subscriptions.SetAudience(r.Context(), account.ID, r.FormValue("audience")); err != nil {
		h.render(w, r, http.StatusUnprocessableEntity, "Choose a valid audience.")
		return
	}
	h.redirect(w, r, "/account/notifications?notice=audience-updated")
}

func (h *NotificationSettingsHandler) RemoveDevice(w http.ResponseWriter, r *http.Request) {
	account, ok := webmiddleware.CurrentUser(r.Context())
	if !ok {
		http.Error(w, "authentication required", http.StatusUnauthorized)
		return
	}
	subscriptionID, err := strconv.ParseInt(chi.URLParam(r, "subscriptionID"), 10, 64)
	if err != nil {
		http.Error(w, "invalid device ID", http.StatusBadRequest)
		return
	}
	if err := h.subscriptions.RemoveDevice(r.Context(), account.ID, subscriptionID); err != nil {
		webmiddleware.LogError(r, "remove push device", err)
		http.Error(w, "remove device", http.StatusInternalServerError)
		return
	}
	h.redirect(w, r, "/account/notifications?notice=device-removed")
}

func (h *NotificationSettingsHandler) render(w http.ResponseWriter, r *http.Request, status int, formError string) {
	account, ok := webmiddleware.CurrentUser(r.Context())
	if !ok {
		http.Error(w, "authentication required", http.StatusUnauthorized)
		return
	}
	devices, err := h.subscriptions.ListDevices(r.Context(), account.ID)
	if err != nil {
		webmiddleware.LogError(r, "list push devices", err)
		http.Error(w, "load notification settings", http.StatusInternalServerError)
		return
	}
	preference, err := h.subscriptions.Preference(r.Context(), account.ID)
	if err != nil {
		webmiddleware.LogError(r, "load push preference", err)
		http.Error(w, "load notification settings", http.StatusInternalServerError)
		return
	}
	navigation := components.NavigationState{Viewer: &components.Viewer{ID: account.ID, GameNick: account.GameNick, Role: string(account.Role)}, Section: "account"}
	render(w, r, status, pages.NotificationSettings(h.brand, navigation, pages.NotificationSettingsView{
		WebPushAvailable: h.webPushEnabled,
		VAPIDPublicKey:   h.vapidPublicKey,
		Preference:       preference,
		Devices:          devices,
		Notice:           notificationSettingsNotice(r),
		FormError:        formError,
	}))
}

func (h *NotificationSettingsHandler) redirect(w http.ResponseWriter, r *http.Request, target string) {
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", target)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

func notificationSettingsNotice(r *http.Request) string {
	switch r.URL.Query().Get("notice") {
	case "push-enabled":
		return "Push notifications enabled on this device."
	case "push-disabled":
		return "Push notifications disabled. Your devices remain registered."
	case "audience-updated":
		return "Notification audience updated."
	case "device-removed":
		return "Device removed."
	default:
		return ""
	}
}
```

`pages.NotificationSettingsView` deliberately never carries `EndpointCiphertext`/decrypted subscription material — only `ID`/`Label`/`DeviceIDHash`/timestamps, matching `notification.PushSubscriptionRecord`'s already-ciphertext-only shape from `ListDevices`.

- [ ] **Step 5: Implement the settings page templ**

Create `internal/web/pages/notification_settings.templ`. Read `internal/web/pages/account.templ` first and match its layout/DaisyUI component conventions exactly (page shell, navigation, form styling, notice banner). Define:

```go
type NotificationSettingsView struct {
	WebPushAvailable bool
	VAPIDPublicKey   string
	Preference       notification.PushPreference
	Devices          []notification.PushSubscriptionRecord
	Notice           string
	FormError        string
}
```

The template renders: a banner if `!WebPushAvailable` ("Web Push is not configured on this instance"); otherwise a form with a `data-vapid-public-key={ VAPIDPublicKey }` attribute (for the not-yet-built enable JavaScript to read), the current `Preference.WebPushEnabled`/`NewRequestAudience` state, an audience radio/select bound to `SetAudience`'s POST, a "Disable" button posting to `Disable` when enabled, and a list of `Devices` each with a "Remove" button posting to `RemoveDevice`. Every mutating form includes the CSRF token via `webmiddleware.CSRFToken(ctx)`, exactly like `account.templ`'s existing forms.

Run: `templ generate` (or `task generate`, which includes it) to produce `notification_settings_templ.go`.

- [ ] **Step 6: Wire the route and link from the account page**

In `internal/web/router.go`, inside the existing `authenticated.Route("/account", ...)` block (or as a sibling route guarded the same way — read the current block first and match its `RequireAuthentication`/`RequireGameNick` middleware usage), add:

```go
account.Route("/notifications", func(notifications chi.Router) {
	notifications.Get("/", notificationSettingsHandler.Show)
	notifications.Post("/subscribe", notificationSettingsHandler.Subscribe)
	notifications.Post("/disable", notificationSettingsHandler.Disable)
	notifications.Post("/audience", notificationSettingsHandler.SetAudience)
	notifications.Post("/devices/{subscriptionID}/remove", notificationSettingsHandler.RemoveDevice)
})
```

only when the dependency this handler needs is non-nil (matching the existing `if dependencies.Games != nil { ... }`-style conditional route registration elsewhere in this router). Add a link to `/account/notifications` in `internal/web/pages/account.templ`'s existing navigation/section list, next to identity/Game Nick management.

- [ ] **Step 7: Wire the composition root**

In `internal/app/notification.go` (the file Track E1 created), add a small constructor mirroring `newNotificationWorker`:

```go
func newSubscriptionService(store *notificationpostgres.Store, encryptionKey []byte) *notification.SubscriptionService {
	return notification.NewSubscriptionService(store, store, encryptionKey)
}
```

In `internal/app/app.go`, after the existing `notificationStore := notificationpostgres.NewStore(pool)` line, construct `subscriptionService := newSubscriptionService(notificationStore, cfg.DataEncryptionKey)` and pass it (plus `cfg.DataEncryptionKey`, `vapidPublicKey := ""; if cfg.WebPush != nil { vapidPublicKey = cfg.WebPush.PublicKey }`, and `cfg.WebPush != nil`) into `web.NewRouter`'s `RouterDependencies` as new fields (e.g. `Subscriptions: subscriptionService, EncryptionKey: cfg.DataEncryptionKey, VAPIDPublicKey: vapidPublicKey, WebPushConfigured: cfg.WebPush != nil`), following the exact pattern the existing `GameRequests`/`RequestQueries` dependencies already use in that struct literal. Construct `notificationSettingsHandler` in `internal/web/router.go`'s `NewRouter` alongside the other handlers, only when `dependencies.Subscriptions != nil`.

- [ ] **Step 8: Add the logout hook**

Read `internal/web/handler/authentication.go`'s existing `Logout` method. After it clears the session cookie, add: if a device cookie is present on the request, call `h.subscriptions.RemoveDeviceByHash(r.Context(), account.ID, platformcrypto.Fingerprint(h.encryptionKey, deviceCookie.Value))` (best-effort: log and continue on error, do not fail logout over this), then set `webmiddleware.ExpiredDeviceCookie(h.secureCookies)`. This requires threading a `SubscriptionManager`-shaped dependency (just the `RemoveDeviceByHash` method, or reuse the same narrow interface from Step 4) and the encryption key into `AuthenticationHandler`'s constructor — add both as new constructor parameters, defaulting to a no-op when nil (Web Push not configured), matching how other optional dependencies are already handled in this handler.

- [ ] **Step 9: Write the HTTP handler test**

Create `internal/web/handler/notification_settings_test.go` following the existing style in `internal/web/handler/account_test.go` (a fake `SubscriptionManager`, `webmiddleware.ContextWithUser` to simulate a signed-in request). Cover at minimum: `Show` renders 200 for a signed-in user; `Subscribe` with a complete form calls `Enable` and redirects; `Subscribe` with a missing field returns 422 and does not call `Enable`; `RemoveDevice` calls `RemoveDevice` with the parsed ID; `Disable`/`SetAudience` call through correctly; an unauthenticated request to any handler returns 401.

- [ ] **Step 10: Run the tests to verify they pass**

Run: `go test ./internal/web/... ./internal/app/... ./cmd/lansurfer/... -race` and `go build ./...` and `task generate` (to catch any templ/sqlc drift).
Expected: PASS / succeeds.

- [ ] **Step 11: Commit**

```bash
git add internal/web/pages/notification_settings.templ internal/web/pages/notification_settings_templ.go \
        internal/web/handler/notification_settings.go internal/web/handler/notification_settings_test.go \
        internal/web/middleware/authentication.go internal/web/middleware/authentication_test.go \
        internal/web/router.go internal/web/pages/account.templ internal/web/pages/account_templ.go \
        internal/web/handler/authentication.go internal/app/app.go internal/app/notification.go
git commit -m "feat(notification): add the Web Push settings page, device cookie, and logout hook"
```

---

### Task 6: Documentation reconciliation and full verification

**Files:**
- Modify: `docs/ARCHITECTURE.md`
- Modify: `docs/ROADMAP.md`
- Modify: `docs/superpowers/README.md`

- [ ] **Step 1: Update ARCHITECTURE.md**

In the `### \`notification\` (Track E)` section, add one paragraph after the existing E1 description noting: `notification_push_subscription`/`notification_push_preference` persistence, AES-256-GCM-at-rest storage via `internal/platform/crypto`, the `/account/notifications` settings surface, and that this is Web Push *foundation* only — no push has been sent by this code, recipient materialization and the transport adapter are a separate follow-up. State plainly that E2 delivery itself remains not yet implemented.

- [ ] **Step 2: Update ROADMAP.md**

Update the Track E status line/bullet to describe this foundation slice as implemented while E2's delivery (recipient materialization, transport, actual send) and the browser-side service-worker/enable JavaScript remain outstanding.

- [ ] **Step 3: Update docs/superpowers/README.md**

Note in the Notifications section that E2's foundation (subscriptions/preferences/settings page) is implemented; E2's delivery and client pieces are tracked by their own follow-up plans, not yet written/executed.

- [ ] **Step 4: Full verification**

Run, in order: `task generate`, `task test`, `task test:integration`, `task lint`, `task build`.
Expected: all succeed. Fix any pre-existing hardcoded assertion (for example a migration-count check, as happened twice during Track E1) directly rather than treating it as a new finding.

- [ ] **Step 5: Commit**

```bash
git add docs/ARCHITECTURE.md docs/ROADMAP.md docs/superpowers/README.md
git commit -m "docs(notification): document the Web Push subscription/preference foundation"
```

- [ ] **Step 6: Final summary**

Report: what was implemented, that no push has actually been sent by any of this code (Delivery plan still to come), that no service-worker/browser JavaScript exists yet to call the new endpoints from a real browser (Client plan still to come), and any deviation from this plan discovered during execution (in particular the exact `go-webpush`-independent VAPID validation approach and any sqlc/templ field-name corrections).
