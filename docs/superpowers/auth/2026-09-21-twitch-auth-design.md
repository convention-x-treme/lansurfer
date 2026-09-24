# Twitch Authentication Design

Status: Historical supporting design — implemented by #40
Date: 2026-09-21
Tracking issue: #40

## Authority and intent

This design supports the approved post-1.0 addition of optional Twitch authentication. `docs/PRODUCT.md` remains authoritative for product behavior, `docs/ARCHITECTURE.md` for durable technical decisions, and `docs/ROADMAP.md` for delivery status. Implementation must update those canonical documents so they no longer describe Twitch authentication as unscheduled or unsupported.

LANsurfer will offer generic OIDC, Discord, and Twitch as independent authentication choices. All three resolve through the existing local user, external identity, explicit linking, onboarding, and revocable session model. Twitch provider data never becomes a LANsurfer user ID or public identity.

## Scope

The change includes:

- optional Twitch authentication through Twitch's OpenID Connect authorization-code flow;
- Twitch identities in the existing provider registry and identity persistence model;
- login, new-account choice, explicit linking, unlinking, and Game Nick onboarding through the common authentication flow;
- shared Twitch application credentials for Twitch authentication and IGDB synchronization;
- explicit, independent enablement for Twitch authentication and IGDB synchronization;
- operator documentation and focused automated verification.

The change does not include:

- automatic account merging by email, username, or display name;
- Twitch-specific sessions, account records, or linking rules;
- email access or any Twitch API scope beyond OpenID Connect identity;
- avatars or other Twitch profile metadata;
- compatibility with the prototype-only `LANSURFER_IGDB_CLIENT_ID` and `LANSURFER_IGDB_CLIENT_SECRET` variables.

Breaking the old IGDB environment-variable names is accepted because LANsurfer remains a prototype. Existing deployments must rename their credentials and explicitly enable IGDB before upgrading.

## Provider adapter

Twitch supports OIDC discovery at `https://id.twitch.tv/oauth2/.well-known/openid-configuration` and publishes the fixed issuer `https://id.twitch.tv/oauth2`. LANsurfer will reuse its existing OIDC discovery, PKCE, nonce, token validation, and claim-decoding implementation.

The OIDC adapter will become narrowly configurable for provider kind, scopes, and authorization parameters. Generic OIDC keeps its current behavior and identity namespace. A Twitch constructor supplies:

```text
provider_kind: twitch
provider_key:  twitch
issuer:        https://id.twitch.tv/oauth2
scope:         openid
claims:        {"id_token":{"preferred_username":null}}
```

The Twitch profile mapping uses the verified ID token:

```text
subject:             sub
name:                preferred_username
preferred_username:  preferred_username
Game Nick suggestion: preferred_username when valid
email:               empty
```

The adapter must not request `email`, `email_verified`, or `user:read:email`. Twitch authentication therefore receives only the stable Twitch subject and display username required by issue #40.

## Common authentication flow

The existing provider registry gains `twitch` after `oidc` and `discord` in deterministic presentation order. Twitch uses the existing routes and handler behavior:

```text
GET  /auth/twitch
GET  /auth/twitch/callback
GET  /account/link/twitch
POST /account/unlink/twitch
```

The common flow remains:

```text
provider authorization
OAuth callback consumption
verified auth.Profile
existing/new-account decision
optional proof through another configured provider
identity linking
local LANsurfer session
```

No Twitch-specific business service or session path will be added. Existing browser binding, one-use flow state, PKCE, nonce validation, identity-conflict handling, and at-least-one-linked-identity rule apply unchanged.

## Persistence

Add a forward migration that expands the allowed `provider_kind` values from `oidc | discord` to `oidc | discord | twitch` for both `user_identities` and `auth_flows`.

Existing uniqueness constraints remain authoritative:

```text
UNIQUE(provider_kind, provider_key, subject)
UNIQUE(user_id, provider_kind, provider_key)
```

No new tables or columns are required. Twitch subjects remain provider-local identifiers and never replace local user UUIDs.

## Configuration

Shared Twitch application credentials replace the old IGDB-specific credentials:

```text
LANSURFER_TWITCH_CLIENT_ID
LANSURFER_TWITCH_CLIENT_SECRET
LANSURFER_TWITCH_AUTH_ENABLED
LANSURFER_IGDB_ENABLED
```

Both enable flags are strict booleans and default to `false`. Rules:

- if both capabilities are disabled, Twitch credentials are optional and neither integration starts;
- if either capability is enabled, both Twitch credentials are required;
- Twitch authentication enabled alone configures only the Twitch login provider;
- IGDB enabled alone configures only IGDB synchronization;
- both enabled reuse the same credentials without coupling their lifecycle;
- invalid booleans or partial required credentials fail configuration loading with a precise error.

`LANSURFER_IGDB_CLIENT_ID` and `LANSURFER_IGDB_CLIENT_SECRET` are removed. Documentation will provide the direct rename and explicit-enable upgrade instructions.

## Startup and failure behavior

When Twitch authentication is enabled, application startup performs Twitch OIDC discovery with the same bounded timeout used for generic OIDC. Discovery failure remains fatal because an advertised login provider cannot operate safely without verified metadata.

IGDB preserves its current asynchronous availability model. Enabling IGDB constructs its adapter from the shared credentials, but provider authentication and synchronization do not block HTTP startup.

The two capabilities share configuration values only. They do not share access tokens, runtime clients, retries, or enabled state.

## Presentation and privacy

Configured Twitch authentication appears as `Twitch` on login, account linking, and identity management surfaces through the existing provider-driven UI. Provider metadata remains visible only to the account owner. Public surfaces continue to use Game Nick.

No Twitch email, avatar, profile URL, or other provider metadata is stored or rendered. Existing escaping, CSRF, callback validation, and session protections remain unchanged.

## Verification

Implementation follows test-driven development with focused red-green cycles. Verification covers:

- configuration for all four Twitch-auth/IGDB enablement combinations;
- missing credentials, partial credentials, and invalid boolean values;
- generic OIDC retaining its current scopes and profile mapping;
- Twitch authorization requesting `openid` and `preferred_username`, but no email scope or claim;
- Twitch ID-token mapping into `auth.Profile`;
- deterministic provider availability and callback dispatch for three providers;
- Twitch login, linking, conflict, and unlink behavior through existing common services;
- PostgreSQL acceptance of `twitch` and rejection of unknown provider kinds;
- independent Twitch-auth and IGDB composition-root wiring;
- updated operator configuration examples and production build.

Run the focused package and integration tests during development, then the repository's authoritative `task test`, relevant PostgreSQL integration tests, `task lint`, and `task build` checks before completion.

## Rejected alternatives

### Bespoke Twitch OAuth and Helix profile lookup

This would duplicate authorization-code exchange and add another remote profile request. Twitch OIDC already supplies a verified subject and requested display username, so the extra path adds failure modes without satisfying another requirement.

### Configure Twitch as generic OIDC

This would blur operator intent, store Twitch identities under the generic `oidc` kind, and prevent independent provider presentation. A small configuration seam in the existing adapter preserves shared security machinery while retaining the explicit `twitch` provider kind.

### Implicit capability enablement from credentials

Credentials alone cannot express whether an operator wants Twitch authentication, IGDB, or both. Explicit flags provide the required independent enablement without duplicate credentials.
