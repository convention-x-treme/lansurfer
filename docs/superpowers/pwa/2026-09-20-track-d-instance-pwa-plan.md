# Track D — Minimum Instance Identity & PWA Implementation Plan

> **Historical execution record:** Track D implementation merged via PR #42. This file is retained because that merged PR explicitly references the design/plan pair. It is not current status, not an implementation queue, and must not override DOMAIN_LANGUAGE, PRODUCT, ARCHITECTURE, ROADMAP, or AGENTS.

For the coding agent: execute this plan task-by-task. The approved design is docs/superpowers/pwa/2026-09-20-track-d-instance-pwa-design.md. Do not reopen settled design choices. Prefer failing test -> minimal implementation -> passing test. Keep commits coherent and reviewable.

Goal: implement issue #27 without expanding stable-1.0 scope.

Branch: feat/track-d-instance-pwa

Base: bf7e1b36b87773239c32e5ce158ebabb5583c7d2

## Preflight

- Read AGENTS.md, issue #27, the approved design, relevant Track D sections in ROADMAP/PRODUCT/ARCHITECTURE/BRANDING.
- Read issue #28 only for the D -> E contract and issue #30 only for the D -> G boundary.
- Confirm branch is based on current main; if main advanced, rebase before implementation unless doing so changes approved contracts.
- Run baseline checks appropriate to the environment.
- Do not add dependencies unless concrete need is demonstrated and approved.

## Task 1 — instance identity configuration

Primary files:

- internal/platform/config/config.go
- config tests
- .env.example
- compose.yaml

Add LANSURFER_INSTANCE_NAME.

Behavior:

- optional;
- outer trim;
- empty => unset;
- maximum 64 Unicode code points;
- reject controls and line breaks;
- resolved fallback = LANsurfer.

Keep this separate from branding.Brand.

Add an immutable resolved instance-identity value to router/web dependencies rather than creating persistence or a new domain module.

Tests first.

Suggested commit: feat(config): add minimum instance identity

## Task 2 — shell/title identity rendering

Primary files:

- shared layout/public/auth templates;
- layout/template tests;
- router dependency wiring.

Implement:

- fixed LANsurfer brand stays unchanged;
- configured instance name appears as a secondary label in public/login and authenticated shell;
- title is <page> · <instance> when configured;
- fallback remains <page> · LANsurfer;
- fixed tagline remains the description.

Do not invent custom styling outside Track G patterns. Reuse current UI-kit/component semantics.

Suggested commit: feat(web): render instance identity separately from brand

## Task 3 — manifest endpoint

Add public GET/HEAD /manifest.webmanifest.

Emit:

- name = resolved instance name;
- short_name = resolved instance name;
- id = "/";
- start_url = "/";
- scope = "/";
- display = "standalone";
- theme_color = "#0D1B2A";
- background_color = "#0D1B2A";
- approved icon list.

Headers:

- Content-Type: application/manifest+json; charset=utf-8
- Cache-Control: no-store

Link the manifest from all full HTML layouts.

Add Apple touch-icon markup separately in the HTML head.

Do not add speculative manifest members.

Suggested commit: feat(pwa): add stable instance manifest

## Task 4 — application icon assets

Prepare/copy bundled assets through the existing frontend asset pipeline.

Required renditions:

- 192x192 PNG, purpose any;
- 512x512 PNG, purpose any;
- 512x512 dedicated maskable PNG, purpose maskable;
- 180x180 Apple touch icon;
- favicon renditions from the same source artwork.

Source guidance:

- preferably vector or at least 1024x1024;
- opaque background for maskable;
- essential artwork inside central circular safe zone with 80% canvas diameter.

Do not add an image-processing build dependency.

The user-approved wave artwork may temporarily occupy these slots in main, clearly documented as temporary. It is not final Track-G approval and must not close issue #27/Pilot acceptance.

Do not substitute the otter.

Suggested commit: feat(pwa): bundle temporary install icon renditions

## Task 5 — service-worker build entry and root route

Primary files:

- web/src/service-worker.ts;
- frontend build configuration/scripts;
- embedded asset serving;
- router/handler tests.

Build one bundled classic worker.

Serve it at /service-worker.js.

Headers:

- Content-Type: text/javascript; charset=utf-8
- Cache-Control: no-store

Do not add Service-Worker-Allowed.

Worker Track-D code contains only:

- install -> skipWaiting();
- activate -> clients.claim().

No fetch, caches, IndexedDB, message, sync, periodic sync, or offline fallback.

Tests should make absence of network/cache ownership explicit.

Suggested commit: feat(pwa): add minimal root service worker

## Task 6 — focused registration helper

Primary files:

- web/src/app.ts;
- a small focused helper module if it improves testability;
- TS/browser tests.

Requirements:

- exactly one code path calls navigator.serviceWorker.register;
- register after window.load;
- URL /service-worker.js;
- scope "/";
- updateViaCache "none";
- run in supported dev/prod environments;
- unsupported browsers do nothing;
- registration failure is diagnostic only;
- no user-facing player toast;
- no update polling;
- no forced reload on controller change.

Expose a small promise/result contract sufficient for the admin diagnostic page and future Track E subscription code.

Do not create a generic PWA framework.

Suggested commit: feat(pwa): own service-worker registration lifecycle

## Task 7 — bfcache auth revalidation

Add the narrow browser-side protection approved by the design.

On authenticated pages restored from back/forward cache, force server revalidation/reload.

Do not add BroadcastChannel logout orchestration, cross-tab polling, session-state persistence, or service-worker auth state.

Add focused tests for logout -> back and related session transitions.

Suggested commit: fix(auth): revalidate authenticated bfcache restores

## Task 8 — CSP and public routing

Update security middleware/tests.

Add:

- worker-src 'self'
- manifest-src 'self'

Preserve all existing directives and remote-image exceptions.

Ensure manifest, worker, and PWA icons are public, have no auth gate, and do not issue CSRF cookie/state for GET/HEAD.

Suggested commit: security(pwa): constrain manifest and worker sources

## Task 9 — HTTPS / secure-context warning

Use existing LANSURFER_BASE_URL.

Behavior:

- HTTPS => no warning;
- localhost/loopback HTTP => no warning;
- other HTTP public base URL => one clear startup warning explaining ordinary LANsurfer remains usable but service workers/installability/Web Push require a secure context.

Do not make this a startup error.

Do not change /healthz or /readyz.

Add tests for localhost, loopback, HTTPS, and public HTTP.

Suggested commit: feat(pwa): warn on insecure public origin

## Task 10 — admin PWA diagnostics

Add admin-only /admin/pwa.

Integrate it into the existing admin navigation/action structure; do not create a new dashboard.

Server-render:

- resolved instance name;
- configured Base URL;
- manifest URL;
- service-worker URL;
- secure-context expectation.

Progressive JS fills:

- diagnostics-ran marker;
- window.isSecureContext;
- SW API support;
- registration state;
- registration scope;
- controller state;
- current registration error.

No persistence.

If JS is unavailable, page states that browser diagnostics could not run.

Ordinary users get no PWA error UI.

Suggested commit: feat(admin): add read-only PWA diagnostics

## Task 11 — dedicated PWA browser-test target

Keep existing task test:browser semantics intact.

Add task test:pwa.

Use narrowly scoped Playwright/browser infrastructure for behavior that Go cannot prove.

Required cases:

- registration;
- scope;
- activation;
- immediate worker replacement/update;
- controller behavior;
- registration-failure degradation;
- zero Track-D CacheStorage;
- no fetch/offline interception;
- network-backed navigation;
- logout/back-navigation safety.

If a test-control plane is needed to swap worker revisions or force registration failure, keep it outside production routes, following the existing browsertest pattern.

Update AGENTS.md testing guidance if necessary so this is an explicit PWA-specific browser exception rather than an accidental expansion of the current interaction-hardening suite.

Suggested commit: test(pwa): add focused service-worker browser suite

## Task 12 — validation artifact

Create docs/validation/track-d-pwa.md.

Template should capture:

- tested Git revision;
- resolved instance name;
- icon/artwork revision;
- Desktop device/OS/browser/version;
- Android device/OS/browser/version;
- iPhone device/OS/browser/version;
- installation;
- standalone relaunch;
- start URL/deep links;
- worker registration;
- activation/update;
- login/logout;
- session expiry/revalidation;
- back-navigation;
- unsupported/failure behavior;
- blockers.

Do not claim real-device success without actual evidence.

Track E later adds permission/subscription/real-push evidence separately or links it into the integrated Pilot Gate.

Suggested commit: docs(pwa): add Track D validation matrix

## Task 13 — operator documentation

Update:

- .env.example;
- compose.yaml;
- README.md;
- relevant architecture/operator docs if exact behavior needs recording.

Document:

- LANSURFER_INSTANCE_NAME;
- optional fallback to LANsurfer;
- pilot requires explicit instance name;
- secure-context requirement;
- localhost/loopback development behavior;
- public HTTP warning;
- origin-change reinstall implications;
- temporary vs final icon ownership;
- icon size preparation instructions.

Do not modify ROADMAP merely to mark progress.

Suggested commit: docs(pwa): document instance identity and installability

## Task 14 — Track D -> E contract audit

Before final verification, audit the implementation for the future Track E handoff.

Prove:

- one worker;
- one registration path;
- one registration helper;
- no fetch handler;
- no app cache;
- no offline auth behavior;
- no push implementation;
- no VAPID/subscription code;
- Track E can later add push and notificationclick to the same worker;
- resolved Track-D instance name is the only notification-facing instance context.

Suggested commit only if fixes are needed.

## Task 15 — final verification

Generated files are never hand-edited.

Run:

task generate
task lint
go test -race ./...
task test:integration
task test:browser
task test:pwa
task build

Also verify manually where feasible:

- no service-worker fetch listener exists;
- no caches.open, CacheStorage, or IndexedDB use exists in Track D;
- no PWA-specific auth/session state exists;
- no second visual config system exists;
- no Track E notification behavior exists;
- /manifest.webmanifest and /service-worker.js are public;
- admin diagnostics are admin-only;
- insecure public HTTP warns but does not fail startup;
- manifest/worker remain root-scoped and origin-root assumptions are documented.

Do not mark issue #27 complete until automated checks pass, real-device validation evidence exists, final icon artwork is approved/replaced by Track G, and Pilot Gate evidence requirements are satisfied.

## Pull request

Prepare one coherent Track D PR from feat/track-d-instance-pwa and link issue #27.

Call out explicitly in the PR:

- one optional instance identity;
- no branding system;
- no offline behavior;
- no fetch/cache interception;
- one worker/registration path;
- Track E extension contract;
- temporary icon status if final Track-G assets are still pending;
- real-device validation status and remaining blockers.
