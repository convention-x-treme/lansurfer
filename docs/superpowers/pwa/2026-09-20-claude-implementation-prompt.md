# Claude Coding Prompt — Implement LANsurfer Track D

You are implementing LANsurfer Roadmap Track D — Minimum Instance Identity & PWA in the private repository:

convention-x-treme/lansurfer

Work on branch:

feat/track-d-instance-pwa

Branch base:

bf7e1b36b87773239c32e5ce158ebabb5583c7d2

## Read first

Before changing code, read in this order:

1. AGENTS.md
2. docs/superpowers/pwa/2026-09-20-track-d-instance-pwa-design.md
3. docs/superpowers/pwa/2026-09-20-track-d-instance-pwa-plan.md
4. issue #27
5. Track D in docs/ROADMAP.md
6. Track D -> E contract in docs/ROADMAP.md and issue #28
7. Pilot Gate and 1.0 Integration Gate in docs/ROADMAP.md
8. relevant sections of docs/PRODUCT.md
9. relevant sections of docs/ARCHITECTURE.md
10. docs/BRANDING.md
11. current UI kit / Track G work and issue #30

Treat the approved design as authoritative. Do not reopen settled choices during implementation.

If code reality requires changing the approved service-worker lifecycle, authentication/session model, deployment model, security model, Track D -> E boundary, Track D -> G boundary, or stable-1.0 scope, STOP and report the conflict instead of silently redesigning it.

## Core implementation contract

Implement exactly one optional configuration value:

LANSURFER_INSTANCE_NAME

Rules:

- trim surrounding whitespace;
- empty => unset;
- fallback => LANsurfer;
- maximum 64 Unicode code points;
- reject controls/newlines;
- configuration only;
- no DB state;
- no admin editor;
- no second identity field;
- do not derive from hostname.

Keep instance identity structurally separate from branding.Brand.

### Browser identity

Configured instance name:

- appears as a secondary label in public/login and authenticated shell;
- title becomes <page> · <instance>;
- manifest name = instance;
- manifest short_name = instance.

Unset:

- no extra label;
- title fallback remains <page> · LANsurfer;
- manifest name/short_name = LANsurfer.

Do not alter fixed LANsurfer tagline, wordmark, colors, typography, mascot, or operator attribution.

## Manifest

Add public /manifest.webmanifest with:

- name = resolved instance name;
- short_name = resolved instance name;
- id = "/";
- start_url = "/";
- scope = "/";
- display = "standalone";
- theme_color = "#0D1B2A";
- background_color = "#0D1B2A";
- required icon declarations only.

Omit orientation and speculative extras.

Response headers:

- Content-Type: application/manifest+json; charset=utf-8
- Cache-Control: no-store

Link it from every full HTML layout.

## Icons

Technical contract:

- source preferably vector or at least 1024x1024;
- 192x192 PNG purpose any;
- 512x512 PNG purpose any;
- dedicated 512x512 maskable PNG purpose maskable;
- 180x180 Apple touch icon;
- favicon renditions from the same artwork.

Maskable artwork must have an opaque background and keep essential artwork inside the central circular safe zone whose diameter is 80% of the canvas.

Do not add an image-processing dependency.

The user approved a wave artwork as a temporary PWA icon that may be merged to main to unblock Track D. It is not canonical LANsurfer branding and does not satisfy final Track-G/Pilot acceptance.

If the binary artwork is not present in your checkout, do not invent a different production icon. Leave a clear blocker/instruction for supplying the approved temporary renditions.

Never use the otter as favicon/signet.

## Service worker

Add a dedicated TypeScript worker entry, expected at:

web/src/service-worker.ts

Bundle it with the existing esbuild toolchain.

Serve compiled bytes at:

/service-worker.js

Headers:

- Content-Type: text/javascript; charset=utf-8
- Cache-Control: no-store

Root URL gives root scope. Do not add Service-Worker-Allowed.

Track D worker behavior is ONLY:

- install -> skipWaiting()
- activate -> clients.claim()

There must be NO:

- fetch handler;
- CacheStorage;
- IndexedDB;
- message handler;
- sync/periodicsync;
- offline shell;
- offline fallback;
- user/session state.

## Registration

There must be exactly one registration path.

Register after window.load:

navigator.serviceWorker.register("/service-worker.js", {
  scope: "/",
  updateViaCache: "none"
})

Use one focused helper/promise consumed by normal startup, admin diagnostics, and later Track E.

Do not:

- register separately in Track E preparation code;
- add polling;
- add update banners;
- force reload on controller change;
- add a generic PWA framework.

Unsupported browser or registration failure must not break LANsurfer or show player-facing errors.

## Auth/logout safety

Keep the existing auth/session model unchanged.

Do not unregister the service worker on logout.

The worker must remain user-neutral and cache nothing.

Add narrow bfcache protection: when an authenticated page is restored from back/forward cache, force server revalidation/reload.

Do not add cross-tab logout broadcasting or session polling.

## Track D -> E boundary

Track E later may add only the concrete worker behavior it owns, especially:

- push;
- notificationclick.

Track D must not implement:

- notification permission UX;
- PushManager subscriptions;
- VAPID;
- audience rules;
- payload/business logic;
- delivery;
- notification rendering behavior.

Track E must not need a second worker or registration path.

The resolved Track-D instance name is the only instance context Track E should consume.

## CSP

Add explicit:

- worker-src 'self'
- manifest-src 'self'

Preserve the rest of the current restrictive CSP. Do not add unsafe exceptions or new remote origins.

## Secure-context warning

Do not require HTTPS for server startup.

- HTTPS => no warning.
- localhost/loopback HTTP => no warning.
- other public HTTP base URL => one startup warning that normal LANsurfer works but PWA/Web Push require a secure context.

Do not change health/readiness semantics.

## Admin diagnostics

Add admin-only:

/admin/pwa

Read-only only.

Server facts:

- resolved instance name;
- Base URL;
- manifest URL;
- worker URL;
- expected secure-context status.

Browser facts via progressive JS:

- diagnostics ran;
- window.isSecureContext;
- SW support;
- registration state/scope;
- controller state;
- current registration error.

No persistence and no player-facing warning UI.

## Tests

Use TDD where practical.

Add Go tests for:

- config;
- fallback;
- validation;
- manifest body/headers;
- worker route/headers;
- CSP;
- public access;
- title/shell rendering;
- admin server facts;
- public-HTTP warning;
- no-JS behavior.

Add a dedicated:

task test:pwa

Do not repurpose task test:browser into general E2E coverage.

The focused PWA browser suite must cover:

- registration;
- root scope;
- activation;
- immediate update replacement;
- controller behavior;
- registration-failure degradation;
- zero Track-D CacheStorage;
- no fetch/offline interception;
- logout/back-navigation safety.

Follow the existing internal/web/browsertest pattern: any test control plane stays outside production routes.

## Validation artifact

Create:

docs/validation/track-d-pwa.md

as a real-device evidence template.

Do not fabricate Desktop/Android/iPhone success. Record only evidence actually obtained.

## Build and verification

Do not hand-edit generated files.

Run at the end:

task generate
task lint
go test -race ./...
task test:integration
task test:browser
task test:pwa
task build

If the environment cannot perform a check, state exactly which check was not run and why.

## Scope guardrails

Reject implementation drift into:

- offline-first;
- application caching;
- SPA behavior;
- custom install UX;
- branding/theme configuration;
- uploaded icons;
- white-labeling;
- Web Push business logic;
- generic worker/plugin infrastructure;
- new frontend frameworks.

## Delivery style

Work through the implementation plan task-by-task.

For each task:

1. inspect existing code;
2. add/adjust tests first where practical;
3. implement minimally;
4. run targeted tests;
5. keep commits coherent.

At completion, summarize:

- files changed;
- behavior implemented;
- tests run and results;
- real-device validation status;
- temporary-icon status;
- any blockers;
- any place where implementation reality forced a deviation.

Prepare the branch for one PR linked to issue #27, but do not mark Track D complete until final Track-G icon approval and real-device validation evidence exist.
