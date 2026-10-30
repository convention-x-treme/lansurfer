# Demo instance (demo.lansurfer.de)

Runs a normal [production deployment](../README.md) with **demo mode** on and
a host systemd timer that empties the database every day. Demo mode shows a
banner with the next reset time on every page, lets every signed-in player
grant themselves administrator rights, and disables destructive or externally
visible administrator actions. See PRODUCT.md "Demo mode".

## Setup

1. Follow the [self-hosting quickstart](../../docs/SELF_HOSTING.md) in
   `/opt/lansurfer-demo`: release `compose.yaml`, `.env` from the matching
   `production.env.example`, HTTPS reverse proxy for `https://demo.lansurfer.de`.
   Pin `LANSURFER_IMAGE` manually like any self-hosted instance.
2. Copy `compose.demo.yaml` from the **same tag** next to `compose.yaml` and add
   `COMPOSE_FILE=compose.yaml:compose.demo.yaml` to `.env`, so every
   `docker compose` command uses the overlay.
3. In `.env`, set the Twitch app credentials (`LANSURFER_TWITCH_CLIENT_ID`,
   `LANSURFER_TWITCH_CLIENT_SECRET`); the overlay enables IGDB. Configure the
   login providers with demo callback URLs, e.g.
   `https://demo.lansurfer.de/auth/discord/callback`,
   `https://demo.lansurfer.de/auth/twitch/callback`,
   `https://demo.lansurfer.de/auth/callback` (OIDC).
4. Create `/opt/lansurfer-demo/legal/{terms,privacy,imprint}.md` from the
   `legal/*.md.example` files here. They must be readable by the container user
   (`chmod 644`). The privacy text must state retention of at most 24 hours,
   the login providers used, and that Game Nicks, Game Requests, and chat are
   public to other demo visitors. Each file: non-empty, at most 20,000
   characters.
5. Optional metrics: key rotation is disabled in demo mode, and the key
   survives resets. Generate it once with `LANSURFER_DEMO_MODE=false`
   (temporarily remove the overlay from `COMPOSE_FILE`), then re-enable.
6. Start: `docker compose up -d --wait`.
7. Install the timer:

   ```sh
   sudo cp lansurfer-demo-reset.service lansurfer-demo-reset.timer /etc/systemd/system/
   sudo systemctl daemon-reload
   sudo systemctl enable --now lansurfer-demo-reset.timer
   sudo systemctl start lansurfer-demo-reset.service   # first reset, applies legal texts
   ```

   The service assumes `/opt/lansurfer-demo`; edit `WorkingDirectory` otherwise.

## Reset

`lansurfer-demo-reset.service` stops the app, runs
`lansurfer admin demo reset --legal-dir /legal`, and starts the app again
(also when the reset fails). The command refuses unless
`LANSURFER_DEMO_MODE=true` and validates the legal files before touching data.
It applies pending migrations, truncates every table except the IGDB catalog
and sync state, legal texts, metrics key, and migration history, then writes
the legal files. Everyone is signed out; product counters and Prometheus
counters restart from zero.

## Schedule

The timer is the schedule truth (`OnCalendar=*-*-* 04:00:00 Europe/Berlin`).
The banner only displays `LANSURFER_DEMO_RESET_TIME` and
`LANSURFER_DEMO_RESET_TIMEZONE` from `compose.demo.yaml`. Change both
together.

## Logs

```sh
journalctl -u lansurfer-demo-reset.service
systemctl list-timers lansurfer-demo-reset.timer
```
