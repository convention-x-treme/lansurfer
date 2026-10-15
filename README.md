# 🌊 LANsurfer

**Find your crew. Catch the next game.**

**LANsurfer is back.**

Originally built for LAN parties in the early 2000s.<br>
Reimagined as an open-source **Looking for Group (LFG)** platform for gaming communities.

Self-hosted and built for LAN parties, gaming events, and the people who bring them to life.

Hundreds of players. Dozens of games. And somewhere in the room, someone else is thinking: *“Anyone up for Warcraft III?”*

**You just need to find each other.**

[The idea](#-what-is-lansurfer) · [How it works](#-how-it-works) · [Project status](#-project-status) · [Get involved](#-get-involved)

> 🚧 **Under active development:** there is no stable release yet. The design and database may still change.

## 🎮 What is LANsurfer?

Maybe it's a quick round of Counter-Strike. Maybe it's FlatOut 2, an all-night OpenTTD session, or that obscure game you haven't launched in twenty years.

Chances are, someone brought it too.

LANsurfer aims to give everyone at the event a shared place to find out what others want to play, follow their favorite games, and get a group together. From Warcraft III and Age of Empires II to Trackmania, Quake III Arena, and Unreal Tournament — old favorites deserve another round.

The original use case is **CXT**, a LAN party with around **400 participants**. The goal is to make those spontaneous “let's play something” moments easier for LAN organizers and gaming communities everywhere.

## 🏄 How it works

LANsurfer keeps the flow simple and runs in the browser:

1. **Follow your games.** Pick the games you'd like to play and find them again with the **Followed only** filter on Games.
2. **Find players.** Create a Game Request with a start time, an optional player limit, and a short description.
3. **Join a game.** Browse requests, see who's still looking for players, and join in. Live updates keep player counts and new requests visible in the browser.

A request looks like this:

```text
Warcraft III
2 / 4 players · Starts now

“Casual 2v2 — getting back into WC3.
 Rusty players welcome 😄”

[ Join ]
```

Once the group is ready, it's time to launch the game.

**Follow games → Find players → Play together**

## 🌊 Built for your community

- **Your LAN, your instance.** Organizers can host LANsurfer on their own infrastructure and maintain the games available at their event.
- **A place for the classics.** The local game catalog can include the games your community actually plays, including the less obvious ones.
- **Easy to join in.** LANsurfer works in desktop and mobile browsers, with quick, readable game requests.
- **Built in the open.** Players, organizers, designers, and developers can help shape the project.

## 🦦 Meet the LANsurfer Otter

<img src="docs/mascot/initial-mascot.png" alt="LANsurfer otter wearing a headset and holding a surfboard" width="280">

An otter with a headset and a surfboard feels right at home here: social, at ease in the water, and ready for the next game.

The otter brings a little personality to LANsurfer's ocean-inspired identity. In the application, the wording stays familiar: games, players, and joining a game.

The [design system](DESIGN.md) defines the palette, typography, icons, mascot usage, and dark/light interface direction.

## 🗺️ Project status

**One LANsurfer installation serves one LAN party or gaming event context.**

- [Product definition](PRODUCT.md): what LANsurfer does and where its boundaries are.
- [Architecture](docs/ARCHITECTURE.md), [terminology](docs/TERMINOLOGY.md), and [design system](DESIGN.md): the other canonical references.
- [GitHub Issues](https://github.com/convention-x-treme/lansurfer/issues) and [milestones](https://github.com/convention-x-treme/lansurfer/milestones): current development work and proposals. There is no separate roadmap file.

## 🐳 Run LANsurfer locally

With Go, Node.js/npm, Task, and Docker Compose installed, start the development workflow:

```sh
task dev
```

`task dev` generates the required files, then runs LANsurfer as a local Go process. It starts repository PostgreSQL and waits for readiness when `LANSURFER_DATABASE_URL` is unset. If the URL is configured in your shell or `.env`, LANsurfer uses it unchanged and does not start PostgreSQL. Set `LANSURFER_POSTGRES_PORT` to use a non-default host port.

Use a dedicated development database in external mode. LANsurfer runs database migrations at startup.

Open <http://localhost:8080>. Check health and readiness with:

```sh
curl http://localhost:8080/healthz
curl http://localhost:8080/readyz
```

Expect `ok` from `/healthz` and `ready` from `/readyz`. Ctrl+C stops the app, not managed PostgreSQL. Run `task db:down` to stop development Compose services; it keeps the database volume and does not reset data.

To run both LANsurfer and PostgreSQL in Compose explicitly, use:

```sh
docker compose up --build
```

Stop the full stack with `docker compose down`; the PostgreSQL data volume remains.

### Configure OIDC login

LANsurfer uses generic OpenID Connect discovery and server-side PostgreSQL sessions. Both `task dev` and the full Compose stack default to the CXT development realm and `lansurfer` public client below. The provider must allow the exact callback URL `http://localhost:8080/auth/callback`:

```sh
LANSURFER_BASE_URL=http://localhost:8080
LANSURFER_OIDC_ISSUER=https://auth.convention-x-treme.de/realms/local-dev
LANSURFER_OIDC_CLIENT_ID=lansurfer
# Leave empty for the local public PKCE client.
LANSURFER_OIDC_CLIENT_SECRET=
```

Set the issuer URL itself rather than its `/.well-known/openid-configuration` endpoint; LANsurfer discovers that endpoint automatically. The client secret is optional for public PKCE clients and required when the provider configures a confidential client. For an exposed deployment, use an HTTPS base URL; LANsurfer then marks its opaque session and login-flow cookies `Secure`. The identity provider subject is linked to a separate local LANsurfer user, and matching email addresses are never used to merge accounts.

For the local Keycloak client, enable **Standard flow**, leave **Client authentication** disabled, require the `S256` PKCE method, and configure `http://localhost:8080/auth/callback` as the exact valid redirect URI. A production server deployment should normally use a confidential client with client authentication enabled and provide its secret through `LANSURFER_OIDC_CLIENT_SECRET`.

Every player chooses a unique, case-insensitive public **Game Nick** the first time they sign in. It is the only public player identity: authentication-provider names and emails stay private to the account owner and are visible only on that owner's own `/account` page.

### Configure Discord login

Discord authorization-code login is an independent, optional provider alongside OIDC; configure either one, both, or neither:

```sh
LANSURFER_DISCORD_CLIENT_ID=
LANSURFER_DISCORD_CLIENT_SECRET=
```

In the [Discord Developer Portal](https://discord.com/developers/applications), create an application, add an OAuth2 redirect of `http://localhost:8080/auth/discord/callback` (or your deployment's base URL), and request only the `identify` scope — LANsurfer never asks for a Discord email address and never persists Discord access/refresh tokens beyond the login request that needs them.

OIDC and Discord identities resolve to the same local User only through an explicit, signed-in `/account` link, or by proving an existing account through the other configured provider during onboarding. LANsurfer never merges accounts by matching email address, name, or Game Nick.

Discord notification delivery (posting Game Request updates to a Discord channel) is a separate, optional integration with its own OAuth credentials:

```sh
LANSURFER_DISCORD_WEBHOOK_CLIENT_ID=
LANSURFER_DISCORD_WEBHOOK_CLIENT_SECRET=
```

Configuring Discord login does not enable delivery, and configuring delivery does not enable login. Delivery additionally requires `LANSURFER_DATA_ENCRYPTION_KEY` to be set, since webhook credentials are encrypted at rest.

### Configure Twitch login and IGDB discovery

Twitch login and IGDB discovery share one Twitch Developer Application but are independently enabled:

```sh
LANSURFER_TWITCH_CLIENT_ID=
LANSURFER_TWITCH_CLIENT_SECRET=
LANSURFER_TWITCH_AUTH_ENABLED=false
LANSURFER_IGDB_ENABLED=false
```

Set either enable flag to `true` only after configuring both credentials. Register `<base URL>/auth/twitch/callback` as the Twitch OAuth redirect URI when Twitch login is enabled. Twitch login requests only OpenID Connect identity and a display username; it does not request email. The old `LANSURFER_IGDB_CLIENT_ID` and `LANSURFER_IGDB_CLIENT_SECRET` variables no longer work.

IGDB covers are served same-origin through LANsurfer; player browsers do not contact `images.igdb.com`. On a cache miss LANsurfer fetches a known cover from that fixed origin server-side. `LANSURFER_COVER_CACHE_DIR` selects a persistent disk cache; when it is unset, LANsurfer uses a fixed 64 MiB in-process LRU. The production Compose configuration sets it to `/var/cache/lansurfer/covers` and mounts the `cover-cache` named volume there. Deleting the configured cache directory resets the disk cache; known covers are fetched again on demand.

Provider avatars (Discord avatar, Twitch or OIDC `picture`) use the same proxy and cache: browsers load `/media/avatar/{key}` from LANsurfer only, and on a cache miss the server fetches the user's selected picture from the provider host.

When LANsurfer runs behind a reverse proxy, set `LANSURFER_BASE_URL` to the
public HTTPS origin. Set `LANSURFER_TRUSTED_PROXIES` to a comma-separated list
of the proxy IP addresses or CIDR ranges only when client-IP logging should use
`X-Forwarded-For` (for example, `10.20.0.5,10.21.0.0/16`). LANsurfer ignores
forwarded client addresses from every other peer; do not use a broad range that
includes untrusted clients. Proxy TLS headers do not control cookie security—the
public HTTPS `LANSURFER_BASE_URL` does.

### Configure the chat rate limit (optional)

```sh
LANSURFER_CHAT_RATE_LIMIT=5
LANSURFER_CHAT_RATE_WINDOW=20s
```

A player may send `LANSURFER_CHAT_RATE_LIMIT` messages per `LANSURFER_CHAT_RATE_WINDOW` (a Go duration of at least `1s`) in one Game Request. Both default to the values shown when unset.

### Configure minimum instance identity (optional)

```sh
LANSURFER_INSTANCE_NAME=
```

Optional and unset by default, in which case the application uses the fixed "LANsurfer" name and
branding everywhere. When set, it is trimmed, limited to 64 Unicode characters, may not contain
control characters or line breaks, and appears only as a secondary label next to the wordmark, in
the page title, and as the installed PWA's name/short name — it never changes LANsurfer's colors,
typography, or other fixed branding. LANsurfer also serves `/manifest.webmanifest` and
`/service-worker.js` for installability; an administrator can check their resolved values, and
whether the deployment is a secure context (required for installation and Web Push), at
`/admin/pwa`.

Brave disables its push service by default. Brave users must turn on "Use Google services for push
messaging" in `brave://settings/privacy` and restart Brave before they can enable Web Push; the
notification settings page tells them so when subscribing fails.

### Configure legal identity

```sh
LANSURFER_LEGAL_ENTITY_NAME=
LANSURFER_LEGAL_ADDRESS=
LANSURFER_LEGAL_CONTACT_EMAIL=
LANSURFER_IMPRINT_URL=
```

LANsurfer serves a public Terms of Service at `/terms` and Privacy Policy at `/privacy`, linked
from the footer of every screen. These values fill in the operator (data controller) on both
pages; unset values show as "not configured" and the imprint link is hidden. Entity name and
address are single-line (200 and 300 characters), the contact email must be a bare address, and
the imprint must be an absolute `http(s)` URL. **Discord login requires the entity name and
contact email**; LANsurfer refuses to start without them, because Discord requires a privacy
policy with a responsible contact. Register `<base URL>/terms` and `<base URL>/privacy` in the
Discord Developer Portal. The page text describes the reference deployment (30-day log retention,
self-operated server in a colocation data center); another operator edits
`internal/web/pages/legal.templ` to match theirs and has both texts legally reviewed.

### Promote the first administrator

Every new account starts with the `user` role. Log in once to create your local account, then list users from the configured database:

```sh
task admin:users
```

Copy your user ID from the output and promote it:

```sh
task admin:promote USER_ID=2c359aee-7690-4975-a2f8-1730cf37f42b
```

These tasks run the Go CLI locally. They use `LANSURFER_DATABASE_URL` when configured or the local development URL otherwise; they do not start PostgreSQL. Ensure the database is running before using them.

Reload LANsurfer after the command succeeds. Existing sessions resolve the local role on every request, so you do not need to log out. The equivalent binary commands are `lansurfer admin users` and `lansurfer admin promote --user-id <UUID>`.

Once promoted, an administrator can also inspect and retry stuck notification deliveries from the binary: `lansurfer admin notifications problems` lists exhausted, permanent, and uncertain deliveries, and `lansurfer admin notifications retry --delivery-id <ID>` (or `retry-uncertain` for a delivery in the uncertain state) reopens one for a fresh attempt.

To create the Web Push VAPID keys and the data-encryption key, run `lansurfer admin notifications generate-secrets` (from source: `go run ./cmd/lansurfer admin notifications generate-secrets`; with Compose: `docker compose exec lansurfer /lansurfer admin notifications generate-secrets`). It needs no configuration or database, prints env-ready `KEY=value` lines to stdout only, and stores nothing. Copy the lines into your `.env` and set `LANSURFER_WEBPUSH_SUBJECT` (`mailto:` or `https://`) yourself. Keep the output secret and out of version control. If you lose `LANSURFER_DATA_ENCRYPTION_KEY`, stored notification credentials become unreadable and users must register their devices again.

### Prometheus metrics

`GET /metrics` requires a metrics-only API key; browser sessions do not grant access.
Before a key exists, scrapes return `401 Unauthorized`. Sign in as an administrator,
open **Administration → Metrics** (`/admin/metrics`), and select **Generate Metrics
API Key**. Save the full key from that response: later page loads cannot retrieve it.
LANsurfer stores only its SHA-256 hash in PostgreSQL. **Rotate Metrics API Key**
atomically replaces the instance's one active key; the previous key stops working
for new requests immediately, including requests to another application process.
An already-authenticated scrape may finish. Rotation requires updating the scraper's
credentials file. Database backups contain the hash, not a recoverable plaintext key;
rotate if the scraper's copy is lost.

Use HTTPS outside loopback. Restrict access to the scraper at the reverse proxy or
firewall as well, and never put the key in a URL or log `Authorization` headers.
Prometheus and Grafana are optional external tools, not required LANsurfer services.
An importable [Grafana monitoring dashboard](docs/grafana/README.md) covers scrape/SQL
health, HTTP latency/errors, pool pressure, runtime usage and durable product totals.
Store the key in a file readable only by Prometheus and configure:

```yaml
scrape_configs:
  - job_name: lansurfer
    scheme: https
    metrics_path: /metrics
    scrape_interval: 30s
    scrape_timeout: 10s
    static_configs:
      - targets: [lansurfer.example.org]
    authorization:
      type: Bearer
      credentials_file: /etc/prometheus/lansurfer-metrics-key
```

This sends `Authorization: Bearer <metrics-api-key>`. Scrape output includes:

- Process-local HTTP counters/statuses, duration histograms, and in-flight requests:
  `lansurfer_http_requests_total`, `lansurfer_http_request_duration_seconds`, and
  `lansurfer_http_requests_in_flight`. SSE contributes to in-flight requests until it
  ends, and its full stream lifetime contributes to duration.
- Standard `go_*` runtime and `process_*` metrics. These and HTTP counters reset on
  process restart; they are not durable event-reporting totals.
- `lansurfer_db_pool_*` gauges for maximum, total, acquired, idle and constructing
  connections; process-local counters for acquires, empty/waiting acquires, acquire
  time, empty-acquire wait time, canceled acquires, new connections, and destruction
  on maximum lifetime/idle time. Durations are in seconds.
- Current-state gauges: `lansurfer_users`, `lansurfer_browser_sessions` (unexpired
  sessions, not online users), `lansurfer_game_requests_open`,
  `lansurfer_game_requests_playing`, `lansurfer_game_requests_active`, and
  `lansurfer_game_follows`.
- PostgreSQL-backed durable counters: `lansurfer_game_requests_created_total`,
  `lansurfer_game_request_joins_total`, `lansurfer_game_request_leaves_total`,
  `lansurfer_game_requests_cancelled_total`, `lansurfer_game_requests_closed_total`,
  and `lansurfer_games_started_total`. The automatic Host membership is a creation,
  not a join. Only the first explicit `open → playing` transition counts as a start.
  Rejected, duplicate and rolled-back changes do not count.

Durable totals combine retained live activity with anonymous deletion aggregates
in one PostgreSQL snapshot. They survive process restarts and account/history
deletion. Upgrade includes reconstructed history retained at migration time once;
later backfills do not count as new events. History deleted before the metrics
migration and prototype actions never recorded cannot be recovered: upgraded totals
are a historical lower bound. No start time is invented for old closed requests.
Restoring an older database backup also restores its older totals.

Scrape-time queries have a shared three-second deadline and only one scrape is
collected at a time; a concurrent scrape returns `503`. If an optional SQL source
fails, its metrics are omitted and `lansurfer_metrics_collection_success` reports
`0` for `source="aggregates"` or `source="product_totals"`; runtime, HTTP and pool
metrics remain available. Authentication-storage failure returns `503` and fails
closed. Collection failures do not fail player workflows. If deletion-time aggregate
preservation fails, PostgreSQL logs `metrics counter preservation failed` and allows
account deletion; affected reporting totals may be incomplete. Do not modify or
remove metrics tables outside supported migrations.

Metrics contain only aggregate counts and bounded operational labels. HTTP labels
use route templates, bounded methods/statuses, and `unmatched` rather than raw paths.
Never add Game Nicks, user/Game/Game Request/session IDs, provider identities, query
parameters, descriptions, chat, session-information values, or credentials to
labels or sample values. Protect access to the Prometheus/Grafana data as operator data.

The Compose credentials are development defaults; do not use them for an exposed deployment. See the [contributing guide](CONTRIBUTING.md) for local Go development, configuration, and checks.

## 🤝 Get involved

Have you organized a LAN? Spent half the evening trying to find a fourth player? We'd love your perspective.

You can help with interface design, development, documentation, bug reports, and testing at LAN events. Share a concrete use case in an [issue](https://github.com/convention-x-treme/lansurfer/issues), or start with the [contributing guide](CONTRIBUTING.md).

To reproduce load, browser-recovery, and network-fault scenarios locally, see the [isolated load-test harness](tools/loadtest/README.md). It runs separately from the normal development stack and provides Task commands for setup, test runs, and cleanup.

For the technical side, LANsurfer is a modular Go monolith with server-rendered pages and PostgreSQL. The [architecture](docs/ARCHITECTURE.md) explains the choices; [development rules](AGENTS.md) cover repository conventions and commands.

## 📜 License

LANsurfer is free and open-source software under the [GNU Affero General Public License v3.0 (AGPL-3.0)](LICENSE).

---

Sometimes the best game at a LAN is the twenty-year-old one you suddenly discover ten other people also want to play.

**Built for LAN parties. Built for gaming communities. Built in the open.**
