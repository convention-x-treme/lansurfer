# Self-host LANsurfer

Deploy LANsurfer using the **version-matched Docker Compose files from the
v0.2.0 release**. No Git clone, source code, Go, Node.js, or Python required.

You need Docker Engine with Docker Compose v2, `wget`, `openssl`, an HTTPS
domain, and at least one login provider (Discord, OIDC, or Twitch).

## 1. Download LANsurfer

Choose the LANsurfer release version **once**. The release automatically
includes a matching Docker image reference and its verified digest.

```sh
VERSION=v0.2.0
BASE="https://github.com/convention-x-treme/lansurfer/releases/download/$VERSION"

mkdir -p lansurfer && cd lansurfer
wget -O compose.yaml "$BASE/compose.yaml"
wget -O production.env.example "$BASE/production.env.example"
cp production.env.example .env
chmod 600 .env
```

Both files come from the **same release**. The downloaded `production.env.example`
already references `LANSURFER_IMAGE` with the selected release's exact tag
and immutable multi-platform digest. You **do not need to find or enter
a Docker image version yourself**. Different released versions can be
installed by changing `VERSION` in the download command.

For optional integrity verification, download
`deployment-checksums.txt` from the same `$BASE` URL and run
`sha256sum -c deployment-checksums.txt` before editing `.env`.

## 2. Configure your instance

Generate a random database password:

```sh
openssl rand -hex 32
```

Edit `.env` and set `POSTGRES_PASSWORD` to the entire 64-character result,
and `LANSURFER_BASE_URL` to your public HTTPS origin, for example
`https://lansurfer.example.org` (without a path). Keep the
`LANSURFER_IMAGE` and `POSTGRES_IMAGE` values from the release as-is.

Choose at least one login provider:

| Provider | Configure in `.env` | OAuth callback (append to your HTTPS domain) |
| --- | --- | --- |
| Discord | `LANSURFER_DISCORD_CLIENT_ID`, `LANSURFER_DISCORD_CLIENT_SECRET` | `/auth/discord/callback` |
| OIDC / Keycloak | `LANSURFER_OIDC_ISSUER`, `LANSURFER_OIDC_CLIENT_ID`, optionally `LANSURFER_OIDC_CLIENT_SECRET` | `/auth/callback` |
| Twitch | `LANSURFER_TWITCH_CLIENT_ID`, `LANSURFER_TWITCH_CLIENT_SECRET`, `LANSURFER_TWITCH_AUTH_ENABLED=true` | `/auth/twitch/callback` |

For Discord, register an application at
[Discord Developer Portal](https://discord.com/developers/applications),
request the `identify` scope, and add the callback shown above with
your real domain. Legal documents can be configured after your first
admin login. For additional identity-provider options, see the
[main README](../README.md#configure-oidc-login).

Do not commit or share `.env`: it contains private credentials.

## 3. Choose HTTPS

LANsurfer expects a reverse proxy and only binds its host port to
`127.0.0.1:8080`. PostgreSQL does **not** publish a host port.

### Option A: Caddy on the Docker host

Configure Caddy with your own domain:

```caddyfile
lansurfer.example.org {
    reverse_proxy 127.0.0.1:8080
}
```

Point DNS to the host and make sure Caddy can receive HTTPS traffic
(typically ports 80 and 443).

### Option B: An existing Docker Traefik

Traefik must already have its Docker provider, a TLS entrypoint
(usually `websecure`), a certificate resolver, and a shared external
Docker network.

Download the matching optional third file:

```sh
wget -O compose.traefik.yaml "$BASE/compose.traefik.yaml"
```

Append the following to `.env`, using your own values:

```dotenv
COMPOSE_FILE=compose.yaml:compose.traefik.yaml
TRAEFIK_HOST=lansurfer.example.org
TRAEFIK_NETWORK=traefik_proxy
TRAEFIK_CERT_RESOLVER=letsencrypt
```

The network must exist and Traefik must already be connected to it.
If your HTTPS entrypoint is named differently, set
`TRAEFIK_ENTRYPOINT` too. The overlay attaches **only LANsurfer**
to that shared network; PostgreSQL remains private.

## 4. Start

In your `lansurfer` directory:

```sh
docker compose config --quiet
docker compose pull
docker compose up -d --wait
```

Open your HTTPS URL. You can check that LANsurfer is up locally with
`curl -fsS http://127.0.0.1:8080/healthz` (expected `ok`) and
`curl -fsS http://127.0.0.1:8080/readyz` (expected `ready`).

## 5. Set up the first administrator

Sign in with your configured provider, choose a Game Nick, and find
your local user ID:

```sh
docker compose exec lansurfer /lansurfer admin users
docker compose exec lansurfer /lansurfer admin promote --user-id YOUR_USER_UUID
```

Reload LANsurfer. Under **Administration → Legal texts**
(`/admin/legal`), replace the sample **Terms of Service** and
**Privacy Policy** with text for your organization and installation.
If you need an Imprint, save it there too, set
`LANSURFER_IMPRINT_ENABLED=true` in `.env`, and run
`docker compose up -d --wait` again. Review the public pages before
inviting participants.

## Everyday commands

| Task | Command |
| --- | --- |
| Status | `docker compose ps` |
| Logs | `docker compose logs --tail=100 lansurfer` |
| Follow logs | `docker compose logs -f lansurfer` |
| Restart the application | `docker compose restart lansurfer` |
| Stop (keep data) | `docker compose down` |

**Never use `docker compose down -v` on an installation whose database
you want to keep.** Store your `.env` securely and verify your
database backup/restore procedure before relying on important data.

## Updates

LANsurfer does **not** update automatically. For a later released
version, review its release notes and database compatibility,
make a tested backup, then download the matching release's files
using its `VERSION`. Preserve your existing private `.env` settings
and carefully update only `LANSURFER_IMAGE` to the image reference
shipped in that new release's env example.

A previous Docker image cannot automatically reverse database
migrations. PostgreSQL major upgrades require a separate migration
plan. See the [technical deployment reference](../deploy/README.md)
for additional operational and security details.
