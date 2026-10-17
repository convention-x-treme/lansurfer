# Self-hosted production Compose

> **First time hosting LANsurfer?** Use the [two-file download quickstart](../docs/SELF_HOSTING.md) — no Git clone or Python needed. It includes Caddy and Traefik examples. This page is the technical reference for deeper validation and operations.

This is a **separate, release-based** Docker Compose deployment for a single LANsurfer instance. Unlike root `compose.yaml` (development), it does not build from a checkout, reads **both immutable image references from `deploy/production.env`**, keeps PostgreSQL internal, and binds the application only to Docker-host loopback. The example env file starts with the previously verified `v0.1.0` image; the Compose template is not tied to that release. PostgreSQL data and the media cache use named volumes. No proxy, identity provider or monitoring service is added to the Compose stack.

This is a deployment example, **not** evidence that backup/restore or upgrades have been validated; those remaining regression tests belong to [#29](https://github.com/convention-x-treme/lansurfer/issues/29).

> **Important for operators:** This reference tracks the current `main`
> Compose deployment, but the example still pins **the published v0.1.0
> image**. v0.2.0 is **not yet released**; do not invent or use its tag/digest.
> v0.1.0's fixed Terms/Privacy paragraphs describe the reference deployment
> and have no `/admin/legal` editor. Current `main` supports database-backed
> editable Terms, Privacy and Imprint; **you cannot use that UI until an
> image containing the feature is published and selected**. Do not use v0.1.0's
> sample policies for an unrelated public operator. See the
> [self-hosting guide](../docs/SELF_HOSTING.md#legal-texts-and-imprint).

## Prepare configuration

The commands below use the **full repository layout** and the optional Python security preflight. Ordinary operators need **no Git clone or Python**: the [self-hosting quickstart](../docs/SELF_HOSTING.md) downloads just `compose.yaml` and `.env.example` into one directory (plus `compose.traefik.yaml` when required). For this more technical repository-based procedure, run from the **repository root** with Docker Engine, Compose v2, Python 3 and OpenSSL:

```sh
cp deploy/production.env.example deploy/production.env
chmod 600 deploy/production.env
openssl rand -hex 32
```

Put the newly generated **64-character** hex string in `POSTGRES_PASSWORD` in `deploy/production.env`; do not reuse the development password. Hex is important because Compose constructs `LANSURFER_DATABASE_URL` from this password and does not URL-encode it. Set `LANSURFER_BASE_URL` to the real public **HTTPS origin**, for example `https://lansurfer.example.org` (without a path). Configure at least one real login provider (OIDC, Discord or Twitch); the development Keycloak defaults are intentionally absent. Configure the login provider and any desired Web Push, Discord notification or IGDB settings. For the v0.1.0 image, the `LANSURFER_LEGAL_*` variables populate only its limited, fixed-page operator identity fields. Current `main` instead has administrator-edited legal documents; `LANSURFER_IMPRINT_ENABLED` controls whether the saved Imprint is served (`false` by default). The Compose file passes both the old `LANSURFER_IMPRINT_URL` and new `LANSURFER_IMPRINT_ENABLED` values so the example remains compatible with the currently published image while being ready for a future release. See the root [README](../README.md) for each provider's required fields and callbacks.

`deploy/production.env` must remain outside version control and be readable only by trusted operators. Docker administrators can inspect container environment values and `docker compose config` reveals interpolated secrets; avoid pasting its output into issues or logs. Treat the generated password and notification encryption keys as backup-sensitive secrets.

**Image version management:** `deploy/compose.production.yaml` contains no LANsurfer or PostgreSQL version. `deploy/production.env` selects `LANSURFER_IMAGE` and `POSTGRES_IMAGE`; the example pins the verified `v0.1.0` LANsurfer and PostgreSQL `18.6-alpine` with immutable multi-architecture digests. An operator can update either reference *without editing Compose*. `docker compose pull` alone never selects a newer digest, and deployments do not upgrade automatically. To upgrade LANsurfer, verify the desired published release's multi-architecture digest, update only `LANSURFER_IMAGE` in your private env file, review the release notes and data/backup compatibility, then run the documented preflight, pull and up commands. For a rollback, restore the prior exact image reference only when its database-schema compatibility is confirmed; rollback is not guaranteed. PostgreSQL image updates are independent: PostgreSQL 18 uses `/var/lib/postgresql` as the persistent mount, and a major-version change requires a dedicated database migration/restore plan.

The release image may require authentication while the GHCR package is private. An operator with package read access can run `docker login ghcr.io` before starting the stack. The private `production.env` preserves the operator's version choice even if the example template is updated for a future release. The pinned digest deliberately requires an explicit change to `LANSURFER_IMAGE` when an operator adopts a new release. When v0.2.0 is published, verify its exact image digest and release notes before updating the private file; the example deliberately stays on v0.1.0 until a real release is available.

## Start and inspect

```sh
# Keep this pipe; it validates the effective config WITHOUT displaying secrets.
docker compose --env-file deploy/production.env -f deploy/compose.production.yaml config --format json | python3 scripts/test-production-compose.py
docker compose --env-file deploy/production.env -f deploy/compose.production.yaml pull
docker compose --env-file deploy/production.env -f deploy/compose.production.yaml up -d --wait
docker compose --env-file deploy/production.env -f deploy/compose.production.yaml ps
curl --fail --silent http://127.0.0.1:8080/healthz
curl --fail --silent http://127.0.0.1:8080/readyz
```

The checks should return `ok` and `ready`, respectively. The preflight rejects an incomplete or insecure configuration (including a non-HTTPS origin, invalid database password, unexpectedly public ports, missing login providers or unsafe proxy trust) before deployment; it uses the same checks as CI. Shell-exported variables override values in `--env-file`, so check your environment if the results are unexpected. If `pull` fails with GHCR permission denied while the repository is private, authenticate using a read-authorized account instead of replacing the digest with `latest`. If you change `LANSURFER_HTTP_PORT`, use that host port in the `curl` commands and reverse-proxy upstream. Only Docker-host loopback is published; direct external clients cannot reach the app or PostgreSQL via this Compose file.

A Caddy **installed on the Docker host** can terminate HTTPS, obtain certificates for a correctly configured public DNS name, and proxy to the loopback upstream:

```caddyfile
lansurfer.example.org {
    reverse_proxy 127.0.0.1:8080
}
```

For **Traefik running in Docker**, use the [optional Traefik Compose overlay](compose.traefik.yaml) and [Traefik quickstart](../docs/SELF_HOSTING.md#3-choose-https-caddy-or-traefik). It attaches LANsurfer to a pre-existing external proxy network without exposing PostgreSQL. For other reverse proxies, forward normal HTTP requests and streaming/SSE responses without buffering the event stream. Do not expose port 8080 on the public network. If the proxy is in *another container*, `127.0.0.1` refers to that proxy container and this example must be adapted to a shared private network instead. Set `LANSURFER_TRUSTED_PROXIES` only to known proxy IPs or CIDRs when accurate forwarded client IPs are needed; never trust all addresses. Cookies are marked `Secure` according to the public HTTPS `LANSURFER_BASE_URL`, not forwarded headers. HTTPS must be trusted by client browsers for PWA installation and Web Push.

## Admin bootstrap and optional notification keys

First sign in through a configured provider to create your local user, then run:

```sh
docker compose --env-file deploy/production.env -f deploy/compose.production.yaml exec lansurfer /lansurfer admin users
docker compose --env-file deploy/production.env -f deploy/compose.production.yaml exec lansurfer /lansurfer admin promote --user-id YOUR_USER_UUID
```

If Web Push or Discord notification delivery is needed, create the keys **once**, before enabling those services:

```sh
docker compose --env-file deploy/production.env -f deploy/compose.production.yaml run --rm --no-deps lansurfer admin notifications generate-secrets
```

Transfer those keys securely to `deploy/production.env` and store a separate secure backup of that file. Configure `LANSURFER_WEBPUSH_SUBJECT` if enabling Web Push. Losing or replacing `LANSURFER_DATA_ENCRYPTION_KEY` prevents decryption of already stored notification credentials. Keep database backups and their matching operator keys together in a protected recovery plan.

## Shutdown and data safety

```sh
docker compose --env-file deploy/production.env -f deploy/compose.production.yaml down
```

The Compose database readiness probe runs an **authenticated SQL query over TCP** instead of checking the temporary Unix socket exposed during PostgreSQL initialization. It also rejects a stale/incorrect PostgreSQL password after an operator edits the env file. The application restarts after explicit Compose-driven database restarts. Unexpected crashes are handled by the container restart policies; startup migrations and readiness are still verified via `/readyz`. LANsurfer's application shutdown can spend up to ten seconds each draining HTTP, notifications and IGDB work, so the Compose stop grace period is 45 seconds rather than 15.

A normal `down` preserves both named volumes. **Do not use `down --volumes` or `down -v` on a populated deployment**: it deletes PostgreSQL data. Backups/restores require their own verified procedure before this is used for important real-world data. Changing `POSTGRES_PASSWORD` in the env file after PostgreSQL has initialized does **not** automatically rotate its existing database user's password; rotate the database password in PostgreSQL and the env configuration together with a controlled restart.

The isolated [Production Compose runtime smoke workflow](../.github/workflows/production-compose-smoke.yml) runs the **published digest-pinned image** in a uniquely named disposable project, tests authenticated database readiness and an explicit dependency restart, then recreates the stack and checks a synthetic record survives. It does not touch an operator's deployment or replace the separately planned full backup/restore tests in #29.

This example does not include automatic historical-version upgrade or rollback testing. Updating image references in `deploy/production.env` or the database schema needs explicit release-specific assessment and a verified backup, never an unreviewed moving `latest` tag. Future releases may publish their own tested env example without forcing edits to existing operators' Compose files.
