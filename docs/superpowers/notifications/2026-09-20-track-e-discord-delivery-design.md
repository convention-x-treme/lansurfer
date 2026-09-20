# Track E3 — Discord Delivery Design

Status: Design complete — awaiting shared-understanding approval
Date: 2026-09-20
Tracking issue: #28
Branch: docs/track-e-notifications-sdd
Base: e46afb39f0ce680aa81212eea4e98f6b84ba72a5
Depends on: approved E1 Durable Notification Foundation design

## Authority and intent

This is the bounded Discord-delivery SDD artifact for Track E. It depends on the approved E1 Durable Notification Foundation design.

It does not override `AGENTS.md`, `docs/PRODUCT.md`, `docs/ARCHITECTURE.md`, or `docs/ROADMAP.md`.

Discord authentication and Discord notification delivery are separate integrations. E3 must not derive public notification identity from Discord authentication data.

## Goal

Deliver one synchronized Discord message per Game Request to at most one administrator-configured instance-level Discord destination, using Discord's `webhook.incoming` OAuth flow without a bot, gateway connection, pasted webhook URL, or coupling external delivery failure to Game Request writes.

## Settled repository and canonical constraints

- E3 consumes the approved E1 durable notification foundation.
- There is at most one Discord destination per LANsurfer instance.
- Destination setup uses Discord OAuth with the `webhook.incoming` scope.
- Users/operators do not paste webhook URLs or webhook tokens.
- Discord delivery is independent from Discord login/account linking.
- Global administrators configure the destination.
- Store Discord webhook identifiers/tokens encrypted at rest with AES-GCM and the operator-provided 32-byte Track E data-encryption key.
- Never reveal stored webhook secrets back through UI, logs, diagnostics, or API responses.
- Create one Discord message per Game Request.
- Initial message creation uses webhook execution with `wait=true` so LANsurfer receives and persists the Discord message ID.
- Later Game Request activity edits the same Discord message rather than posting a stream of new messages.
- Persist the external Discord message reference through E1/E3 durable state.
- Later/older work must not overwrite a newer synchronized Discord representation; E3 owns Discord-specific stale-update/supersession behavior.
- External Discord failure never rolls back or fails a Game Request action.
- E1 retry/permanent/exhausted/uncertain/manual-retry semantics apply.
- Message content may include:
  - Game;
  - planned start time;
  - current request status;
  - current capacity/free-place summary;
  - creator's public LANsurfer Game Nick;
  - short sanitized request description;
  - same-origin/deployment deep link back to LANsurfer.
- Do not mirror the full participant roster.
- Do not mirror durable request activity/history.
- Do not use Discord username/display name/email as public player identity.
- Always send an empty `allowed_mentions` policy so user-authored text cannot trigger mentions.
- No Discord bot user, Gateway connection, slash commands, reaction handling, or inbound Discord interaction is part of E3.
- E3 must remain inside the modular Go monolith and use narrow transport/provider interfaces.
- PostgreSQL remains the only durable coordination/persistence service.

## Approved E1 behavior inherited by E3

- Delivery work is durable and restart-safe.
- PostgreSQL claiming uses durable leases.
- Automatic retries use the approved capped retry budget/backoff.
- Provider acceptance ambiguity may become `uncertain`; uncertain is never blindly auto-retried.
- `permanent`, `exhausted`, and `uncertain` are visible to authorized administrators for diagnosis/manual retry.
- `suppressed` is available when an originally materialized delivery becomes intentionally ineligible.
- Technical delivery history is retained in 1.0; there is no user-facing inbox.
- E1 must not grow a generic ordered-stream/provider-registry abstraction for E3.

## Current implementation facts

- Existing Discord login uses `internal/auth/discord.go` with the same standard Discord OAuth2 authorization/token endpoints but only the `identify` scope.
- Existing environment configuration currently exposes one optional `LANSURFER_DISCORD_CLIENT_ID` / `LANSURFER_DISCORD_CLIENT_SECRET` pair for Discord authentication.
- Existing global administrator authorization uses `RequireAdmin`.
- The existing Account/Discord authentication flow must not be reused as evidence that the administrator's Discord identity is the public Game Request identity.
- The data-encryption key is already part of the approved Track E architecture and is required for encrypted external credentials.
- Discord's current `webhook.incoming` authorization-code flow returns the created webhook object in the token response, including webhook ID/token and guild/channel IDs; each authorization creates a new webhook.
- Discord webhook execution with `wait=true` returns the created Message object, which provides the message ID E3 must persist before later edits.
- Editing a webhook message must send an explicit empty `allowed_mentions` again; Discord does not inherit the original create request's mention policy for later edits.
- The webhook-token API can read/modify/delete the webhook and read/edit/delete messages created by that same webhook token without introducing a bot/gateway integration.
- Discord currently distinguishes JSON error code `10015` (Unknown Webhook) from `10008` (Unknown Message), which lets the adapter separate broken-destination handling from single-message recovery.
- Discord HTTP 429 responses provide `Retry-After` / `retry_after`; do not hard-code Discord rate-limit buckets or fixed quotas.
- Discord explicitly advises clients not to continue using a webhook that returns 404, because repeated invalid requests can contribute to temporary restrictions.


## Settled decisions

- **Credential independence:** Discord login keeps the existing `LANSURFER_DISCORD_CLIENT_ID` / `LANSURFER_DISCORD_CLIENT_SECRET` configuration. Discord delivery gets separate operator credentials, e.g. `LANSURFER_DISCORD_WEBHOOK_CLIENT_ID` / `LANSURFER_DISCORD_WEBHOOK_CLIENT_SECRET`. Operators may point both integrations at the same Discord application, but neither feature implicitly enables the other.
- **Setup-flow security:** Discord-destination setup starts only from an authenticated global administrator through a CSRF-protected POST. Persist a short-lived, one-use OAuth state bound to the initiating administrator, browser/session, and the explicit Discord-destination configuration operation. The callback validates that binding. The flow never creates or links a Discord authentication identity.
- **Destination replacement:** an existing destination remains active while a replacement OAuth flow is in progress. After a successful callback and durable storage of the new destination, atomically switch LANsurfer to the new destination, then best-effort delete the old remote webhook. Cleanup failure does not roll back the successful replacement but must remain diagnosable to administrators.
- **Disconnect:** disconnect immediately removes/deactivates the local destination and suppresses still-pending deliveries tied to that destination. LANsurfer best-effort deletes the remote webhook. Existing Discord messages remain in the channel. Reconnect creates a new destination and never replays historical work.
- **Scopes and metadata:** request only `webhook.incoming` for notification delivery. Do not request `identify`, `guilds`, bot permissions, or Gateway access merely to improve display labels. Persist only the non-sensitive destination metadata returned by the webhook flow that is useful for identification/diagnosis (such as webhook ID, guild ID, channel ID, and webhook name when available) alongside encrypted secrets.
- **OAuth token retention:** after a successful `webhook.incoming` setup, do not persist the OAuth access token or refresh token. Persist only the webhook credentials needed for delivery plus non-sensitive destination metadata. Changing destination requires a new explicit authorization flow.
- **Create from current state:** if the initial Discord create is delayed or retried after later Game Request activity occurred, render the then-current Game Request snapshot rather than replaying the original `created` snapshot. Older update work may be absorbed/superseded by the current synchronized state.
- **Serialized external mutation:** allow at most one Discord create/edit mutation at a time for a given destination + Game Request. Persist the external message ID and the highest applied/request state version needed to prevent an older mutation from overwriting a newer synchronized representation.
- **Supersession/coalescing:** multiple rapid activities may collapse into one edit to the latest current Game Request snapshot. Individual durable work/delivery records remain technically traceable; externally obsolete intermediate deliveries may finish as `superseded`.
- **Message format:** use one compact Discord embed per Game Request. Include Game, planned start, current status, current players/capacity or free-place summary, creator Game Nick, short sanitized description, LANsurfer deep link, and minimal instance context. Do not include the full roster, activity history, or join/leave actor identities.
- **Description handling:** treat user-authored request description as plain content, not as permission to control Discord presentation. Neutralize Discord formatting/control characters as needed, respect the existing LANsurfer description limit, do not allow user-controlled embed URLs/structure, and send an empty `allowed_mentions` on every create and edit.
- **Deleted/broken webhook:** a definitive unknown/deleted-webhook result is permanent for the current destination. Mark the local destination broken/disconnected and stop creating new deliveries to it until an administrator explicitly reconnects. Reconnect never replays old work.
- **Deleted message recovery:** if the webhook still exists but the stored Discord message no longer exists, clear/replace the stale message reference and create one new message from the current Game Request snapshot with `wait=true`. Do not replay historical intermediate activity.
- **Encrypted credential shape:** encrypt both Discord webhook ID and webhook token together as a versioned AES-GCM credential blob. Guild ID, channel ID, webhook display name, connection state, and diagnostic timestamps may remain queryable metadata. Never expose webhook ID/token or reconstruct/display a webhook URL.
- **Provider Retry-After:** Discord 429 responses use E1 retry scheduling and honor Discord's explicit `Retry-After` in full, even if it exceeds LANsurfer's normal five-minute self-computed backoff cap.
- **Configuration fail-closed:** if both Discord-delivery client credentials are absent, E3 is disabled normally. Partial Discord-delivery OAuth configuration is a startup error. If Discord delivery is configured but the Track E data-encryption key is absent/invalid, startup fails rather than storing secrets in plaintext or silently disabling the configured integration.
- **Create/edit result classification:** Discord create and edit are intentionally classified differently. A create failure known to occur before transmission, explicit 429, or explicit 5xx is retryable. A create timeout/connection loss after the request may have reached Discord is `uncertain` because replay could duplicate the message. Edits to a known message ID are replay-safe for the same current snapshot and treat network/5xx/429 as retryable. `10008 Unknown Message` enters deleted-message recovery; `10015 Unknown Webhook` breaks the destination; other non-retryable 4xx responses are permanent without automatically deleting a still-known destination.
- **Post-connect live activity:** connecting/replacing a destination never replays old work. However, a later new live activity for an already-existing Game Request may create that request's first message in the new destination, rendered directly from the current snapshot.
- **Broken destination state:** retain safe destination metadata and the failure timestamp/reason for administrator diagnosis, but delete the unusable encrypted webhook credential blob. Stop creating new deliveries while broken. Reconnect replaces the broken destination with a newly authorized webhook. Explicit disconnect removes the remaining local destination metadata.
- **Admin surface:** expose Discord delivery under the existing administrator area with at least `Not configured`, `Connected`, and `Broken` states; show only safe destination metadata and diagnostics, plus Connect/Reconnect/Disconnect and access to E1 delivery diagnostics. Do not add a separate send-test-message workflow in 1.0.

## Implementation details intentionally delegated

The E3 design frontier is closed. Remaining choices are implementation details that do not alter the settled product, security, or architecture contract, including exact SQL column/type names, exact Go type/function names, OAuth route naming, final administrator UI copy/layout, and the exact automated/live Discord validation matrix.

Those details must follow the repository's existing admin UI, OAuth security, configuration, logging, and test conventions. No E3 implementation plan is authorized until shared understanding is explicitly approved.
