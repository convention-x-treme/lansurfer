# Demo instance (demo.lansurfer.de) — design

Status: feature-branch working spec. Harvest into canonical docs before merge (see "Canonical doc updates").
Issue: #227

## Problem

Visitors of the project website should try the real application without self-hosting. A public instance accumulates personal data, vandalism, and stale content, so it needs a daily reset and guardrails against destructive or externally visible admin actions.

## Approval note

This changes approval-gated areas listed in AGENTS.md: product scope (demo mode), security/role model (self-service admin in demo mode), deployment (reset timer). Maintainer decisions below were made on 2026-10-24; final approval happens on the PR that updates PRODUCT/ARCHITECTURE.

## Decisions (maintainer, 2026-10-24)

| Topic | Decision |
| --- | --- |
| Login | Real configured providers (Discord/Twitch/OIDC). No demo login, auth model unchanged. |
| Reset | `lansurfer admin demo reset --legal-dir DIR`, run by host systemd timer. |
| Reset time | Daily 04:00 Europe/Berlin. |
| Seed | Minimal: no fixture players/requests. Catalog comes from IGDB (enabled on demo). |
| Kept across reset | IGDB provider index and sync state (avoids daily baseline resync). Legal texts are re-applied from files. |
| Banner | Every page shows a demo notice with the next reset time. |
| Admin | Signed-in non-admins see "Get admin rights" in the banner; one click promotes themselves. |
| Blocked in demo | Game delete, merge, alias remove, IGDB unlink, IGDB sync, Discord delivery connect/reconnect/disconnect, metrics key rotate. Button stays; action answers with a notice "This action is disabled in the demo." |
| Not blocked | Legal text editing, game create/rename/activate, IGDB link. Accepted risk: vandalism visible ≤ 24 h, legal texts restored at reset. |
| Infra | `deploy/demo/`: Compose overlay, systemd service + timer, README. Image pinned manually like self-hosting. |

## Configuration

| Variable | Default | Meaning |
| --- | --- | --- |
| `LANSURFER_DEMO_MODE` | `false` | Strict boolean. Enables banner, self-promotion route, blocked actions, and permits `admin demo reset`. |
| `LANSURFER_DEMO_RESET_TIME` | `04:00` | `HH:MM`, required format when demo mode is on. Display only. |
| `LANSURFER_DEMO_RESET_TIMEZONE` | `Europe/Berlin` | IANA zone, validated with `time.LoadLocation`. Display only. |

The schedule truth is the systemd timer; the two display variables must match it (documented in `deploy/demo/README.md`). ponytail: duplicated schedule; move scheduling into the app only if drift ever bites.

Validation in `internal/platform/config`: invalid time/zone with demo mode on is a startup error. Demo variables are ignored when demo mode is off.

## Behavior

### Banner

- Rendered by the shared layout on every page (logged in and out, including landing, login, legal pages), via request-context middleware like `InstanceName` (`middleware.Demo`). Layout reads `DemoNextReset(ctx)`; zero value means no banner.
- Text: "Demo instance — all data is reset daily. Next reset: `<time>`." The next reset instant is computed per request from time + zone (DST-correct via `time.Date` in that location) and rendered with the existing `<time data-local-time>` pattern, so `app.ts` shows it in the viewer's zone.
- English only (PRODUCT §5). Uses an existing notice/alert component; no new design token. Accessible: `role="status"` is not needed (static content); plain landmark inside the header region.

### Self-service admin

- `POST /demo/admin` registered only in demo mode, inside the authenticated group with `RequireGameNick` and the existing CSRF middleware.
- Calls `user.Service.PromoteToAdmin(ctx, currentUserID)` — always the session's own user, never a submitted ID.
- Redirects to `/admin/games?notice=demo-admin-granted`.
- Button appears in the banner only for signed-in users with a Game Nick and role `user`.

### Blocked admin actions

- Middleware `handler.DemoForbidden(redirectTo string)` wraps exactly the routes listed in the decisions table when demo mode is on (wired in `internal/web/router.go`; routes are not wrapped when off).
- Behavior: no service call; redirect via the existing admin redirect helper (HTMX-aware) to the owning admin page with `?notice=demo-forbidden`; the notice renders "This action is disabled in the demo." through the existing `notice(r)` mapping.
- Redirect targets: game routes → `/admin/games`, IGDB sync → `/admin/igdb`, Discord → `/admin/discord`, metrics → `/admin/metrics`.
- This is deployment policy at the transport edge, not a business rule; domain packages stay unaware of demo mode.

### Reset command

`lansurfer admin demo reset --legal-dir DIR`

1. Load config; refuse with a clear error unless `LANSURFER_DEMO_MODE=true` (protects production databases).
2. Read and validate `DIR/terms.md`, `DIR/privacy.md`, `DIR/imprint.md` (trimmed, non-empty, ≤ 20,000 chars, same rule as `legal.Store`) **before** touching data. `imprint.md` required only when `LANSURFER_IMPRINT_ENABLED`.
3. Connect, take the migration advisory lock, run UP migrations (same path as startup), so the reset never runs against an older schema.
4. One transaction: list base tables in schema `public` from `pg_tables`, subtract the keep-list, `TRUNCATE ... RESTART IDENTITY CASCADE`.
5. Save each legal document through `legal.Store.Save` (marks them non-sample).
6. Print a one-line summary (tables truncated, legal docs written).

Keep-list (only these survive):

```text
goose_db_version
igdb_game_index
igdb_game_index_multiplayer_modes
igdb_provider_state
igdb_sync_generations
metrics_access_key      # rotate is blocked; keeps the operator's scrape working
events                  # legacy prototype table, empty on demo, see ARCHITECTURE "Legacy Event data"
```

Everything else is truncated by default, so tables added by future migrations reset automatically instead of silently leaking data across days. `CASCADE` is safe: verified that no kept table references a truncated one (IGDB index has no FK to `games`). An integration test pins this (see Verification).

Why not module services: the reset is an operator command that empties the whole database, analogous to restoring an empty database; going through each module's delete paths would be a large surface for no invariant gain. It lives in `internal/platform/postgres` (infrastructure), is reachable only through `app.ResetDemo`, and never runs inside the HTTP process.

Effects (document in PRODUCT): all accounts, identities, sessions, Game Requests, chat, follows, materialized Games, push subscriptions, notification state, and product counters are deleted daily. Everyone is signed out. Prometheus counters reset (rate() handles it).

### Reset orchestration

`deploy/demo/lansurfer-demo-reset.service` (oneshot):

```text
docker compose stop lansurfer
docker compose run --rm lansurfer admin demo reset --legal-dir /legal
docker compose start lansurfer
```

The app is stopped during reset so no in-memory state (SSE broker, notification workers, caches) survives against an emptied database; restart also runs the startup avatar prune, erasing cached avatar bytes of deleted users. Downtime is seconds at 04:00. `ExecStopPost` / failure path always runs `docker compose start lansurfer`, so a failed reset never leaves the demo down.

`lansurfer-demo-reset.timer`: `OnCalendar=*-*-* 04:00:00 Europe/Berlin`, `Persistent=true`.

`deploy/demo/compose.demo.yaml` overlay on `deploy/compose.production.yaml`: sets demo env vars, `LANSURFER_IGDB_ENABLED=true`, `LANSURFER_IMPRINT_ENABLED=true`, mounts `./legal:/legal:ro`.

## Privacy

Real provider identities (name, avatar URL, email where the provider sends it) are stored for at most until the next reset. The demo privacy text (`deploy/demo/legal/privacy.md`, operator-supplied) must state retention ≤ 24 h, providers used, and that Game Nicks, requests, and chat are public to other demo visitors.

## Verification

- Integration (`internal/platform/postgres`): seed rows in every table that has data paths, run reset; assert every non-kept public table is empty and kept tables keep their rows; legal documents equal file contents and are non-sample; reset refuses when demo mode is off; reset fails before truncation on invalid legal files.
- Handler/router tests: in demo mode each blocked route redirects with `demo-forbidden` and never calls the service fake; with demo mode off routes behave as before; `/demo/admin` is 404 when off, promotes only the session user when on, requires CSRF.
- Config tests: defaults, invalid time/zone rejected only in demo mode.
- Layout test: banner renders next reset instant across a DST boundary (Europe/Berlin last Sunday of October/March).
- Smoke: `task dev` with `LANSURFER_DEMO_MODE=true`, sign in, get admin, try delete → notice; run `go run ./cmd/lansurfer admin demo reset --legal-dir <tmp>` → signed out, data gone, IGDB search still works.

## Canonical doc updates (before merge)

- `PRODUCT.md`: new section "Demo mode" (banner, self-service admin, blocked actions, daily deletion).
- `docs/ARCHITECTURE.md` §15 config vars, §20 operations (reset command, keep-list rule, stop/reset/start orchestration), §21 security boundary note (self-promotion only in demo mode).
- `docs/CONFIGURATION.md`: three variables.
- `docs/TERMINOLOGY.md`: "Demo mode", "Demo reset".
- `cmd/lansurfer` help text.
- `deploy/demo/README.md`: setup, timer install, matching display vars, legal files, IGDB credentials.

## Open inputs from maintainer

- Demo legal texts (terms, privacy with ≤ 24 h retention, imprint).
- Which providers get demo OAuth apps + callback URLs `https://demo.lansurfer.de/...`.
- Twitch app credentials for IGDB on the demo host.

## Out of scope

Demo login provider, seeded fixture players/requests, automatic image updates, in-app scheduler, per-visitor sandboxes, rate limits beyond existing ones.
