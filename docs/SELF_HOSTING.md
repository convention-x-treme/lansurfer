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

These are **release download** URLs: they are available only after the
selected version has actually been published on GitHub.

Both files come from the **same release**. The downloaded `production.env.example`
already references `LANSURFER_IMAGE` with the selected release's exact tag
and immutable multi-platform digest. You **do not need to find or enter
a Docker image version yourself**. Different released versions can be
installed by changing `VERSION` in the download command.

For optional integrity verification, download
`deployment-checksums.txt` from the same `$BASE` URL and run
`sha256sum --ignore-missing -c deployment-checksums.txt` before editing `.env`.
The flag skips the optional Traefik overlay if you did not download it.

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

For explanations of **every** `.env` option, default and optional integration,
see the [complete production configuration reference](CONFIGURATION.md).

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
to that shared network; PostgreSQL remains private. This example uses a
fixed Docker project name and fixed Traefik router/service names, so it
supports **one LANsurfer instance per Docker host/Traefik setup**. Running
multiple instances requires distinct project and router names.

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

Check the installed binary and readiness from inside the app container:

```sh
docker compose exec lansurfer /lansurfer version
docker compose exec lansurfer /lansurfer healthcheck
docker compose exec lansurfer /lansurfer help
```

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

### Optional: Discord notifications (webhook)

To publish Game Request updates to a Discord channel, set up **Discord
delivery** separately from player login. A Discord login configuration
using `/auth/discord/callback` does **not** enable webhook delivery.

1. In the [Discord Developer Portal](https://discord.com/developers/applications),
   configure a Discord OAuth2 application for webhook delivery. Register
   **exactly** this redirect URI, using your own HTTPS domain:

   ```text
   https://lansurfer.example.org/admin/discord/callback
   ```

   This flow only requests Discord's `webhook.incoming` scope, not
   `identify` or bot permissions. You can use separate Discord
   applications for login and delivery to keep their credentials distinct.

2. Add the webhook application's credentials to your private `.env`:

   ```dotenv
   LANSURFER_DISCORD_WEBHOOK_CLIENT_ID=YOUR_WEBHOOK_CLIENT_ID
   LANSURFER_DISCORD_WEBHOOK_CLIENT_SECRET=YOUR_WEBHOOK_CLIENT_SECRET
   LANSURFER_DATA_ENCRYPTION_KEY=YOUR_BASE64_ENCRYPTION_KEY
   ```

   Generate the encryption key **once** with `openssl rand -base64 32`,
   then paste the complete output into `LANSURFER_DATA_ENCRYPTION_KEY`.
   If this variable already has a value (for example for Web Push),
   **keep that existing key** instead of generating a replacement.
   Back it up securely alongside your database: losing or changing it
   makes stored webhook credentials unreadable.

3. Apply the updated configuration with `docker compose up -d --wait`.
   While signed in as a LANsurfer administrator, open
   **Administration → Discord delivery** (`/admin/discord`), select
   **Connect**, authorize Discord, and choose the server/channel
   for notifications. LANsurfer creates and stores the webhook for you;
   you do **not** need to paste a webhook URL manually.

The webhook flow is optional and independent of the login provider.
If it needs reauthorization later, use **Reconnect** in the same
administration page.

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

## PostgreSQL backup and restore

LANsurfer persists users, Games, Game Requests, chat, notification state and
operator-managed legal texts in PostgreSQL. The Docker volume survives
`docker compose down` **without** `-v`, but a volume is not a backup.
Back up the database before upgrades and test restores regularly. These
examples assume the version-matched release Compose directory and its
PostgreSQL service named `postgres`.

### Create and check a backup

Run from the directory containing your `compose.yaml` and private `.env`:

```bash
(
set -euo pipefail
umask 077
BACKUP_DIR="$HOME/lansurfer-backups"
mkdir -p "$BACKUP_DIR"
BACKUP="$(mktemp "$BACKUP_DIR/lansurfer-XXXXXXXX.dump")"
if ! docker compose exec -T postgres pg_dump -U lansurfer -d lansurfer \
  --format=custom --no-owner --no-acl > "$BACKUP" ||
  ! test -s "$BACKUP" ||
  ! docker compose exec -T postgres pg_restore --list < "$BACKUP" > /dev/null; then
  rm -f -- "$BACKUP"
  echo "Backup creation or validation failed" >&2
  exit 1
fi
(cd "$BACKUP_DIR" && sha256sum "${BACKUP##*/}" > "${BACKUP##*/}.sha256")
printf 'Backup file: %s\n' "$BACKUP"
)
```
Copy the printed backup path into `BACKUP` before running the checksum or restore examples.

Before restore, verify the checksum from the directory containing both files:

```bash
cd "$BACKUP_DIR"
sha256sum --check "${BACKUP##*/}.sha256"
```

`-T` is important: it keeps the compressed custom-format dump binary-safe.
`pg_dump` takes a transactionally consistent snapshot while the app is
running. Preserve the SHA-256 record, and transfer the dump to encrypted
**off-host** storage with access controls and a tested retention policy.
The dump contains personal data and possibly encrypted delivery credentials:
do not commit it to Git or attach it to GitHub issues/PRs.

Back up the private `.env` **separately and securely** together with the
exact LANsurfer application version/image digest, deployment Compose files
and PostgreSQL major version. In particular, preserve the original
`LANSURFER_DATA_ENCRYPTION_KEY`, `LANSURFER_WEBPUSH_PRIVATE_KEY`,
`LANSURFER_WEBPUSH_PUBLIC_KEY` and relevant OAuth/Discord credentials.
These are **not** in the database dump. Never generate replacement keys
for a restore: stored encrypted subscriptions/webhook credentials would
become unusable and existing Web Push subscriptions may stop working.
The cover cache can be recreated and is not part of this database dump.

### Test a restore without touching the running database

Run this Bash block from the directory containing the version-matched
`compose.yaml` and private `.env`. It creates a separate, temporary Compose
project. The private `.env` is read but never changed or printed.

```bash
(
set -euo pipefail
BACKUP_DIR="$(cd "$(dirname "$BACKUP")" && pwd)"
BACKUP="$BACKUP_DIR/$(basename "$BACKUP")"
(cd "$BACKUP_DIR" && sha256sum --check "${BACKUP##*/}.sha256")
export LANSURFER_POSTGRES_PORT=0 LANSURFER_HTTP_PORT=0
COMPOSE_FILE="$(realpath compose.yaml)"
ENV_FILE="$(realpath .env)"
verify_dir="$(mktemp -d "${TMPDIR:-/tmp}/lansurfer-restore-XXXXXXXX")"
base="$(basename "$verify_dir" | tr '[:upper:]' '[:lower:]')"
RESTORE_PROJECT="lansurfer-restore-verify-$base"
restore_compose=(docker compose --project-name "$RESTORE_PROJECT" \
  --env-file "$ENV_FILE" -f "$COMPOSE_FILE")
project_label="com.docker.compose.project=$RESTORE_PROJECT"
if [[ -n "$(docker ps -aq --filter "label=$project_label")" ]] ||
  [[ -n "$(docker volume ls -q --filter "label=$project_label")" ]]; then
  rm -rf -- "$verify_dir"
  echo "Restore verification project name already exists" >&2
  exit 1
fi
project_owned=true
cleanup() {
  status=$?
  trap - EXIT INT TERM
  if [[ "$project_owned" == true ]] &&
    ! "${restore_compose[@]}" down --volumes --remove-orphans >/dev/null; then
    echo "Failed to remove isolated restore verification project" >&2
    (( status != 0 )) || status=1
  fi
  rm -rf -- "$verify_dir"
  exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
"${restore_compose[@]}" up -d --wait postgres
tables="$("${restore_compose[@]}" exec -T postgres psql -X -U lansurfer \
  -d lansurfer -At -v ON_ERROR_STOP=1 \
  -c "SELECT count(*) FROM pg_tables WHERE schemaname='public'")"
[[ "$tables" == 0 ]] || { echo "Restore target database is not empty" >&2; exit 1; }
"${restore_compose[@]}" exec -T postgres pg_restore -U lansurfer -d lansurfer \
  --exit-on-error --no-owner --no-acl < "$BACKUP"
"${restore_compose[@]}" exec -T postgres psql -X -U lansurfer -d lansurfer \
  -v ON_ERROR_STOP=1 -c 'SELECT count(*) FROM game_requests'
"${restore_compose[@]}" exec -T postgres psql -X -U lansurfer -d lansurfer \
  -v ON_ERROR_STOP=1 -c 'SELECT count(*) FROM notification_delivery'
"${restore_compose[@]}" exec -T postgres psql -X -U lansurfer -d lansurfer \
  -v ON_ERROR_STOP=1 -c 'SELECT count(*) FROM game_requests req JOIN notification_work work ON work.game_request_id=req.id JOIN notification_delivery delivery ON delivery.work_id=work.id'
)
```

Only this self-created test project may be removed with
`"${restore_compose[@]}" down --volumes --remove-orphans`. Its Bash cleanup
subshell removes only resources with this unique project name on success,
failure, or signal. The operator project and original database volume stay
untouched. Compare representative data and confirm that the restored schema
matches the exact application release. A readable dump alone does not prove
recovery works.

### Recover an actual installation

1. Stop the application so it cannot write or run migrations. Retain the
   existing database volume untouched as a fallback; **never** use
   `docker compose down -v` or restore over a populated live database.
2. Provision a **new, empty PostgreSQL database/volume**, preferably on a
   separate recovery host, with a compatible PostgreSQL major version.
   Restore the exact operator `.env` (especially encryption and VAPID keys)
   and choose the matching LANsurfer application release first.
3. Start **only** the new PostgreSQL service with `docker compose up -d
   postgres`. Confirm the database `lansurfer` exists and is empty.
4. Restore the dump into that empty database, **before** starting LANsurfer:

   ```sh
   docker compose exec -T postgres pg_restore -U lansurfer -d lansurfer \
     --exit-on-error --no-owner --no-acl < "$BACKUP"
   ```

5. Verify rows, migration version and restored settings. Then start LANsurfer
   using `docker compose up -d --wait lansurfer`; check `/readyz`, admin
   login and representative data/notification settings. Reconfigure DNS/TLS
   only after the isolated recovery installation has been validated.

LANsurfer database migrations run **forward only** on application startup.
An older application binary cannot safely reverse a newer schema. Do not
attempt an automated downgrade or a PostgreSQL major-version upgrade as
part of a restore; first validate compatibility and follow a separate
database upgrade plan. If recovery fails, keep the original volume and
backup intact and investigate before retrying.

## Updates

LANsurfer does **not** update automatically. Before updating:

1. Make a **tested database backup** and a secure copy of your private
   `.env`. Read the new release notes, especially database compatibility.
2. Download that release's `compose.yaml`, optional Traefik overlay and
   `production.env.example`. Compare the new template to your existing
   `.env`:

   ```sh
   diff -u .env production.env.example
   ```

   Review this output **locally only**; it may display secrets.
3. Keep your credentials, add any new settings, and update
   `LANSURFER_IMAGE` to the new **tag-and-digest** reference.
   Review `POSTGRES_IMAGE` changes separately: **never blindly change
   a PostgreSQL major version** without a database migration plan.
4. Run `docker compose config --quiet`, `docker compose pull` and
   `docker compose up -d --wait`.

An older application image cannot automatically reverse database migrations.
See the [technical deployment reference](../deploy/README.md) for more.

### Moving from an earlier checkout-based installation

If you used `docker compose --env-file deploy/production.env -f
deploy/compose.production.yaml` from a repository checkout, **do not
create a new database**. Back up the existing database and private env.
Stop the old stack using the same Compose flags with `down` **without
`-v`**. Download the version-matched files into a new directory and
copy your existing password/provider settings instead of generating new
ones. The fixed project name `lansurfer-production` means Docker's
**named volumes** remain available on the same daemon, provided you
did not manually change the project name or volume configuration.
Verify volume mappings and compatibility before starting the new stack;
never remove the existing database volume to make an upgrade work.
