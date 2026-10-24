# Demo instance — implementation plan

Spec: `docs/superpowers/specs/2026-10-24-demo-instance-design.md`
Branch: `feat/demo-mode` · PR title: `feat(demo): add demo mode with daily database reset`

## Tasks

1. **Config** — `internal/platform/config`: `DemoMode bool`, `DemoResetTime`, `DemoResetLocation *time.Location`; strict bool parse; validate only when on. Tests: defaults, invalid values rejected only in demo mode.
2. **Next-reset helper** — small func next to the demo middleware: `nextReset(now, hh, mm, loc) time.Time`. Test DST transition days for Europe/Berlin.
3. **Middleware + banner** — `internal/web/middleware/demo.go` carrying next-reset instant and demo flag (pattern: `instance.go`). Layout renders banner with `<time data-local-time>` and, for signed-in `user` role with Game Nick, a CSRF form `POST /demo/admin` "Get admin rights". `task generate`, regenerate CSS if new classes.
4. **Self-promotion** — handler `DemoAdmin` calls `user.Service.PromoteToAdmin(ctx, sessionUserID)`; route registered only when demo mode, inside authenticated + `RequireGameNick`. Notice key `demo-admin-granted`.
5. **Blocked actions** — `handler.DemoForbidden(target)` middleware; wrap in `router.go` only when demo mode: games delete/merge(POST)/alias remove/igdb unlink, `/admin/igdb/sync`, discord connect/reconnect/disconnect, `/admin/metrics/key`. Notice key `demo-forbidden` with text "This action is disabled in the demo." Router tests for on/off.
6. **Reset store** — `internal/platform/postgres/demo_reset.go`: transaction listing `pg_tables WHERE schemaname='public'` minus keep-list, quoted identifiers, single `TRUNCATE ... RESTART IDENTITY CASCADE`. Integration test: every non-kept table empty, kept tables untouched, test fails if a future kept table is referenced by FK from a truncated one (CASCADE would empty it).
7. **App + CLI** — `app.ResetDemo(ctx, cfg, legalDir, logger)`: demo-mode guard, read/validate legal files first, connect, migrate under advisory lock, truncate, `legal.Store.Save` each kind. `cmd/lansurfer/main.go`: `admin demo reset --legal-dir DIR`, help text, main_test for argument errors.
8. **Deploy** — `deploy/demo/compose.demo.yaml`, `lansurfer-demo-reset.service` (stop → run reset → start; start also on failure), `lansurfer-demo-reset.timer` (04:00 Europe/Berlin, Persistent), `legal/*.md.example`, `README.md`.
9. **Docs harvest** — PRODUCT "Demo mode", ARCHITECTURE §15/§20/§21, CONFIGURATION, TERMINOLOGY, README link; delete plan/spec.
10. **Verify** — `task test`, `task test:integration`, `task lint`; smoke per spec with `task dev` + demo env; run reset against dev DB (scratch database, never the developer's `.env`-configured data without consent).

## Order / dependencies

1 → 2 → 3 → 4; 5 independent after 1; 6 → 7 → 8. Single branch, single PR.

## Manual / operator (record in issue)

- Demo host: DNS `demo.lansurfer.de`, reverse proxy + TLS, OAuth apps with demo callbacks, Twitch credentials for IGDB, legal files, install timer (`systemctl enable --now lansurfer-demo-reset.timer`), first manual reset run.
