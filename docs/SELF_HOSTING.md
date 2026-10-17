# Self-host LANsurfer with Docker Compose

Run your own LANsurfer instance for a LAN party or gaming community. This guide
is written for **server operators**: you do not need Go, Node.js, or a build
toolchain. Docker downloads the published LANsurfer image and runs it alongside
PostgreSQL.

**New here?** Follow [Setup](#setup) from top to bottom. Already running an
instance? Jump to [Daily operations](#daily-operations) or
[Updating LANsurfer](#updating-lansurfer).

> [!IMPORTANT]
> **Current release vs. upcoming v0.2.0:** This guide documents the
> **current `main` code**, which is being prepared for a later release.
> **v0.2.0 has not been published.** The production environment template
> still installs the last published image, **v0.1.0**, by default.
> In particular, current `main` supports editing Terms, Privacy, and
> Imprint in **Administration → Legal texts** (`/admin/legal`), while
> **v0.1.0 does not**. An updated checkout does not upgrade its Docker image.
> Only use the new features after a release containing them has been
> published and you have deliberately selected its verified image digest.
>
> v0.1.0's fixed Terms/Privacy text describes the reference deployment; it
> cannot be edited in the running image. Do **not** publicly host it for a
> different operator relying on that wording. Regardless of version,
> **backup/restore and cross-version upgrade/rollback procedures are not yet
> fully validated**. Test your own recovery before using valuable data;
> see [issue #29](https://github.com/convention-x-treme/lansurfer/issues/29).

## How this deployment works

```text
Players ── HTTPS ──> Your reverse proxy (on the Docker host)
                         |
                         +── http://127.0.0.1:8080 ──> LANsurfer container
                                                          |
                                                          +── PostgreSQL container
                                                              (private Docker network)
```

The [production Compose file](../deploy/compose.production.yaml) starts only
LANsurfer and PostgreSQL. The database has no public port. The app is published
on **host loopback only**, so you provide HTTPS separately. Image versions are
selected by a private env file rather than hardcoded into Compose.

This setup assumes a **reverse proxy running directly on the Docker host**.
If yours runs in another container or on another machine, do not copy the
loopback configuration unchanged; adapt the private networking first.

## Before you start

| You need | Why |
| --- | --- |
| Linux server with Docker Engine and Docker Compose v2 | Runs the published images and PostgreSQL |
| Git, Python 3, and OpenSSL on the server | Fetches the deployment files, validates configuration, and generates a password |
| A domain pointing at the server and an HTTPS reverse proxy | LANsurfer uses a public HTTPS origin for sign-in and secure browser features |
| At least one login provider | Choose Discord, OIDC (for example, Keycloak), or Twitch |
| Registry access | The images must be downloadable; while the LANsurfer repository/package is private you need appropriate GitHub package-read access |

You will need to manage your server, DNS, firewall, provider credentials, and
backups. The commands below are intended for a Linux shell. Replace
`lansurfer.example.org` with **your own domain**, everywhere it occurs.

## Setup

### 1. Get the deployment files

```sh
git clone https://github.com/convention-x-treme/lansurfer.git
cd lansurfer
```

The repository is currently private, so cloning requires authorized GitHub
access. You only need its Compose configuration and validation script: **the
production deployment does not compile the application from source**. The
checkout may already include features planned for v0.2.0; which **application
version actually runs** is determined solely by `LANSURFER_IMAGE` in
`deploy/production.env`.

Keep running all commands in this guide from the repository root.

### 2. Prepare the public HTTPS address

Configure DNS so your chosen domain points to your server. Allow inbound
**80/tcp and 443/tcp** for your reverse proxy; do **not** expose the LANsurfer
port (8080) or PostgreSQL (5432) publicly.

For example, with **Caddy installed directly on the Docker host**, add the
following to its Caddyfile:

```caddyfile
lansurfer.example.org {
    reverse_proxy 127.0.0.1:8080
}
```

Apply/reload the configuration using your host's Caddy setup. With correct DNS
and publicly reachable ports, Caddy can arrange HTTPS certificates
automatically. See the [Caddy HTTPS guide](https://caddyserver.com/docs/quick-starts/https)
for installation and certificate prerequisites.

For Traefik, Nginx, or a containerized reverse proxy, see the
[technical deployment reference](../deploy/README.md) and adapt the upstream
network. In particular, `127.0.0.1` **inside a proxy container is not the
Docker host**. Make sure live SSE connections are not buffered or prematurely
closed by the proxy.

### 3. Create your private configuration

```sh
cp deploy/production.env.example deploy/production.env
chmod 600 deploy/production.env
openssl rand -hex 32
```

Copy the generated **64-character hexadecimal string** into
`POSTGRES_PASSWORD` in `deploy/production.env`. Open that file in your
preferred text editor and set at least:

| Setting | What to enter |
| --- | --- |
| `POSTGRES_PASSWORD` | The generated 64-character hex password; do not reuse development credentials |
| `LANSURFER_BASE_URL` | The complete HTTPS origin, e.g. `https://lansurfer.example.org`, **without a path** |
| `LANSURFER_IMAGE`, `POSTGRES_IMAGE` | The example pins the **published v0.1.0** image and PostgreSQL by digest. Do not use an unpublished v0.2.0 tag; once it is released, choose its verified digest deliberately. |
| Login provider settings | At least one provider, described in step 4 |
| `LANSURFER_HTTP_PORT` | Leave at `8080` unless that port is already used on the Docker host |
| `LANSURFER_IMPRINT_ENABLED` | Optional for the current `main` app (intended for an upcoming release): `true` exposes the operator-written `/imprint` document after you have saved it; `false` hides the link. The v0.1.0 image does not support this flag. |

The file is ignored by Git. Never commit it or share its contents, since it may
contain credentials and encryption keys. Environment variables exported in
your shell can override the file's values.

### 4. Set up sign-in (choose at least one)

<details>
<summary>Discord login</summary>

Create an application in the [Discord Developer Portal](https://discord.com/developers/applications).
Register this exact OAuth2 redirect URL, using your public domain:

```text
https://lansurfer.example.org/auth/discord/callback
```

LANsurfer requests the `identify` scope. In `deploy/production.env`, set:

```dotenv
LANSURFER_DISCORD_CLIENT_ID=YOUR_APPLICATION_ID
LANSURFER_DISCORD_CLIENT_SECRET=YOUR_APPLICATION_SECRET
LANSURFER_LEGAL_ENTITY_NAME=YOUR_ORGANIZATION_NAME
LANSURFER_LEGAL_CONTACT_EMAIL=contact@example.org
```

Replace all example values with your real operator details. The production
Compose preflight still requires the legal entity name and contact email for
Discord login (also required by v0.1.0). On current `main`, the
`LANSURFER_LEGAL_*` variables do **not** edit the published Terms/Privacy
content; the operator must save their own texts in the administrator UI after
a supporting release is selected. Discord expects public Terms and Privacy
links: `https://lansurfer.example.org/terms` and
`https://lansurfer.example.org/privacy`. Do not invite users while those
pages contain placeholder or reference-deployment text.

</details>

<details>
<summary>OIDC / Keycloak login</summary>

Configure your provider to allow
`https://lansurfer.example.org/auth/callback` as an exact redirect URI.
Set its **issuer URL**, not the discovery endpoint:

```dotenv
LANSURFER_OIDC_ISSUER=https://your-identity-provider.example/realms/your-realm
LANSURFER_OIDC_CLIENT_ID=YOUR_CLIENT_ID
LANSURFER_OIDC_CLIENT_SECRET=YOUR_CLIENT_SECRET
```

A confidential client needs its secret. A correctly configured public PKCE
client may leave the secret empty. Do not use LANsurfer's local-development
Keycloak realm for a public installation.

</details>

<details>
<summary>Twitch login (optional alternative)</summary>

Register `https://lansurfer.example.org/auth/twitch/callback` in your Twitch
Developer Application and set:

```dotenv
LANSURFER_TWITCH_CLIENT_ID=YOUR_CLIENT_ID
LANSURFER_TWITCH_CLIENT_SECRET=YOUR_CLIENT_SECRET
LANSURFER_TWITCH_AUTH_ENABLED=true
```

IGDB game discovery is a **separate, optional** setting
(`LANSURFER_IGDB_ENABLED=true`) that uses the same Twitch credentials; it
is not necessary for Twitch login. See the [main README](../README.md#configure-twitch-login-and-igdb-discovery)
for third-party terms and limitations.

</details>

You can enable more than one provider later. Web Push, Discord **notification
delivery**, and IGDB are optional; none is required just to start LANsurfer.

### 5. Check configuration and start

In the repository root, define this shortcut in your current shell:

```sh
dc() {
  docker compose --env-file deploy/production.env -f deploy/compose.production.yaml "$@"
}
```

Run the security preflight. It checks the effective Compose configuration
without printing secrets to the screen:

```sh
dc config --format json | python3 scripts/test-production-compose.py
```

If the preflight fails, fix the named setting in `deploy/production.env`
before proceeding. Do **not** work around failures by switching to `latest`
tags or opening database ports.

If GitHub Container Registry asks for authentication, sign in with an account
that has permission to read the LANsurfer package:

```sh
docker login ghcr.io
```

Then start the two containers:

```sh
dc pull
dc up -d --wait
dc ps
```

Check locally on the Docker host:

```sh
curl -fsS http://127.0.0.1:8080/healthz
curl -fsS http://127.0.0.1:8080/readyz
```

The expected responses are `ok` and `ready`. If you changed
`LANSURFER_HTTP_PORT`, replace 8080 in the `curl` commands.

Now visit **`https://lansurfer.example.org`** in a browser to test the
reverse proxy and sign-in flow.

### 6. Create the first administrator

1. Sign in through your configured provider and choose your **Game Nick**.
2. List local accounts and find your own user ID:
   ```sh
   dc exec lansurfer /lansurfer admin users
   ```
3. Promote that exact ID:
   ```sh
   dc exec lansurfer /lansurfer admin promote --user-id YOUR_USER_UUID
   ```
4. Reload LANsurfer and follow [Legal texts and imprint](#legal-texts-and-imprint)
   before inviting players. The exact steps depend on your **installed image
   version**, not the Git checkout.
5. If IGDB is disabled (the default), create your community's Games manually
   in **Administration → Games** (`/admin/games`).

After the deployment and legal content are ready for public users, your
players can sign in, follow Games, create or join Game Requests, and use
LANsurfer from desktop or mobile browsers.

### Legal texts and imprint

The administration interface available in **current `main`** (intended for
an upcoming release) lets the operator maintain public legal documents **in
the application's database**, not through environment variables.

Once you are running a published release that includes this feature:

1. Open **Administration → Legal texts** (`/admin/legal`).
2. Replace every placeholder in the sample **Terms of Service** and **Privacy
   Policy** with real details about your organization and infrastructure.
   The public URLs are `/terms` and `/privacy`.
3. If you need an Imprint page, prepare and save its text in the same
   administration area. Then set `LANSURFER_IMPRINT_ENABLED=true` in
   `deploy/production.env` and run `dc up -d --wait`. The public
   `/imprint` page and footer link then become available. If disabled, the
   page is not served. **This setting is available on current `main`, not
   v0.1.0.**
4. Review the rendered pages in a private browser window, and have the legal
   content checked for your jurisdiction **before** inviting players.

The **published v0.1.0 image has no `/admin/legal` editor**. Its fixed
Terms/Privacy paragraphs describe the LANsurfer reference deployment.
`LANSURFER_LEGAL_ENTITY_NAME`, `LANSURFER_LEGAL_ADDRESS`, and
`LANSURFER_LEGAL_CONTACT_EMAIL` only fill the old release's operator
identity fields; `LANSURFER_IMPRINT_URL` points to an external imprint.
These environment variables do **not** make the built-in sample policies
accurate for a different operator.

**Until a new release with editable legal texts exists, do not treat the
default v0.1.0 production image as ready for an unrelated public deployment.**

## Daily operations

Keep using the `dc` shortcut in the same shell. In a fresh shell, define it
again with the function from step 5.

| Task | Command |
| --- | --- |
| See container state | `dc ps` |
| Read recent application logs | `dc logs --tail=100 lansurfer` |
| Follow logs live | `dc logs -f lansurfer` |
| Start previously configured services | `dc up -d --wait` |
| Restart just LANsurfer | `dc restart lansurfer` |
| Stop the stack while keeping data | `dc down` |

**Data safety:** The PostgreSQL named volume stores the important product
data. A normal `dc down` leaves named volumes intact. **Never run Compose
with `--volumes` / `-v` on an installation you need to preserve.** The cover
cache is separate and can be rebuilt.

Keep a secure, separate copy of `deploy/production.env`, especially if you
enable Web Push or Discord notification delivery. For those optional features,
generate keys once using the
[documented CLI](../deploy/README.md#admin-bootstrap-and-optional-notification-keys);
losing the data-encryption key can make existing notification credentials
unreadable.

## Updating LANsurfer

LANsurfer **does not update automatically**. The application and PostgreSQL
versions live in your private `deploy/production.env`:

- `LANSURFER_IMAGE` controls the exact LANsurfer release **and digest**.
- `POSTGRES_IMAGE` separately controls PostgreSQL. Do not casually change its
  major version: a database migration plan is needed.

When **v0.2.0 is actually published**, read its release notes and verify the
published **multi-architecture image digest**. This guide does not include a
made-up v0.2.0 digest, and the configuration example intentionally continues
to pin v0.1.0 until a verified release is available. Once you have a **tested
backup and recovery plan**, change `LANSURFER_IMAGE` in your *private*
`deploy/production.env` to that exact published tag-and-digest reference,
run the preflight, then use `dc pull` and `dc up -d --wait`. If updating
from v0.1.0, verify the migration/compatibility notes before doing so.

**Important:** Application startup applies forward-only database migrations.
Switching the image back to an older release does *not* automatically restore
the older database schema. Cross-version upgrades, rollbacks, and automated
backup/restore regression tests are not yet complete; see
[issue #29](https://github.com/convention-x-treme/lansurfer/issues/29).

## Troubleshooting

| Symptom | What to check |
| --- | --- |
| `docker compose` or the preflight fails | Run from the repository root; check required values in `deploy/production.env` and confirm Docker Compose v2 + Python 3 are installed |
| Image pull denied | The package may still be private; authenticate with `docker login ghcr.io` using a permitted account |
| `dc up` fails / app stays unready | Inspect `dc ps` and `dc logs --tail=100 lansurfer`; check the database password and container health |
| Site shows 502 / cannot connect | Check both local health URLs, DNS, your HTTPS proxy, and the proxy-to-`127.0.0.1:8080` route |
| Login redirects fail | Register the **exact HTTPS callback URL** for the selected provider and match `LANSURFER_BASE_URL` |
| No **Administration → Legal texts** menu | Check the `LANSURFER_IMAGE` tag and digest in `production.env`: the published v0.1.0 image has no legal editor. Cloning newer source does not upgrade a container. |
| Imprint link missing on a supporting release | Save the Imprint in `/admin/legal`, set `LANSURFER_IMPRINT_ENABLED=true` in `deploy/production.env`, and recreate the app with `dc up -d --wait`. |
| HTTPS proxy is itself containerized | Host loopback is the wrong upstream; use an explicitly configured **private Docker network** instead |
| Need to recover data | Do not delete or reinitialize the PostgreSQL volume; follow your tested database recovery procedure |

Avoid pasting `docker compose config` output or complete application environment
values into public support issues: expanded Compose output may reveal secrets.
When asking for help, share only **redacted** error messages.

## Further reading

- [Technical production Compose reference](../deploy/README.md) — more detail on the proxy, Docker security, secrets, and shutdown
- [Production environment template](../deploy/production.env.example) — all supported deployment options in this Compose example
- [LANsurfer main README](../README.md) — provider specifics, application features, and optional integrations
- [Docker Compose environment variables](https://docs.docker.com/compose/how-tos/environment-variables/variable-interpolation) — how `--env-file` works
- [Caddy HTTPS setup](https://caddyserver.com/docs/quick-starts/https) — certificates, ports, and DNS

For LANsurfer development instead of self-hosting, use `task dev` as documented
in the [development section of the README](../README.md#-run-lansurfer-locally).
