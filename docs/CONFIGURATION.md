# Production .env configuration reference

This reference explains **every setting** in the LANsurfer production
`production.env.example` (included with each version-matched GitHub Release).
For the shortest installation path, use the
[self-hosting quickstart](SELF_HOSTING.md).

Download `compose.yaml` and `production.env.example` from the **same
release**, copy the latter to `.env`, and configure it locally. The raw
source template under `deploy/` contains an unresolved image marker:
**use the release download, not the source template**.

For a normal installation, you only need to provide
`POSTGRES_PASSWORD`, `LANSURFER_BASE_URL`, and the credentials for
**at least one player login provider**. Image pins are prefilled by
the release; all other settings are optional or conditional.

Treat `.env` as secret: `chmod 600 .env`. Never commit it or post the
expanded `docker compose config` output to a public issue.

## Images and server settings

| Setting | When needed | Explanation and valid values |
| --- | --- | --- |
| `LANSURFER_IMAGE` | Release-provided | Exact LANsurfer version **and immutable multi-platform digest**. Do not change during initial installation. For updates, use the pinned image reference from the next published release, after reviewing upgrade compatibility. |
| `POSTGRES_IMAGE` | Release-provided | Digest-pinned PostgreSQL image. The v0.2 deployment template ships with PostgreSQL 18. Keep the release value; changing database major versions needs migration planning. |
| `POSTGRES_PASSWORD` | **Required** | Generate a 64-character **hexadecimal** password with `openssl rand -hex 32`. The Compose stack reuses it for the internal database connection. Changing the env value alone will **not rotate** a password in an existing PostgreSQL volume. |
| `LANSURFER_BASE_URL` | **Required** | Public HTTPS origin, e.g. `https://lansurfer.example.org`; no subpath, query, or credentials. Used for OAuth callback URLs and browser security. Must match your public domain and reverse proxy. |
| `LANSURFER_HTTP_PORT` | Optional; default `8080` | TCP port bound to **host loopback** (`127.0.0.1`), never to the public interface. Change it if occupied and update the host-based Caddy upstream accordingly. Container port remains 8080; Docker Traefik uses the private container network. |
| `LANSURFER_INSTANCE_NAME` | Optional; defaults to `LANsurfer` | Your LAN party/community label, up to **64 Unicode characters**, one line, with no control characters. Appears alongside fixed LANsurfer branding and in the installed PWA name. |
| `LANSURFER_TRUSTED_PROXIES` | Optional; empty by default | Comma-separated proxy **IP addresses or CIDR networks**, e.g. `10.20.0.5,10.21.0.0/16`. Trust forwarded client IPs only from these proxies. Leave empty unless needed for accurate forwarded-IP handling; never use `0.0.0.0/0` or trust arbitrary internet clients. |

## OIDC / Keycloak player login

Configure OIDC by registering the exact callback
`https://lansurfer.example.org/auth/callback` using your own domain.

| Setting | When needed | Explanation and valid values |
| --- | --- | --- |
| `LANSURFER_OIDC_ISSUER` | Required **for OIDC login** | Provider issuer URL, e.g. `https://id.example.org/realms/community`, **not** the `.well-known` discovery URL. Pair with the client ID. |
| `LANSURFER_OIDC_CLIENT_ID` | Required **for OIDC login** | The client ID registered at that issuer. |
| `LANSURFER_OIDC_CLIENT_SECRET` | Conditional | Secret for a confidential client (normally used for production). May be empty for a correctly configured public **PKCE** client. |
| `LANSURFER_OIDC_DISPLAY_NAME` | Optional; default `OpenID Connect` | User-facing label for this login provider, e.g. `Community SSO`; does not change authorization. |
| `LANSURFER_OIDC_GAME_NICK_CLAIM` | Optional; empty by default | Name of an ID-token string claim, e.g. `nickname`, used **only to suggest** a Game Nick. If missing/invalid, LANsurfer tries `preferred_username` and `name`. Players still choose their own unique Game Nick. |

## Discord player login

Register `https://lansurfer.example.org/auth/discord/callback` as the
OAuth redirect URL with the `identify` scope. Discord player login and
Discord channel notifications are **separate** integrations.

| Setting | When needed | Explanation and valid values |
| --- | --- | --- |
| `LANSURFER_DISCORD_CLIENT_ID` | Required **for Discord login** | OAuth client/application ID for authenticating players. |
| `LANSURFER_DISCORD_CLIENT_SECRET` | Required **for Discord login** | Matching application secret. Set both login credentials together or leave both empty. |

## Discord channel notifications (optional)

Discord notification delivery authorizes a channel-selected webhook using
`https://lansurfer.example.org/admin/discord/callback` and the
`webhook.incoming` scope. Set the separate credentials and
`LANSURFER_DATA_ENCRYPTION_KEY`, apply the env file, then connect the
channel via **Administration → Discord delivery** (`/admin/discord`).
No pasted webhook URL or Discord bot is required.

| Setting | When needed | Explanation and valid values |
| --- | --- | --- |
| `LANSURFER_DISCORD_WEBHOOK_CLIENT_ID` | Required **for Discord delivery** | OAuth client ID for the webhook authorization flow, **not** the player-login ID. |
| `LANSURFER_DISCORD_WEBHOOK_CLIENT_SECRET` | Required **for Discord delivery** | Matching OAuth application secret. Set both webhook credentials together. The encrypted destination also needs the data-encryption key below. |

See [Discord setup](SELF_HOSTING.md#optional-discord-notifications-webhook)
for the step-by-step flow.

## Browser Web Push and encryption (optional)

Browser Web Push requires HTTPS, **both** matching VAPID keys, a contact
subject and a stable data-encryption key. To generate env-ready keys
**once**, run this inside your installed LANsurfer directory:

```sh
docker compose run --rm --no-deps lansurfer admin notifications generate-secrets
```

This command prints `KEY=value` pairs; it does not store them.
Copy the required lines to your private `.env`. For an encryption key
alone, use `openssl rand -base64 32`. **Reuse any existing encryption
key** when enabling another feature, and protect it with your backups.

| Setting | When needed | Explanation and valid values |
| --- | --- | --- |
| `LANSURFER_WEBPUSH_PUBLIC_KEY` | Required **for Web Push** | VAPID P-256 public key encoded as **Base64url**; generated alongside the matching private key. |
| `LANSURFER_WEBPUSH_PRIVATE_KEY` | Required **for Web Push** | Matching **secret** Base64url P-256 VAPID key. Protect it; existing browser subscriptions depend on this pair. |
| `LANSURFER_WEBPUSH_SUBJECT` | Required **for Web Push** | Contact identifier starting with `mailto:` (e.g. `mailto:admin@example.org`) or `https://`. |
| `LANSURFER_DATA_ENCRYPTION_KEY` | Required **for Web Push or Discord delivery** | A secret **32-byte AES key in standard Base64**, used to encrypt stored push credentials and Discord webhook secrets. Generate once, reuse across integrations, and preserve across backups/restores. Replacing it makes previously stored credentials unreadable. |

## Twitch login and IGDB (optional)

Both options share one Twitch Developer application, but can be enabled
**independently**. IGDB discovery can be used without Twitch login.

| Setting | When needed | Explanation and valid values |
| --- | --- | --- |
| `LANSURFER_TWITCH_CLIENT_ID` | Required if **either Twitch option** enabled | Twitch Developer application's client ID, shared by login and IGDB. |
| `LANSURFER_TWITCH_CLIENT_SECRET` | Required if **either Twitch option** enabled | Client secret for the same Twitch application. |
| `LANSURFER_TWITCH_AUTH_ENABLED` | Optional; default `false` | Set `true` to enable Twitch player login; register `https://lansurfer.example.org/auth/twitch/callback` as the redirect URI. |
| `LANSURFER_IGDB_ENABLED` | Optional; default `false` | Set `true` for server-side IGDB game discovery and cover fetching. Requires Twitch credentials, **not** Twitch player login. |

## Legal pages and game-request chat (optional)

| Setting | When needed | Explanation and valid values |
| --- | --- | --- |
| `LANSURFER_IMPRINT_ENABLED` | Optional; default `false` | Set `true` to expose `/imprint` and its footer link. Create and review the document in **Administration → Legal texts** (`/admin/legal`); the Terms and Privacy texts live there too. |
| `LANSURFER_CHAT_RATE_LIMIT` | Optional; default `5` | Maximum number of chat messages per player and Game Request during the configured rate window; must be a **positive integer**. |
| `LANSURFER_CHAT_RATE_WINDOW` | Optional; default `20s` | Rate-limiting duration, e.g. `20s` or `1m`; must be at least `1s`. Both chat settings can be overridden independently. |

## Traefik Docker reverse proxy (optional)

These entries are **commented out** in the release env example.
Uncomment them only after downloading `compose.traefik.yaml` from the
**same release** and setting up an existing Traefik Docker provider,
HTTPS entrypoint, certificate resolver and external Docker network.
For host-based Caddy, leave them commented out.

| Setting | When needed | Explanation and valid values |
| --- | --- | --- |
| `COMPOSE_FILE` | Required **with Traefik overlay** | `compose.yaml:compose.traefik.yaml` loads both files automatically. This is a **Docker Compose** setting, not a LANsurfer app variable. |
| `TRAEFIK_HOST` | Required **with Traefik overlay** | Public **hostname only**, e.g. `lansurfer.example.org`, without `https://`; should match the host in `LANSURFER_BASE_URL`. Used in the Traefik router rule. |
| `TRAEFIK_NETWORK` | Required **with Traefik overlay** | **Existing external Docker network** already joined by Traefik, e.g. `traefik_proxy`. Only LANsurfer joins it; PostgreSQL stays private. |
| `TRAEFIK_CERT_RESOLVER` | Required **with Traefik overlay** | Name of a working TLS certificate resolver already configured in Traefik, e.g. `letsencrypt`. The overlay does not create it. |
| `TRAEFIK_ENTRYPOINT` | Optional; default `websecure` | Name of the HTTPS entrypoint in your Traefik configuration. Set only if it differs from `websecure`. |

## Development-only variables

The repository-root [development env file](../.env.example) is **not**
the release deployment template. In local development,
`LANSURFER_LISTEN_ADDR` chooses the application's listening address
(the production Compose fixes it to `:8080`), and the optional
`LANSURFER_DATABASE_URL` overrides development PostgreSQL connection
management. You do not need to add either to the production `.env`.

The production Compose file itself sets internal database URL,
listener and cover-cache settings automatically.

Changes to the private `.env` normally require
`docker compose up -d --wait` to recreate affected services; an
ordinary `docker compose restart` may not load changed environment
variables. For advanced operations and backup guidance, see the
[technical deployment reference](../deploy/README.md).
