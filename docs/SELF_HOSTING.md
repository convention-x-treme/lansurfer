# Self-host LANsurfer

Host LANsurfer with Docker Compose and an existing HTTPS reverse proxy.
**No Git clone, source code, Go, Node.js or Python needed.**

> [!IMPORTANT]
> **Release status:** v0.1.0 is the latest published release; v0.2.0 is
> being prepared but **is not published yet**. The default image stays
> pinned to v0.1.0 until a verified new release exists.
> v0.1.0 has fixed Terms/Privacy text for the reference deployment,
> **not** the legal-text editor already on `main`. Do not invite players
> to a different public installation with those sample policies.
> Backup/restore and cross-version upgrades remain unverified
> ([#29](https://github.com/convention-x-treme/lansurfer/issues/29)).

## Quickstart

You need **Docker Engine and Docker Compose v2**, `wget`, `openssl`, a domain,
an HTTPS reverse proxy (Caddy or Traefik below), and a login provider
(Discord, OIDC or Twitch).

### 1. Download the deployment files

```sh
mkdir -p lansurfer && cd lansurfer

# Use main until a release containing these deployment files exists.
REF=main
BASE="https://raw.githubusercontent.com/convention-x-treme/lansurfer/$REF/deploy"
wget -O compose.yaml "$BASE/compose.production.yaml"
wget -O .env.example "$BASE/production.env.example"
cp .env.example .env
chmod 600 .env
```

The deployment files were added **after v0.1.0**, so `REF=v0.1.0` will
not work. **After v0.2.0 is published**, use `REF=v0.2.0` to obtain files
from that release. Downloading Compose files does not automatically
upgrade the app image in `.env`.

The repository is **currently private**, so anonymous `wget` cannot
download the files yet. Authorized operators can instead use GitHub CLI
without cloning:

<details>
<summary>Download while the repository is private</summary>

After `gh auth login`, in an empty `lansurfer` directory run:

```sh
gh api -H "Accept: application/vnd.github.raw+json" \
  repos/convention-x-treme/lansurfer/contents/deploy/compose.production.yaml > compose.yaml
gh api -H "Accept: application/vnd.github.raw+json" \
  repos/convention-x-treme/lansurfer/contents/deploy/production.env.example > .env.example
cp .env.example .env
chmod 600 .env
```

You may also need `docker login ghcr.io` for private image access.

</details>

### 2. Configure LANsurfer

Generate a database password:

```sh
openssl rand -hex 32
```

Edit `.env` and set `POSTGRES_PASSWORD` to the entire **64-character
hex string**. Set `LANSURFER_BASE_URL` to your public HTTPS origin
(e.g. `https://lansurfer.example.org`). Leave the preconfigured
`LANSURFER_IMAGE` and `POSTGRES_IMAGE` values unchanged initially.

Enable **at least one** login provider:

| Provider | Required `.env` settings | Register this HTTPS callback |
| --- | --- | --- |
| **Discord** | `LANSURFER_DISCORD_CLIENT_ID`, `LANSURFER_DISCORD_CLIENT_SECRET`, plus `LANSURFER_LEGAL_ENTITY_NAME` and `LANSURFER_LEGAL_CONTACT_EMAIL` | `/auth/discord/callback` |
| **OIDC / Keycloak** | `LANSURFER_OIDC_ISSUER`, `LANSURFER_OIDC_CLIENT_ID`; a secret if required by the provider | `/auth/callback` |
| **Twitch** | `LANSURFER_TWITCH_CLIENT_ID`, `LANSURFER_TWITCH_CLIENT_SECRET`, `LANSURFER_TWITCH_AUTH_ENABLED=true` | `/auth/twitch/callback` |

Append each callback path to **your own HTTPS origin**. Discord login uses
the `identify` scope; create credentials in the
[Discord Developer Portal](https://discord.com/developers/applications).
[Provider details](../README.md#configure-oidc-login) are documented separately.
IGDB, Web Push and Discord notification delivery are optional.

Keep `.env` private, with file permissions restricted. Do not put
real keys or expanded Compose output in bug reports.

### 3. Choose HTTPS: Caddy **or** Traefik

**Option A — Caddy installed directly on the Docker host**

Point your domain at the server. Configure Caddy to proxy to the locally
bound app port (8080 by default):

```caddyfile
lansurfer.example.org {
    reverse_proxy 127.0.0.1:8080
}
```

Caddy needs access to ports 80/443 for its standard HTTPS setup.
**Do not publish the LANsurfer port 8080 or PostgreSQL port 5432.**

**Option B — Existing Traefik container with Docker provider**

Traefik must already be running with its Docker provider, an HTTPS
entrypoint (typically `websecure`), a certificate resolver and a
**shared external Docker network**.

Download an optional third file:

```sh
wget -O compose.traefik.yaml "$BASE/compose.traefik.yaml"
```

While private, use the authenticated `gh api` pattern from step 1 with
the `deploy/compose.traefik.yaml` path.

Add these lines to `.env`:

```dotenv
COMPOSE_FILE=compose.yaml:compose.traefik.yaml
TRAEFIK_HOST=lansurfer.example.org
TRAEFIK_NETWORK=traefik_proxy
TRAEFIK_CERT_RESOLVER=letsencrypt
```

Replace the values with **your actual domain, existing Traefik Docker
network and configured TLS resolver**. Traefik must be connected to
that network; the overlay does not install or configure Traefik.
Set `TRAEFIK_ENTRYPOINT` too if yours is not named `websecure`.

Compose automatically loads both files via `COMPOSE_FILE`. Traefik
reaches LANsurfer through the shared network; **PostgreSQL remains only
on the separate internal app network**. The base app's host port is
still bound to 127.0.0.1.

See the [Traefik Docker provider reference](https://doc.traefik.io/traefik/reference/routing-configuration/other-providers/docker/)
for its requirements and certificate configuration.

### 4. Start the containers

From the directory containing `compose.yaml` and `.env`:

```sh
docker compose config --quiet
docker compose pull
docker compose up -d --wait
docker compose ps
```

`config --quiet` checks Compose syntax, but is **not** a complete
security validation. The [technical reference](../deploy/README.md)
explains the optional stronger preflight.

On the Docker host, check `curl -fsS http://127.0.0.1:8080/healthz`
(expected `ok`) and `curl -fsS http://127.0.0.1:8080/readyz`
(expected `ready`). Then open your public HTTPS domain in a browser.

### 5. Set up your first administrator

Sign in once and choose a **Game Nick**. List your local user ID
and promote it:

```sh
docker compose exec lansurfer /lansurfer admin users
docker compose exec lansurfer /lansurfer admin promote --user-id YOUR_USER_UUID
```

Reload LANsurfer. For an **actually published image containing current
`main` features**, open **Administration → Legal texts**
(`/admin/legal`), replace the sample Terms and Privacy text, and
review the public pages. If you need an Imprint page, save one and set
`LANSURFER_IMPRINT_ENABLED=true` in `.env`, then recreate with
`docker compose up -d --wait`.

**The v0.1.0 image does not have this editor.** Current source code
alone does not make it available in an older published image.
Review legal content before allowing public users.

## Daily use

| Action | Command |
| --- | --- |
| Status | `docker compose ps` |
| Application logs | `docker compose logs --tail=100 lansurfer` |
| Follow logs | `docker compose logs -f lansurfer` |
| Restart | `docker compose restart lansurfer` |
| Stop (preserve data) | `docker compose down` |

Run commands in your `lansurfer` directory. **Never use `down -v` or
`down --volumes` on an installation with data you need to keep.**
Keep an independent, protected backup of your `.env` and database.

## Updating later

After **v0.2.0 has actually been published**, read its release and migration
notes, verify the published GHCR image digest, and ensure you have a
**tested backup and restore procedure**. Update only
`LANSURFER_IMAGE` in your private `.env` to the exact release tag **and
digest**, then run `docker compose pull` and `docker compose up -d --wait`.

LANsurfer may apply forward-only database migrations. An image rollback
does not automatically undo a database migration. PostgreSQL major
upgrades need a separate migration plan. Do not use a moving `latest` tag.

For advanced operations, troubleshooting, notification encryption
keys, and proxy options, see the [technical deployment reference](../deploy/README.md).
