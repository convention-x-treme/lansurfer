# Production Docker Compose reference

**Start here:** [Self-hosting quickstart](../docs/SELF_HOSTING.md). It downloads
the correct files **directly from a chosen GitHub Release** and offers examples
for both host-based Caddy and Docker-based Traefik. No Git clone or Python
is required for a normal installation.

## Versioned deployment files

The GitHub Release includes these assets:

| Asset | Purpose |
| --- | --- |
| `compose.yaml` | LANsurfer and private PostgreSQL, named data volumes, restart and safety settings |
| `production.env.example` | Environment template with the **exact tag and verified GHCR multi-platform image digest** |
| `compose.traefik.yaml` | Optional overlay for an existing Docker Traefik with an external network |
| `deployment-checksums.txt` | SHA-256 hashes for the three deployment files |

The release publisher generates the environment asset **after the image is
built and verified** and attaches everything to the same unpublished draft
before making that release public. The source file
`deploy/production.env.example` contains an image marker and is *not* itself
a runnable deployment. Use the release downloads, not raw files from `main`.

The operator's private `.env` selects the application and PostgreSQL images.
Updating source code, cloning the repo, or running `docker compose pull` with
an unchanged digest does **not** update LANsurfer.

## Networking and HTTPS

The base Compose file publishes only `127.0.0.1:8080` and keeps PostgreSQL
on the private Compose network. A host-based Caddy can proxy directly:

```caddyfile
lansurfer.example.org {
    reverse_proxy 127.0.0.1:8080
}
```

For an existing Docker-based Traefik, use
[`compose.traefik.yaml`](compose.traefik.yaml) with `TRAEFIK_NETWORK`,
`TRAEFIK_HOST` and `TRAEFIK_CERT_RESOLVER`. This adds only LANsurfer to
the proxy network, not PostgreSQL. The configured TLS entrypoint must exist
(default: `websecure`). A containerized reverse proxy cannot reach the
host's loopback at `127.0.0.1`; it needs this shared private network.

For other reverse proxies, forward SSE streams without buffering or
prematurely terminating connections. Use HTTPS for browser sessions and
Web Push. Only trust known proxy CIDRs when configuring
`LANSURFER_TRUSTED_PROXIES`.

## Configuration and data safety

The example needs a 64-digit hexadecimal `POSTGRES_PASSWORD`, an HTTPS
`LANSURFER_BASE_URL` and at least one login provider. Generate the
password with `openssl rand -hex 32`. LANsurfer uses an internally
constructed PostgreSQL URL; only hex passwords are safe in this example.

The application runs read-only with dropped Linux capabilities, private
temporary space, and a named cover-cache volume. PostgreSQL data is stored
in a named volume. The DB healthcheck checks authenticated SQL over TCP,
and Compose waits for the DB to be healthy before starting LANsurfer.
Stopping the stack normally preserves data; **`down --volumes` destroys
the database volume**. Changing the env password alone does not rotate
the initialized database user's password.

Discord webhook notification delivery is **separate from Discord login**.
Register `<your HTTPS origin>/admin/discord/callback` in the Discord
Developer Portal, and configure `LANSURFER_DISCORD_WEBHOOK_CLIENT_ID`
and `LANSURFER_DISCORD_WEBHOOK_CLIENT_SECRET` in `.env`.
Only the `webhook.incoming` scope is used. Webhook delivery additionally
requires `LANSURFER_DATA_ENCRYPTION_KEY` (standard Base64 encoding
of 32 random bytes). For a key **only**, run `openssl rand -base64 32`.
You may also generate notification secrets, including Web Push keys,
with the LANsurfer CLI:

```sh
docker compose run --rm --no-deps lansurfer admin notifications generate-secrets
```

Generate and store the data-encryption key **only once**; reuse it if already
configured for Web Push. Losing or changing the key makes stored Discord
webhook credentials unreadable. After updating `.env`, run
`docker compose up -d --wait`, then visit **Administration → Discord
delivery** (`/admin/discord`) and use **Connect** to authorize your
server/channel. There is no manual webhook URL to paste. The separate
Discord login redirect is `<your HTTPS origin>/auth/discord/callback`
and does not enable notifications.

Store secrets securely with your database backup.
For operator setup, see the
[quickstart](../docs/SELF_HOSTING.md#5-set-up-the-first-administrator)
and its [Discord notification walkthrough](../docs/SELF_HOSTING.md#optional-discord-notifications-webhook).

The public Terms, Privacy and optional Imprint are edited through
**Administration → Legal texts** (`/admin/legal`). The imprint is
controlled by `LANSURFER_IMPRINT_ENABLED`.

## Advanced validation

Docker Compose itself validates structure using
`docker compose config --quiet`. In the source repository,
`scripts/test-production-compose.py` provides an additional fail-closed
configuration check and negative mutation tests. Operators do not need
to download or install Python to follow the simple Quickstart.

The project CI uses an isolated Docker runner to boot the published
baseline image, verify PostgreSQL readiness and persistence, and
validate the Traefik overlay without launching a proxy.
The separate [release snapshot pipeline](../.github/workflows/release-snapshot.yml)
tests release-asset generation with synthetic digests.

## Updates and backups

Upgrade by choosing the next **published tag** and downloading its
matching assets. Keep the existing private `.env` (do not overwrite
credentials); replace `LANSURFER_IMAGE` only after checking release
notes, migration compatibility and restoring ability. Database
migrations are forward-only and rolling back an image does not
automatically downgrade the schema. PostgreSQL major upgrades need
their own procedures.

A container restart or successful data-volume retention test is not
a complete database backup-and-restore verification; see
[issue #29](https://github.com/convention-x-treme/lansurfer/issues/29).
