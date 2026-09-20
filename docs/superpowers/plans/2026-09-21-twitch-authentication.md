# Twitch Authentication Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add optional Twitch OIDC login that shares Twitch application credentials with independently enabled IGDB synchronization.

**Architecture:** Add a narrow configuration seam to the existing OIDC adapter. A Twitch constructor supplies its fixed issuer, provider kind, scope, and username claim request. Existing OAuth flows, identity resolution, sessions, templates, and account UI remain shared.

**Tech Stack:** Go, PostgreSQL/goose, sqlc, `coreos/go-oidc`, `golang.org/x/oauth2`, templ, Taskfile.

**Spec:** `docs/superpowers/auth/2026-09-21-twitch-auth-design.md`

## Global Constraints

- Keep generic OIDC, Discord, and Twitch independent choices resolving to local users and explicit identities.
- Do not add Twitch-specific sessions, account persistence, HTTP flow logic, or dependencies.
- Request Twitch `openid` and `preferred_username` only. Never request email, `email_verified`, or `user:read:email`.
- Replace legacy IGDB credentials with shared `LANSURFER_TWITCH_CLIENT_ID/SECRET`.
- Parse strict `LANSURFER_TWITCH_AUTH_ENABLED` and `LANSURFER_IGDB_ENABLED` booleans; both default false.
- Expand existing provider-kind constraints only with `twitch`. Preserve keys, transactions, and other schema.
- Update PRODUCT, ARCHITECTURE, ROADMAP, README, and `.env.example`.

## Review Focus

- Enabled Twitch capability without both credentials must fail startup.
- Twitch authorization must omit email scopes and claims.
- A profile claiming another kind/key must fail existing common callback validation.
- PostgreSQL must accept Twitch and reject an unknown provider kind.
- Twitch-auth-only must not initialize IGDB; IGDB-only must not advertise Twitch login.

---

### Task 1: Shared Twitch configuration and IGDB gating

**Files:**

- Modify: `internal/platform/config/config.go`
- Modify: `internal/platform/config/config_test.go`
- Modify: `internal/app/igdb.go`
- Modify: `internal/app/igdb_test.go`

**Interfaces:**

- Produce `config.Twitch{ClientID, ClientSecret, AuthEnabled, IGDBEnabled}` and `Config.Twitch *Twitch`.
- Change `newIGDBSync(context.Context, *config.Twitch, game.ProviderIndexRepository, *slog.Logger) igdbSync`.

- [ ] **Step 1: Write failing configuration tests**

Name break: an enabled feature may lack credentials or enable its sibling.

```go
func TestLoadTwitchConfigurationEnablesCapabilitiesIndependently(t *testing.T) {
    clearOptionalEnvironment(t)
    t.Setenv("LANSURFER_DATABASE_URL", "postgres://example")
    t.Setenv("LANSURFER_TWITCH_CLIENT_ID", "client")
    t.Setenv("LANSURFER_TWITCH_CLIENT_SECRET", "secret")
    t.Setenv("LANSURFER_TWITCH_AUTH_ENABLED", "true")

    cfg, err := Load()
    if err != nil { t.Fatal(err) }
    if cfg.Twitch == nil || !cfg.Twitch.AuthEnabled || cfg.Twitch.IGDBEnabled {
        t.Fatalf("Twitch = %#v", cfg.Twitch)
    }
}
```

Add literal cases for both flags false, IGDB only, both enabled, partial credentials, and invalid boolean. Clear all new variables in `clearOptionalEnvironment`.

- [ ] **Step 2: Run RED check**

Run: `go test ./internal/platform/config -run 'Twitch|IGDB' -count=1`

Expected: FAIL because `Config.Twitch` and feature-flag parsing do not exist.

- [ ] **Step 3: Add minimal configuration and IGDB wiring**

Replace `IGDB` with:

```go
type Twitch struct {
    ClientID, ClientSecret string
    AuthEnabled, IGDBEnabled bool
}
```

Use `strconv.ParseBool` for non-empty flags. If either flag is true, require both trimmed credentials. Return nil when flags are false and credentials empty. Make `newIGDBSync` return `disabledIGDBSync{}` unless `cfg != nil && cfg.IGDBEnabled`; otherwise pass shared credentials to `igdb.NewClient`.

- [ ] **Step 4: Run GREEN check**

Run: `go test ./internal/platform/config ./internal/app -run 'Twitch|IGDB' -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/platform/config/config.go internal/platform/config/config_test.go internal/app/igdb.go internal/app/igdb_test.go
git commit -m "feat(config): share Twitch credentials with IGDB"
```

### Task 2: Twitch OIDC adapter and provider registry

**Files:**

- Modify: `internal/auth/auth.go`
- Modify: `internal/auth/oidc.go`
- Modify: `internal/auth/oidc_test.go`
- Modify: `internal/auth/oauth_service.go`
- Modify: `internal/auth/oauth_service_test.go`

**Interfaces:**

- Produce `auth.DiscoverTwitchProvider(context.Context, auth.TwitchConfig) (*auth.OIDCProvider, error)`.
- `TwitchConfig` has `ClientID`, `ClientSecret`, and `RedirectURL`.
- `OAuthService.Providers()` orders `oidc`, `discord`, then `twitch`.

- [ ] **Step 1: Write failing adapter tests**

Name break: Twitch login may request email or store a generic OIDC identity.

```go
func TestTwitchAuthorizationURLRequestsOnlyOpenIDAndUsername(t *testing.T) {
    provider := newOIDCProviderForTest("twitch", "twitch", []string{"openid"}, twitchUsernameClaims())
    parsed, err := url.Parse(provider.AuthorizationURL("state", "nonce", "verifier"))
    if err != nil { t.Fatal(err) }
    query := parsed.Query()
    if query.Get("scope") != "openid" || strings.Contains(query.Get("scope"), "email") ||
        !strings.Contains(query.Get("claims"), "preferred_username") {
        t.Fatalf("authorization query = %v", query)
    }
}
```

Add a profile fixture proving verified Twitch `sub` maps to subject, `preferred_username` maps to name/suggestion, and email remains empty. Add provider ordering coverage using stub adapters.

- [ ] **Step 2: Run RED check**

Run: `go test ./internal/auth -run 'Twitch|ListsTwitch' -count=1`

Expected: FAIL because Twitch construction, allowed profile kind, and provider order do not exist.

- [ ] **Step 3: Add minimal OIDC seam**

Add `twitch` to `validateProfile`. Extend private OIDC configuration with provider kind/key, scopes, and optional authorization parameters. Preserve generic OIDC defaults exactly: kind `oidc`, verified issuer key, scopes `openid profile email`, and current custom Game Nick behavior.

Implement `DiscoverTwitchProvider` through the shared discovery function with issuer `https://id.twitch.tv/oauth2`, kind/key `twitch`, scope `[]string{"openid"}`, and an ID-token claims parameter for `preferred_username`. Never populate email. Append `twitch` to deterministic provider availability ordering.

- [ ] **Step 4: Run GREEN check**

Run: `go test ./internal/auth -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/auth/auth.go internal/auth/oidc.go internal/auth/oidc_test.go internal/auth/oauth_service.go internal/auth/oauth_service_test.go
git commit -m "feat(auth): add Twitch OIDC provider"
```

### Task 3: Composition root, routes, and constraints

**Files:**

- Modify: `internal/app/app.go`
- Modify: `internal/web/router.go`
- Modify: `internal/web/router_test.go`
- Modify: `internal/web/handler/authentication_test.go`
- Modify: `internal/platform/postgres/postgres_integration_test.go`
- Create: `internal/platform/postgres/migrations/00012_twitch_authentication.sql`

**Interfaces:**

- Consume `config.Twitch` and `auth.DiscoverTwitchProvider`.
- Produce `/auth/twitch`, `/auth/twitch/callback`, and `/account/link/twitch`.
- Permit `twitch` for `user_identities.provider_kind` and `auth_flows.provider_kind`.

- [ ] **Step 1: Write failing route and migration tests**

Name break: configured Twitch may be unreachable or PostgreSQL may reject a valid Twitch flow.

Add router/handler cases that assert Twitch begin/callback delegates with `ProviderKind: "twitch"`. Extend `TestForwardMigrationIntroducesGameNickAndAuthFlows` with direct Twitch identity and OAuth-flow inserts, while retaining an insert of an invalid kind that PostgreSQL rejects.

- [ ] **Step 2: Run RED check**

Run: `go test ./internal/web ./internal/platform/postgres -run 'Twitch|ForwardMigrationIntroducesGameNickAndAuthFlows' -count=1`

Expected: FAIL because Twitch routes and constraints do not exist.

- [ ] **Step 3: Wire common handlers and migrate**

In `app.Run`, only discover/create Twitch provider when `cfg.Twitch != nil && cfg.Twitch.AuthEnabled`; use callback `cfg.BaseURL + "/auth/twitch/callback"` and display name `Twitch`. Do not construct it for IGDB-only configuration.

Mount:

```go
authenticated.Get("/auth/twitch", authenticationHandler.BeginFor("twitch"))
authenticated.Get("/auth/twitch/callback", authenticationHandler.CallbackFor("twitch"))
account.Post("/link/twitch", authenticationHandler.LinkFor("twitch"))
```

Create migration `00012_twitch_authentication.sql`. Drop and recreate only both named provider-kind check constraints with `twitch` included.

- [ ] **Step 4: Regenerate and run GREEN check**

Run: `task generate && go test ./internal/web ./internal/platform/postgres -run 'Twitch|ForwardMigrationIntroducesGameNickAndAuthFlows' -count=1`

Expected: PASS. Commit generated output only if changed.

- [ ] **Step 5: Commit**

```bash
git add internal/app/app.go internal/web/router.go internal/web/router_test.go internal/web/handler/authentication_test.go internal/platform/postgres/postgres_integration_test.go internal/platform/postgres/migrations/00012_twitch_authentication.sql
git add internal/web/pages/*_templ.go internal/auth/postgres/sqlcgen
git commit -m "feat(auth): wire Twitch login and identity constraints"
```

Stage generated paths only when changed.

### Task 4: Canonical documentation and operator configuration

**Files:**

- Modify: `.env.example`
- Modify: `README.md`
- Modify: `docs/PRODUCT.md`
- Modify: `docs/ARCHITECTURE.md`
- Modify: `docs/ROADMAP.md`

**Interfaces:**

- Document the exact four variables, callback URL, independent enablement, and intentional legacy-variable break.

- [ ] **Step 1: Update operator configuration**

Add this example and comments:

```text
LANSURFER_TWITCH_CLIENT_ID=
LANSURFER_TWITCH_CLIENT_SECRET=
LANSURFER_TWITCH_AUTH_ENABLED=false
LANSURFER_IGDB_ENABLED=false
```

State Twitch registration requires `<base URL>/auth/twitch/callback`. State old `LANSURFER_IGDB_*` variables no longer work.

- [ ] **Step 2: Align canonical documents**

PRODUCT: optional independent Twitch provider, local identity/linking, and no-email behavior. ARCHITECTURE: Twitch kind, shared OIDC machinery, shared credentials, flags, separate runtime capability state. ROADMAP: mark #40 completed post-1.0 only after code verification succeeds.

- [ ] **Step 3: Verify docs-adjacent behavior**

Run: `go test ./internal/platform/config -run 'Twitch|IGDB' -count=1 && git diff --check`

Expected: PASS and no whitespace errors.

- [ ] **Step 4: Commit**

```bash
git add .env.example README.md docs/PRODUCT.md docs/ARCHITECTURE.md docs/ROADMAP.md
git commit -m "docs: document Twitch authentication configuration"
```

### Task 5: Full verification and review

**Files:** Verify all changed files.

- [ ] **Step 1: Run project checks**

```bash
task test
task test:integration
task lint
task build
```

Expected: all commands exit 0.

- [ ] **Step 2: Inspect spec acceptance**

Verify all four capability states, no legacy variables, no Twitch email request, shared authentication behavior, and migration changes only provider-kind constraints.

- [ ] **Step 3: Confirm clean tree**

Run: `git status --short`

Expected: no output.

## Plan self-review

- Spec coverage: Tasks 1–4 cover credentials, flags, OIDC reuse, no-email profile, common flows, constraints, and docs. Task 5 proves integration.
- Placeholder scan: no incomplete implementation or unspecified validation steps.
- Type consistency: Task 1 produces `config.Twitch`; Task 2 produces `auth.TwitchConfig` and `DiscoverTwitchProvider`; Task 3 consumes both.
- Review focus: configuration boundaries appear in Task 1, scope/profile integrity in Task 2, and routes/database constraints in Task 3.
