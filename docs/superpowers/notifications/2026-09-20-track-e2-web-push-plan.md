# Track E2 Web Push Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Deliver Track E2 personal Web Push through E1's durable work/delivery pipeline, Track D's single service worker, and the existing authenticated Account UI.

**Architecture:** Keep `game_request_activity`, `notification_work`, and `notification_delivery` unchanged as the durable handoff and delivery ledger. Add notification-owned preference/subscription persistence, a narrow read-only recipient/payload projection, encrypted subscription storage, one `webpush` transport, Account handlers, and push-specific browser behavior in the existing application bundle and service worker. Materialize exact subscription IDs once; current eligibility can suppress those IDs but never add or redirect old delivery rows.

**Tech Stack:** Go 1.27, PostgreSQL/pgx/sqlc/goose, `github.com/marknefedov/go-webpush/v2` v2.0.0, standard-library AES-GCM/HMAC-SHA256/HKDF, chi, templ/HTMX, TypeScript/esbuild, Playwright.

**Spec:** `docs/superpowers/notifications/2026-09-20-track-e-web-push-design.md`

## Global Constraints

- Implement E2 only. Do not implement Discord/E3 or a generic provider framework.
- Keep `game_request_activity` as Track A's only transactional handoff. Never write foreign module tables from `notification`.
- Reuse E1 `notification_work`/`notification_delivery`; each delivery target is one immutable active subscription ID.
- `web_push_enabled` defaults to `false`; `new_request_audience` defaults to `followed` and permits only `followed` or `all`.
- Materialize recipients/devices once. Later checks may only suppress existing deliveries.
- Encrypt endpoint, `p256dh`, and `auth` separately with AES-256-GCM. Store no raw endpoint beside ciphertext.
- Derive separate HMAC-SHA256 keys from `LANSURFER_DATA_ENCRYPTION_KEY` for endpoint fingerprints and device-cookie hashes.
- One endpoint belongs to at most one user. Cross-user explicit enablement replaces the subscription row with a new ID so old deliveries cannot redirect.
- Reuse Track D's `/service-worker.js` and `registerServiceWorker()`. Never add another worker or registration path.
- Use one explicit browser gesture for permission and enablement. Never prompt on page load.
- Web Push payload TTL is exactly 300 seconds.
- Payload version is `1`; fields are `version`, `kind`, `game`, `plannedStart`, `url`, `instance`, and optional `actorGameNick` for `joined`/`left` only.
- Never include description, roster, provider identity, activity history, or a shared replacement tag.
- Provider `Retry-After` is honored in full. Only LANsurfer-computed backoff is capped at 5 minutes.
- All mutations use existing authentication, Game Nick, and CSRF middleware. Do not add a generic `/api` surface.
- Generated templ/sqlc/assets are regenerated, never hand-edited.
- Persisted docs, comments, commit messages, and user-visible copy use normal English despite Caveman conversation mode.

## Review Focus

1. Endpoint transfer races must never send an old delivery to a new owner; Task 3 tests new subscription IDs and global fingerprint uniqueness under concurrent reassignment.
2. Eligibility lookup failure must never fail open or consume work as zero-target; Task 1 tests retryable materialization and send suppression gates.
3. Cookie loss/re-registration must not revive old deliveries; Tasks 3 and 4 test hard deletion plus new IDs and target-key stability.
4. Payload parsing must reject malformed, oversized, cross-origin, or actor-leaking values before OS display/navigation; Task 6 tests each class.
5. Logout with expired/missing session must still remove only the device named by the independent valid device cookie; Task 5 tests cleanup independent of session revocation.

---

### Task 1: Align E1 retry and fail-closed resolution behavior

**Files:**
- Modify: `internal/notification/backoff.go`
- Modify: `internal/notification/backoff_test.go`
- Modify: `internal/notification/worker.go`
- Modify: `internal/notification/worker_test.go`
- Modify: `internal/platform/postgres/notification_integration_test.go`

**Interfaces:**
- Consumes: existing `Transport.Targets`, `WorkerRepository.MaterializeWork`, and `SendResult`.
- Produces: provider `RetryAfter` honored without the local cap; target-resolution failures leave activity undiscovered or delivery pending/retryable; `ErrNoLongerEligible` returned by a transport during its final send-side lookup records `suppressed`.

- [ ] **Step 1: Change tests first for Retry-After and target lookup failures**

  Replace the obsolete long-hint assertion with `20*time.Minute`, add a worker test where `Targets` fails during materialization and `MaterializeWork` is not called, add a claimed-delivery test where recheck failure records retry without calling `Send`, and add a send-side `ErrNoLongerEligible` test that records suppression. The production change each test defeats is respectively: unconditional delay cap, permanent zero-target work, fail-open send, and TOCTOU misclassification.

  ```go
  longHint := 20 * time.Minute
  if got := computeBackoff(1, &longHint, noJitter); got != longHint {
      t.Fatalf("computeBackoff() = %v, want %v", got, longHint)
  }
  // Fake repository counters must remain: materialize=0 after initial Targets error,
  // send=0 and retry=1 after recheck error, suppress=1 after send-side eligibility loss.
  ```

- [ ] **Step 2: Run red tests**

  Run:

  ```bash
  go test ./internal/notification -run 'TestComputeBackoff|TestWorker' -count=1
  task test:integration
  ```

  Expected: long Retry-After still reports 5 minutes; new worker tests show unwanted materialization/send/status behavior.

- [ ] **Step 3: Apply the minimal E1 correction**

  Implement these exact rules:

  ```go
  if retryAfter != nil {
      if *retryAfter < 0 { return 0 }
      return *retryAfter
  }
  delay := jitter(backoffSchedule[index])
  return min(max(delay, 0), maxAutomaticDelay)
  ```

  In `materialize`, if any configured transport returns an error, log and skip `MaterializeWork` for that activity so the existing anti-join rediscovers it. In `processOne`, turn a recheck error into `OutcomeRetryable` without calling `Send`. In `recordOutcome`, use `errors.Is(result.Err, ErrNoLongerEligible)` to call `RecordSuppressed` before provider outcome classification.

- [ ] **Step 4: Run green tests**

  ```bash
  go test ./internal/notification -count=1
  task test:integration
  ```

  Expected: all notification unit/integration tests pass; explicit 20-minute provider delay persists unchanged.

- [ ] **Step 5: Commit**

  ```bash
  git add internal/notification internal/platform/postgres/notification_integration_test.go
  git commit -m "fix(notification): honor provider retry delays"
  ```

### Task 2: Add validated Web Push configuration, crypto, and dependency

**Files:**
- Modify: `go.mod`
- Modify: `go.sum`
- Modify: `internal/platform/config/config.go`
- Modify: `internal/platform/config/config_test.go`
- Create: `internal/notification/crypto.go`
- Create: `internal/notification/crypto_test.go`
- Create: `internal/notification/webpush_config.go`
- Create: `internal/notification/webpush_config_test.go`

**Interfaces:**
- Consumes: environment and `github.com/marknefedov/go-webpush/v2` v2.0.0 VAPID JSON decoding.
- Produces: `config.WebPush`, `notification.SecretBox`, `notification.WebPushKeys`, endpoint normalization/fingerprinting, and device-token hashing.

- [ ] **Step 1: Write configuration and cryptography tests**

  Add table tests for: both VAPID keys absent disables Web Push; either key alone errors; malformed/mismatched P-256 keys error; configured keys require valid `mailto:` or HTTPS subject; configured keys require a raw-base64url 32-byte data key; disabled Web Push does not require the data key. Add crypto tests proving randomized ciphertext, successful round-trip, tamper failure, domain-separated deterministic endpoint/device HMAC values, HTTPS endpoint validation, and no plaintext substring in ciphertext.

  ```go
  t.Setenv("LANSURFER_WEBPUSH_PUBLIC_KEY", validPublic)
  t.Setenv("LANSURFER_WEBPUSH_PRIVATE_KEY", validPrivate)
  t.Setenv("LANSURFER_WEBPUSH_SUBJECT", "mailto:ops@example.test")
  t.Setenv("LANSURFER_DATA_ENCRYPTION_KEY", validDataKey)
  cfg, err := Load()
  if err != nil || cfg.WebPush == nil { t.Fatalf("Load() = %#v, %v", cfg.WebPush, err) }

  first, _ := box.Seal([]byte("secret"))
  second, _ := box.Seal([]byte("secret"))
  if bytes.Equal(first, second) { t.Fatal("ciphertexts reused a nonce") }
  if got, err := box.Open(first); err != nil || string(got) != "secret" { t.Fatalf("Open() = %q, %v", got, err) }
  ```

- [ ] **Step 2: Run red tests**

  ```bash
  go test ./internal/platform/config ./internal/notification -run 'Test.*(WebPush|Secret|Fingerprint|Device)' -count=1
  ```

  Expected: missing types/functions and configuration cases fail.

- [ ] **Step 3: Add the approved dependency and minimal types**

  ```bash
  go get github.com/marknefedov/go-webpush/v2@v2.0.0
  ```

  Add:

  ```go
  type WebPush struct {
      PublicKey string
      PrivateKey string
      Subject string
      DataEncryptionKey [32]byte
  }

  type SecretBox struct { aead cipher.AEAD }
  func NewSecretBox(key [32]byte) (*SecretBox, error)
  func (b *SecretBox) Seal(plaintext []byte) ([]byte, error)
  func (b *SecretBox) Open(ciphertext []byte) ([]byte, error)
  func DeriveEndpointFingerprintKey(key [32]byte) []byte
  func DeriveDeviceHashKey(key [32]byte) []byte
  func FingerprintEndpoint(key []byte, endpoint string) ([32]byte, string, error)
  func HashDeviceToken(key []byte, token string) ([32]byte, error)
  func ParseWebPushKeys(publicKey, privateKey string) (*webpush.VAPIDKeys, error)
  ```

  Use AES-GCM with a fresh nonce prefixed to each ciphertext. Use HKDF-SHA256 labels `lansurfer/webpush/endpoint-fingerprint/v1` and `lansurfer/webpush/device-cookie/v1`; use resulting keys only as HMAC keys. Validate endpoint as absolute HTTPS, without user info or fragment, then fingerprint its canonical string.

- [ ] **Step 4: Implement environment loading**

  Read `LANSURFER_WEBPUSH_PUBLIC_KEY`, `LANSURFER_WEBPUSH_PRIVATE_KEY`, `LANSURFER_WEBPUSH_SUBJECT`, and `LANSURFER_DATA_ENCRYPTION_KEY`. Decode public/private with the approved library, compare derived public key, and store validated strings/key bytes in `Config.WebPush` only when both VAPID keys exist.

- [ ] **Step 5: Run green tests and tidy**

  ```bash
  go test ./internal/platform/config ./internal/notification -count=1
  go mod tidy
  ```

  Expected: configuration and crypto tests pass; `go.mod` contains only the approved direct Web Push dependency plus its required indirect dependencies.

- [ ] **Step 6: Commit**

  ```bash
  git add go.mod go.sum internal/platform/config internal/notification/crypto* internal/notification/webpush_config*
  git commit -m "feat(notification): validate Web Push secrets"
  ```

### Task 3: Persist preferences and encrypted device subscriptions

**Files:**
- Create: `internal/platform/postgres/migrations/00012_web_push.sql`
- Create: `internal/notification/webpush.go`
- Modify: `internal/notification/postgres/queries/notification.sql`
- Modify: `internal/notification/postgres/store.go`
- Generated: `internal/notification/postgres/sqlcgen/*`
- Modify: `internal/platform/postgres/notification_integration_test.go`

**Interfaces:**
- Consumes: `SecretBox`, endpoint fingerprint key, device hash key.
- Produces: `WebPushPreferences`, `PushSubscription`, `SubscriptionInput`, and store methods for settings, explicit enable/reassignment, label update, exact-device removal/reconciliation, targeting, and send-side load.

- [ ] **Step 1: Write PostgreSQL tests before schema/store code**

  Add integration tests for default disabled/followed preference, valid updates, invalid audience/label constraints, multi-device rows, same-user endpoint refresh retaining ID, cross-user explicit reassignment receiving a new ID, concurrent reassignment leaving one owner, global endpoint fingerprint uniqueness, unique device hash, hard device deletion preserving historical delivery rows, encrypted columns excluding raw endpoint/keys, and decrypt round-trip through the store.

  ```go
  settings, devices, err := service.Settings(ctx, userA, "")
  if err != nil || settings.Enabled || settings.NewRequestAudience != AudienceFollowed || len(devices) != 0 {
      t.Fatalf("default settings = %#v, devices=%d, err=%v", settings, len(devices), err)
  }
  firstID, err := service.Enable(ctx, userA, validSubscription("device-a", endpoint))
  refreshedID, err := service.Enable(ctx, userA, validSubscription("device-a", endpoint))
  if err != nil || refreshedID != firstID { t.Fatalf("same-owner refresh changed ID: %q -> %q (%v)", firstID, refreshedID, err) }
  transferredID, err := service.Enable(ctx, userB, validSubscription("device-b", endpoint))
  if err != nil || transferredID == firstID { t.Fatalf("cross-user transfer reused ID %q (%v)", transferredID, err) }
  ```

- [ ] **Step 2: Run red integration tests**

  ```bash
  task test:integration
  ```

  Expected: migration/table/store symbols are missing.

- [ ] **Step 3: Add notification-owned schema**

  Create:

  ```sql
  CREATE TABLE web_push_preferences (
      user_id uuid PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
      web_push_enabled boolean NOT NULL DEFAULT false,
      new_request_audience text NOT NULL DEFAULT 'followed'
          CHECK (new_request_audience IN ('followed', 'all')),
      updated_at timestamptz NOT NULL DEFAULT now()
  );

  CREATE TABLE web_push_subscriptions (
      id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
      user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
      label text NOT NULL CHECK (char_length(label) BETWEEN 1 AND 64),
      device_hash bytea NOT NULL UNIQUE CHECK (octet_length(device_hash) = 32),
      endpoint_fingerprint bytea NOT NULL UNIQUE CHECK (octet_length(endpoint_fingerprint) = 32),
      endpoint_ciphertext bytea NOT NULL,
      p256dh_ciphertext bytea NOT NULL,
      auth_ciphertext bytea NOT NULL,
      created_at timestamptz NOT NULL DEFAULT now(),
      updated_at timestamptz NOT NULL DEFAULT now()
  );
  CREATE INDEX web_push_subscriptions_user_idx ON web_push_subscriptions(user_id);
  ```

  DOWN drops only these two E2 tables. Do not change E1 delivery tables.

- [ ] **Step 4: Define domain/store API and implement transactions**

  Add domain types and a small service over the PostgreSQL repository:

  ```go
  type NewRequestAudience string
  const (AudienceFollowed NewRequestAudience = "followed"; AudienceAll NewRequestAudience = "all")
  type WebPushPreferences struct { Enabled bool; NewRequestAudience NewRequestAudience }
  type PushSubscription struct { ID, UserID, Label string; CurrentDevice bool; CreatedAt, UpdatedAt time.Time }
  type SubscriptionInput struct { Endpoint, P256DH, Auth, Label, DeviceToken string }

  type WebPushService struct { repo WebPushSettingsRepository; deviceHashKey []byte }
  func NewWebPushService(repo WebPushSettingsRepository, deviceHashKey []byte) *WebPushService
  func (s *WebPushService) Settings(ctx context.Context, userID, deviceToken string) (WebPushPreferences, []PushSubscription, error)
  func (s *WebPushService) SetAudience(ctx context.Context, userID string, audience NewRequestAudience) error
  func (s *WebPushService) Disable(ctx context.Context, userID string) error
  func (s *WebPushService) Enable(ctx context.Context, userID string, input SubscriptionInput) (string, error)
  func (s *WebPushService) Reassociate(ctx context.Context, userID string, input SubscriptionInput) (string, error)
  func (s *WebPushService) RenameDevice(ctx context.Context, userID, subscriptionID, label string) error
  func (s *WebPushService) RemoveDevice(ctx context.Context, userID, subscriptionID, deviceToken string) (bool, error)
  func (s *WebPushService) ReconcileCurrentDevice(ctx context.Context, userID, deviceToken string) error
  func (s *WebPushService) RemoveDeviceByToken(ctx context.Context, deviceToken string) error
  func (s *WebPushService) DeviceKnown(ctx context.Context, deviceToken string) (bool, error)
  ```

  `Enable` validates input first, locks by endpoint fingerprint with a PostgreSQL transaction advisory lock, updates in place only when existing owner matches, and otherwise deletes the old row then inserts a fresh UUID row. It upserts `web_push_enabled=true` without changing an existing audience. `Reassociate` accepts only an endpoint already owned by the same user, keeps its ID/label/enablement, and updates its device hash and encrypted key material; another user's endpoint returns conflict until explicit `Enable`. `Disable` is the only ordinary preference mutation for enablement; re-enable always goes through `Enable`. Never return ciphertext or raw endpoint to UI/diagnostics.

- [ ] **Step 5: Generate sqlc and run green integration tests**

  ```bash
  task generate
  task test:integration
  ```

  Expected: subscription ownership, encryption, reassignment, and deletion tests pass.

- [ ] **Step 6: Commit**

  ```bash
  git add internal/platform/postgres/migrations/00012_web_push.sql internal/notification internal/platform/postgres/notification_integration_test.go
  git commit -m "feat(notification): persist encrypted Push devices"
  ```

### Task 4: Implement targeting, payloads, and provider adapter

**Files:**
- Modify: `internal/notification/webpush.go`
- Create: `internal/notification/webpush_transport.go`
- Create: `internal/notification/webpush_transport_test.go`
- Create: `internal/notification/postgres/queries/web_push.sql`
- Modify: `internal/notification/postgres/store.go`
- Generated: `internal/notification/postgres/sqlcgen/*`
- Modify: `internal/platform/postgres/notification_integration_test.go`

**Interfaces:**
- Consumes: current `games`, `game_subscriptions`, `game_requests`, `game_request_members`, `users`, notification preferences/subscriptions, and E1 `Transport`.
- Produces: `WebPushTransport` named `web_push`, exact subscription targets, minimal JSON payload, send-side eligibility/decryption, provider classification, and invalid-endpoint deletion.

- [ ] **Step 1: Write recipient/payload/provider tests first**

  Unit-test payload JSON fields and omission of description/roster/provider/history; actor nick present only for join/leave; URL is `BaseURL + /game-requests/{id}`; TTL passed as `300`. Table-test accepted 2xx, 404/410 expiry cleanup, 429 with `Retry-After`, 5xx, network failure, and other 4xx without deletion. Integration-test followed versus all, actor exclusion, joined/left/closed/cancelled current participants, lifecycle audience independence, deduplication, multi-device expansion, no-device behavior, no late follow/join/enable/device expansion, and suppression after disable/unfollow/leave/removal/reassignment.

  ```go
  var fields map[string]json.RawMessage
  if err := json.Unmarshal(payload, &fields); err != nil { t.Fatal(err) }
  want := []string{"version", "kind", "game", "plannedStart", "url", "instance"}
  if got := sortedKeys(fields); !slices.Equal(got, want) { t.Fatalf("payload fields = %v, want %v", got, want) }

  targets, err := transport.Targets(ctx, createdActivity)
  if err != nil { t.Fatal(err) }
  if slices.ContainsFunc(targets, targetFor(actorID)) { t.Fatal("actor received own notification") }
  // After materialization, enabling/following/adding another device leaves delivery row count unchanged.
  ```

- [ ] **Step 2: Run red tests**

  ```bash
  go test ./internal/notification -run 'TestWebPush' -count=1
  task test:integration
  ```

  Expected: transport/read-model methods are missing.

- [ ] **Step 3: Add narrow read models and exact target identity**

  Add:

  ```go
  type WebPushSnapshot struct {
      GameID string
      Game string
      PlannedStart time.Time
      ActorGameNick string
  }
  type WebPushTarget struct { SubscriptionID, UserID string }
  type WebPushRepository interface {
      EligibleWebPushTargets(context.Context, Activity) ([]WebPushTarget, error)
      LoadEligibleWebPushTarget(context.Context, Activity, WebPushTarget) (WebPushSnapshot, DecryptedSubscription, error)
      RemoveExpiredWebPushSubscription(context.Context, string) error
  }
  ```

  Encode target keys as `<subscription UUID>:<user UUID>` and parse strictly. Initial SQL selects enabled current recipients, excludes actor, expands active devices, and returns each device once. Created uses current follower or `all`; lifecycle uses current membership. Final load checks the exact subscription/user binding plus current preference/follow or membership in one read before decrypting. Missing eligibility returns `ErrNoLongerEligible`.

- [ ] **Step 4: Implement narrow library adapter**

  Construct `webpush.Client` once. Convert decrypted browser keys with `webpush.DecodeSubscriptionKeys`; send using `webpush.SendOptions{Subject: subject, VAPIDKeys: keys, TTL: 300}`. Close response bodies. Map `PushServiceError.SubscriptionExpired` to permanent plus hard delete; `Temporary`, 429, and 5xx to retryable; network errors to retryable; remaining 4xx to permanent without deletion. Convert absolute library `RetryAfter` to a duration relative to injected `now`, clamp past values to zero, and never include raw endpoint/body in `Err`.

- [ ] **Step 5: Implement transport methods**

  `Targets` maps the repository's positive snapshot to E1 targets. `Send` calls `LoadEligibleWebPushTarget` again immediately before external send; `ErrNoLongerEligible` flows to Task 1 suppression. Marshal exactly payload version 1 and the allowed fields. Use current public actor Game Nick for join/leave only.

  Emit no `uncertain` result: accepted HTTP is succeeded, provider HTTP is status-classified, and the approved network class is retryable. Keep E1 `uncertain` reserved for a future genuinely unsafe retry.

- [ ] **Step 6: Generate and run green tests**

  ```bash
  task generate
  go test ./internal/notification -count=1
  task test:integration
  ```

  Expected: every required targeting/classification/suppression case passes.

- [ ] **Step 7: Commit**

  ```bash
  git add internal/notification internal/platform/postgres/notification_integration_test.go
  git commit -m "feat(notification): deliver targeted Web Push"
  ```

### Task 5: Add Account settings, device cookie, and logout cleanup

**Files:**
- Create: `internal/web/handler/notifications.go`
- Create: `internal/web/handler/notifications_test.go`
- Modify: `internal/web/handler/authentication.go`
- Modify: `internal/web/handler/authentication_test.go`
- Modify: `internal/web/router.go`
- Modify: `internal/web/router_test.go`
- Create: `internal/web/pages/notifications.templ`
- Modify: `internal/web/pages/account.templ`
- Modify: `internal/web/pages/account_test.go`
- Modify: `internal/web/components/layout.templ`
- Generated: `internal/web/pages/notifications_templ.go`, account/layout generated files

**Interfaces:**
- Consumes: authenticated current user, CSRF token, `WebPushSettings` store/service, public VAPID key, secure-cookie flag.
- Produces: `/account/notifications` settings surface and POST actions; `lansurfer_push_device` 32-byte random opaque cookie with 400-day lifetime; exact-current-device cleanup on logout.

- [ ] **Step 1: Write handler/template/logout tests first**

  Test authentication/Game Nick gates; Account link; public key exposed only on authenticated notification page; default disabled/followed, unsupported placeholder, disabled, enabled, enabled-no-devices, device list/current marker; preference update validation; explicit subscription enable JSON validation; label trim/64-rune bound; ownership-safe rename/removal; reconcile removal without changing global enablement; 400-day HttpOnly/SameSite=Lax/Secure cookie; logout removes the cookie-named device only and still revokes the session; missing/invalid device cookie changes no other device.

  ```go
  request := httptest.NewRequest(http.MethodPost, "/auth/logout", strings.NewReader("csrf_token="+csrf))
  request.AddCookie(sessionCookie)
  request.AddCookie(pushDeviceCookie("device-a"))
  response := httptest.NewRecorder()
  router.ServeHTTP(response, request)
  if settings.removedToken != "device-a" { t.Fatalf("removed token = %q", settings.removedToken) }
  if sessions.loggedOutToken != sessionCookie.Value { t.Fatal("session was not revoked") }
  if !hasExpiredCookie(response.Result().Cookies(), "lansurfer_push_device") { t.Fatal("device cookie not expired") }
  ```

- [ ] **Step 2: Run red web tests**

  ```bash
  go test ./internal/web/... -run 'Test.*(Notification|Logout|Account)' -count=1
  ```

  Expected: new page/routes/interfaces are missing.

- [ ] **Step 3: Add the narrow handler service contract**

  Define handler-owned methods:

  ```go
  type NotificationSettings interface {
      Settings(context.Context, string, string) (notification.WebPushPreferences, []notification.PushSubscription, error)
      SetAudience(context.Context, string, notification.NewRequestAudience) error
      Disable(context.Context, string) error
      Enable(context.Context, string, notification.SubscriptionInput) (string, error)
      Reassociate(context.Context, string, notification.SubscriptionInput) (string, error)
      RenameDevice(context.Context, string, string, string) error
      RemoveDevice(context.Context, string, string, string) (bool, error)
      ReconcileCurrentDevice(context.Context, string, string) error
      RemoveDeviceByToken(context.Context, string) error
      DeviceKnown(context.Context, string) (bool, error)
  }
  ```

  Add GET `/account/notifications`; POST `/audience`; POST `/disable`; POST `/subscriptions`; POST `/current-device/reassociate`; POST `/devices/{subscriptionID}/label`; POST `/devices/{subscriptionID}/remove`; POST `/current-device/reconcile`. Keep audience/disable/labels/removal as forms. Accept subscription/reassociation JSON only with existing `X-CSRF-Token` support and strict unknown-field rejection. No ordinary form can set `web_push_enabled=true`; only successful subscription enablement does that.

- [ ] **Step 4: Implement device cookie and logout coordination**

  Generate a device token for explicit enable/reassociation, persist its hash atomically, then set `lansurfer_push_device` with Path `/`, MaxAge `34560000`, HttpOnly, SameSite=Lax, and deployment Secure flag. Authenticated middleware refreshes it only when `DeviceKnown` confirms the hash remains active. Expire it on current-device removal/reconciliation and logout. Add `RemoveDeviceByToken` to `AuthenticationHandler`; call it independently of whether the auth session cookie exists, then revoke the session. A cleanup database error makes logout fail visibly rather than claiming device removal occurred.

- [ ] **Step 5: Build server-rendered page states**

  Reuse `Layout`, `NavigationState{Section:"account"}`, `auth-panel`, alerts, status chips, and existing button hierarchy. Render data attributes for VAPID public key, CSRF token, server enablement, and current-device state. Include explicit text for unavailable/unsupported placeholder, permission undecided, browser blocked, disabled, enabled, and enabled with no devices; TypeScript selects browser-specific state after feature detection.

- [ ] **Step 6: Generate and run green web tests**

  ```bash
  task generate
  go test ./internal/web/... -count=1
  ```

  Expected: routes, CSRF, cookie, page-state, and logout tests pass.

- [ ] **Step 7: Commit**

  ```bash
  git add internal/web
  git commit -m "feat(web): add Push notification settings"
  ```

### Task 6: Add browser enablement and service-worker push behavior

**Files:**
- Create: `web/src/web-push.ts`
- Modify: `web/src/app.ts`
- Modify: `web/src/service-worker.ts`
- Modify: `web/tests-pwa/service-worker.spec.ts`
- Modify: `internal/web/browsertest/main.go`
- Generated: `internal/web/assets/dist/app.js`
- Generated: `internal/web/assets/dist/service-worker.js`

**Interfaces:**
- Consumes: Track D `registerServiceWorker()`, server-rendered VAPID/CSRF/state data, PushManager/Notification/ServiceWorker APIs.
- Produces: gesture-only enable flow, permission/revocation/device management enhancement, validated Push v1 rendering, visible-window suppression, and click navigation.

- [ ] **Step 1: Add failing browser/PWA tests**

  Extend fixture settings methods and add Playwright cases for unsupported/insecure state, permission `default`, denied/browser-blocked without repeated prompt, explicit click ordering (`register` then `requestPermission` then `subscribe` then POST), enabled and enabled-no-devices state, logout/current-device best-effort unsubscribe, visible-client suppression, hidden-client one-notification behavior, malformed/cross-origin/oversized payload rejection, one notification per activity without shared tag, existing-window focus+navigate, and no-window `openWindow`.

  ```ts
  await page.goto("/account/notifications");
  expect(await page.evaluate(() => window.__pushTest.permissionCalls)).toBe(0);
  await page.getByRole("button", { name: "Enable notifications" }).click();
  await expect.poll(() => page.evaluate(() => window.__pushTest.permissionCalls)).toBe(1);
  await expect.poll(() => page.evaluate(() => window.__pushTest.subscribeCalls)).toBe(1);

  await deliverPush(validPayload);
  await expect.poll(() => shownNotifications()).toHaveLength(0); // visible window
  await page.evaluate(() => window.__pushTest.setVisibility("hidden"));
  await deliverPush(validPayload);
  await expect.poll(() => shownNotifications()).toHaveLength(1);
  ```

- [ ] **Step 2: Run red browser suites**

  ```bash
  task test:browser
  task test:pwa
  ```

  Expected: new settings and worker interaction tests fail.

- [ ] **Step 3: Implement page-side Web Push enhancement**

  Import one DOM-gated initializer from `web-push.ts` into existing `app.ts`; import and call Track D's memoized `registerServiceWorker()`. Feature-detect `isSecureContext`, `serviceWorker`, `PushManager`, and `Notification`. Never call `requestPermission()` except inside enable-button click. Convert public VAPID base64url to `Uint8Array`; reuse `registration.pushManager.getSubscription()` before `subscribe({userVisibleOnly:true, applicationServerKey})`; POST `subscription.toJSON()` plus label using the rendered CSRF header. On authenticated page load, permission denied or missing browser subscription for a server-known current device calls reconcile without changing global enablement. If the device cookie was cleared but a browser subscription exists, reassociate it only when already owned by the same user; cross-user transfer still requires explicit enable. Intercept only current-device remove/logout controls for best-effort `unsubscribe()`; always let server mutation/logout proceed.

- [ ] **Step 4: Implement strict service-worker payload handling**

  Validate object shape, `version === 1`, allowed kind, required nonempty bounded strings (`game` 200, `plannedStart` 64, `instance` 64, actor nick 32), actor nick required only for joined/left and forbidden otherwise, and absolute URL whose origin equals `self.location.origin` and path matches `/game-requests/{id}`. Invalid data returns without display/navigation. Before display, call `clients.matchAll({type:"window", includeUncontrolled:true})`; suppress when any `WindowClient.visibilityState === "visible"`. Otherwise call `showNotification` with deterministic title, body containing planned start plus instance, and `data:{url}` without `tag`.

- [ ] **Step 5: Implement click behavior**

  Close notification, revalidate `notification.data.url`, find same-origin window clients, call `navigate(url)` then `focus()` on the first usable client, else `clients.openWindow(url)`. Wrap async work in `event.waitUntil`.

- [ ] **Step 6: Generate and run green browser suites**

  ```bash
  task generate
  task test:browser
  task test:pwa
  ```

  Expected: existing Track D lifecycle tests and all E2 capability/permission/push/click tests pass.

- [ ] **Step 7: Commit**

  ```bash
  git add web/src web/tests-pwa internal/web/browsertest internal/web/assets/dist
  git commit -m "feat(pwa): handle personal Web Push"
  ```

### Task 7: Wire production, CLI retry, docs, and complete verification

**Files:**
- Modify: `internal/app/app.go`
- Modify: `internal/app/notification.go`
- Create: `internal/app/notification_test.go`
- Modify: `README.md`
- Modify: `docs/PRODUCT.md`
- Modify: `docs/ARCHITECTURE.md`
- Modify: `docs/ROADMAP.md`
- Modify: `docs/superpowers/README.md`
- Modify: `docs/superpowers/notifications/2026-09-20-track-e2-web-push-plan.md`

**Interfaces:**
- Consumes: validated `config.WebPush`, notification PostgreSQL store, Web Push transport/settings service, router dependencies.
- Produces: one configured production transport shared by worker and CLI manual retry, settings UI wiring, current canonical E2 status, and final evidence.

- [ ] **Step 1: Write construction tests first**

  Test disabled configuration registers no transport but still materializes zero-delivery work; configured Web Push constructs exactly one `web_push` transport and exposes only its public key to the router; invalid/lost encryption material fails startup; CLI manual retry constructs the same transport and eligibility checks rather than returning `ErrTransportNotConfigured`.

  ```go
  disabled, err := newNotificationDependencies(config.Config{}, store, "LANsurfer", "http://localhost:8080")
  if err != nil || len(disabled.Transports) != 0 || disabled.PublicVAPIDKey != "" {
      t.Fatalf("disabled dependencies = %#v, %v", disabled, err)
  }
  enabled, err := newNotificationDependencies(configuredWebPush(), store, "CXT", "https://lan.example")
  if err != nil || len(enabled.Transports) != 1 || enabled.Transports[0].Name() != "web_push" {
      t.Fatalf("enabled dependencies = %#v, %v", enabled, err)
  }
  if enabled.PublicVAPIDKey != configuredWebPush().WebPush.PublicKey { t.Fatal("public key mismatch") }
  ```

- [ ] **Step 2: Run red construction tests**

  ```bash
  go test ./internal/app -count=1
  ```

  Expected: composition still returns `nil` transports and router lacks notification settings.

- [ ] **Step 3: Wire one shared constructor**

  Replace the zero-argument helpers with a constructor returning transport/settings/public-key dependencies from `config.WebPush` and `notificationpostgres.Store`. Use it in `Run`, `ListNotificationProblems`, and both manual retry commands. Do not create a service locator. Start no new process or runtime service.

  ```go
  type notificationDependencies struct {
      Transports []notification.Transport
      Settings *notification.WebPushService
      PublicVAPIDKey string
  }
  func newNotificationDependencies(cfg config.Config, store *notificationpostgres.Store, instanceName, baseURL string) (notificationDependencies, error)
  ```

- [ ] **Step 4: Update durable documentation only**

  Document the four environment variables, disabled/invalid startup rules, key-loss consequence, Account device management, and real-device validation caveat in README. Update ARCHITECTURE module/status text from “E2 not implemented” to the actual encrypted subscription/recipient/adapter design. Update ROADMAP status/evidence without marking Pilot real-device validation complete. Mark this plan historical after verified implementation and update `docs/superpowers/README.md`.

- [ ] **Step 5: Run generated-output and targeted checks**

  ```bash
  task generate
  go test ./internal/notification ./internal/platform/config ./internal/web/... ./internal/app -count=1
  task test:integration
  task test:browser
  task test:pwa
  ```

  Expected: all targeted/unit/PostgreSQL/browser/PWA behavior passes.

- [ ] **Step 6: Run every requested repository gate fresh**

  ```bash
  task generate
  task test
  task test:integration
  task test:browser
  task test:pwa
  task lint
  task build
  ```

  Expected: every command exits 0. Record exact command/output status in the final review report; do not infer success from earlier runs.

- [ ] **Step 7: Perform Sol-level final review**

  Dispatch GPT-5.6 Sol with the approved E2 design, this plan, base SHA, head SHA, and full diff. Require explicit review of security/crypto, endpoint ownership races, no late expansion, suppression, provider classification, service-worker validation/navigation, logout isolation, module boundaries, and tests. Fix every Critical/Important finding, rerun affected tests, then rerun the complete gate in Step 6.

- [ ] **Step 8: Record real-device validation boundary**

  State that automated Chromium proves browser and worker logic. Unless physically performed, leave Desktop/Android/iPhone real provider receipt, installed-PWA permission behavior, and notification-click behavior as Pilot validation still required. This is not an implementation deviation.

- [ ] **Step 9: Commit**

  ```bash
  git add internal/app README.md docs internal/web/assets/dist
  git commit -m "docs(notification): record Web Push delivery"
  ```
