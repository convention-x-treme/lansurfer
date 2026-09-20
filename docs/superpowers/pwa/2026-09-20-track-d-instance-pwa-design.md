# Track D — Minimum Instance Identity & PWA Design

Status: Approved for implementation
Date: 2026-09-20
Branch: feat/track-d-instance-pwa
Tracking issue: #27
Base commit: bf7e1b36b87773239c32e5ce158ebabb5583c7d2

## Authority and intent

This design records the approved Track D decisions from the interactive design review.

Implement it together with AGENTS.md, docs/ROADMAP.md, docs/PRODUCT.md, docs/ARCHITECTURE.md, docs/BRANDING.md, the current UI kit / Track G work, issue #27, issue #28 only for the Track D -> E contract, and issue #30 only for the Track D -> G boundary.

Do not reopen settled product, security, branding, deployment, or lifecycle decisions during implementation. If code reality requires a change to the approved service-worker model, authentication/session model, deployment model, browser security model, Track D -> E boundary, Track D -> G boundary, or stable-1.0 scope, stop and request approval instead of silently redesigning Track D.

## Goal

Track D gives one LANsurfer installation a minimum human-readable identity and makes the application installable as a PWA with a stable, intentionally tiny service-worker foundation that Track E can later extend for Web Push.

Track D must not become a branding product, an offline-first feature, a generic PWA framework, a second frontend architecture, a Web Push implementation, or a new authentication/session model.

## Scope

Track D delivers:

- one optional instance-name configuration value;
- visible minimum instance identity in the existing server-rendered shell;
- a stable manifest;
- bundled/default application icons;
- one root-scoped service worker;
- service-worker registration and lifecycle ownership;
- safe behavior across login, logout, updates, failures, and unsupported browsers;
- explicit Track D -> E extension contracts;
- an admin-only read-only PWA diagnostic surface;
- automated browser verification for worker behavior;
- real-device validation obligations for Desktop, Android, and iPhone.

Stable 1.0 excludes offline-first/authenticated-offline behavior, persistent application caches, a generic PWA/plugin framework, SPA/router/state work, uploaded branding/logo/app icons, custom theme/colors/fonts/CSS, white-labeling, configurable welcome/contact content, admin-managed branding, speculative multi-context identity, and all Track E notification behavior.

## Instance identity

Add exactly one optional technical configuration value:

LANSURFER_INSTANCE_NAME

Rules:

- configuration only; no PostgreSQL persistence;
- no admin editor;
- trim surrounding whitespace;
- empty after trimming means unset;
- reject control characters and line breaks;
- maximum 64 Unicode code points;
- preserve ordinary spelling/case;
- no second configurable short name;
- do not derive identity from hostname.

Resolved identity is the configured value when present, otherwise LANsurfer.

General self-hosted operation may leave the setting unset. The selected real pilot must configure an explicit instance name and record it in validation evidence.

Instance identity stays structurally separate from branding.Brand. branding.Brand continues to own fixed LANsurfer product identity. Resolve/validate configuration once at startup and pass a small immutable instance-identity value into the web layer. Do not create persistent instance state or another branding system.

## Browser presentation

A configured instance name is a secondary deployment label, never a replacement for fixed LANsurfer branding.

When configured:

- LANsurfer wordmark remains fixed;
- login/public shell shows the instance name as a secondary label;
- authenticated shell shows the same secondary label;
- browser title format is <page> · <instance name>;
- manifest name is the resolved instance name;
- manifest short_name is the resolved instance name.

When unset:

- no extra secondary label is required;
- browser title remains <page> · LANsurfer;
- manifest name and short_name are LANsurfer.

Keep the fixed LANsurfer tagline as the HTML description. Track G owns presentation/styling of the secondary label.

## Manifest

Serve a dynamic manifest at /manifest.webmanifest.

Required members:

~~~json
{
  "name": "<resolved instance name>",
  "short_name": "<resolved instance name>",
  "id": "/",
  "start_url": "/",
  "scope": "/",
  "display": "standalone",
  "theme_color": "#0D1B2A",
  "background_color": "#0D1B2A",
  "icons": []
}
~~~

The real icon list is populated from the bundled asset contract below.

Rules:

- id stays stable across application releases and instance-name changes;
- start_url "/" deliberately reuses existing login/onboarding/root redirect behavior;
- direct/deep links remain their requested route;
- scope "/" matches the existing origin-root deployment model;
- omit orientation;
- omit shortcuts, screenshots, categories, share targets, protocol handlers, display_override, and other speculative capabilities.

LANSURFER_BASE_URL currently rejects subpaths. Track D does not add reverse-proxy base-path support.

Changing public origin/hostname is a PWA migration boundary. Track D does not attempt cross-origin migration; users may need to reinstall at the new origin.

Changing only LANSURFER_INSTANCE_NAME preserves the same manifest id. LANsurfer updates its manifest/UI source but does not promise immediate OS-level renaming of an already-installed icon.

## Install UX

Use native browser/OS installation mechanisms only.

Do not add custom install banners, install buttons, beforeinstallprompt handling, or product install-state UX.

If installation is unsupported or unavailable, ordinary LANsurfer browser use continues unchanged.

## Application-icon contract

Track D owns the technical asset contract. Track G owns final artwork approval.

Prepare one consistent icon source, preferably vector or at least 1024x1024 raster, and supply:

- 192x192 PNG, manifest purpose any;
- 512x512 PNG, manifest purpose any;
- dedicated 512x512 PNG, manifest purpose maskable;
- 180x180 PNG, Apple touch icon;
- favicon renditions from the same artwork.

Maskable requirements:

- do not merely label an ordinary icon as maskable;
- use an opaque background;
- keep all essential artwork inside the central circular safe zone whose diameter is 80% of the canvas.

Do not add an image-processing dependency to the normal LANsurfer build. Check prepared renditions into source control and let the existing asset-preparation pipeline copy them into embedded output.

### Temporary wave artwork

The wave artwork supplied during design may be merged into main as a clearly documented temporary PWA icon so implementation can proceed.

This exception does not make it the canonical LANsurfer signet.

Requirements:

- document it as temporary;
- Track G retains ownership of final signet approval/replacement;
- issue #27 / validation evidence keeps final-icon approval open;
- Track D cannot be marked done and the Pilot Gate cannot pass using only the temporary artwork.

Do not use the otter or prototype placeholders as the production signet.

## Service worker

Serve the worker at /service-worker.js and register it with scope "/" and updateViaCache "none".

Because the script itself is at the origin root, do not add Service-Worker-Allowed.

Add a dedicated TypeScript source entry, expected at web/src/service-worker.ts, and compile/bundle it with the existing esbuild toolchain. Use one classic bundled worker. Do not add a module-worker/runtime-import framework.

Track D worker behavior owns only:

~~~text
install  -> skipWaiting()
activate -> clients.claim()
~~~

Track D must not add fetch, CacheStorage, IndexedDB, message, background sync, periodic sync, background fetch, navigation preload behavior, offline fallback, or user/session state.

There is no offline application behavior in Track D.

## Registration

Register from the existing progressive frontend after window.load.

Use the same code path in development and production whenever the browser exposes the Service Worker API and the context permits registration.

Provide one focused registration helper/promise that is the only owner of navigator.serviceWorker.register(). The helper may expose enough state for normal startup, the admin PWA diagnostic page, and future Track E subscription code. Track E must consume this helper rather than independently registering another worker.

Do not turn the helper into a generic PWA framework.

## Update lifecycle

New workers activate immediately through skipWaiting() and clients.claim().

Do not add update banners, refresh-to-update UX, forced reloads solely because a worker changed, periodic update polling, or manual check-for-update controls.

Normal registration/page loads and browser update checks are sufficient.

If initial registration fails, LANsurfer continues normally and no player-facing toast appears.

If installing an update fails, the previously active worker remains and a later normal update attempt may recover.

The worker never caches application assets, so service-worker version changes do not own HTML/CSS/JS cache coherence.

## HTTP cache behavior

Track D does not change the existing frontend asset-cache architecture.

- normal embedded assets keep existing ETag/cache-control behavior;
- /service-worker.js is no-store;
- /manifest.webmanifest is no-store;
- PWA icons under /assets keep normal asset policy.

Do not introduce content-hashed asset URLs solely for Track D.

## Authentication and logout safety

The installed PWA uses the exact existing LANsurfer authentication/session model.

Do not add PWA-specific auth tokens, local credential storage, alternate callbacks, or PWA-only session code.

Logout continues to revoke the server session and expire the cookie through the existing flow.

Do not unregister the service worker at logout.

Security rationale:

- the worker is neutral;
- it contains no user identity;
- it owns no cache;
- it owns no persistent Track-D browser state;
- it cannot return stale authenticated HTML.

A subsequent login as another user uses the same neutral worker. Track E owns subscription/user-association semantics for Web Push.

### Back/forward cache protection

On an authenticated LANsurfer page restored from browser back/forward cache, force server revalidation/reload so a frozen authenticated DOM is not treated as trusted current state after logout/session changes.

Do not add general cross-tab logout broadcasting or session polling in Track D.

Other open tabs may remain visually stale until they navigate/interact, but the server session is authoritative and the worker cannot resurrect private data.

## Track D -> Track E contract

There is exactly one service worker and one registration path.

Track D owns:

- manifest;
- worker URL/scope;
- registration;
- install/activate lifecycle;
- update behavior;
- failure/degradation behavior;
- login/logout neutrality;
- focused registration helper.

Track E may later add concrete push and notificationclick handlers.

Track E may not independently add another worker, another registration path, a generic worker plugin framework, a fetch handler, application caching, or offline authenticated behavior.

Any future network interception/cache behavior requires an explicit architecture decision because it changes Track D security assumptions.

The notification-facing instance context is exactly the same resolved Track-D instance name. Track E gets no separate notification title/instance-label setting.

## Track D -> Track G contract

Track G / BRANDING / UI-kit own the LANsurfer wordmark, tagline, palette, typography, dark/light presentation, mascot, components, fixed operator attribution, and final signet/application-icon artwork.

Track D owns the instance-name data, semantic placement requirements for the secondary label, manifest identity, technical icon sizes/roles, installability, and worker behavior.

Track D must not add another visual configuration system.

## CSP and security headers

Keep existing restrictive CSP and add explicit narrow directives:

~~~text
worker-src 'self'
manifest-src 'self'
~~~

Do not add unsafe script/style exceptions or new remote origins.

Existing IGDB image allowance remains unchanged and is never service-worker cached.

Public infrastructure routes:

- /manifest.webmanifest;
- /service-worker.js;
- bundled PWA icons.

These are public same-origin GET/HEAD resources and must not require authentication or issue CSRF state.

The /admin/pwa page remains admin-only through existing authentication/authorization middleware.

## HTTPS and secure-context behavior

Do not make HTTPS a LANsurfer startup requirement.

Startup warning behavior:

- no warning for https;
- no warning for localhost/loopback HTTP development;
- for another public/non-loopback http base URL, log one clear warning that ordinary LANsurfer works but PWA/Web Push capabilities require a secure context.

The admin PWA page repeats that diagnostic.

Do not make PWA capability part of /healthz or /readyz.

## Admin PWA diagnostics

Add /admin/pwa and keep the current administration structure. Add a small PWA navigation/action near existing admin navigation; do not create a new admin dashboard solely for Track D.

This page is read-only.

Server-render immutable facts such as:

- resolved instance name;
- configured public/base URL;
- manifest location;
- service-worker location;
- secure-context expectation derived from base URL.

Progressive browser JavaScript fills in current-browser facts:

- whether JavaScript diagnostics ran;
- window.isSecureContext;
- Service Worker API support;
- registration state;
- registration scope;
- controller state;
- current registration failure if one occurs during that page load.

Do not persist diagnostics in PostgreSQL, localStorage, telemetry, or server memory as instance health.

If JavaScript is unavailable, state that browser diagnostics could not run.

Do not show PWA failure warnings to ordinary players.

## Progressive enhancement

With JavaScript unavailable, service workers unsupported, insecure context, registration failure, update failure, or installation unavailable, ordinary LANsurfer server-rendered workflows must continue.

Do not add a custom offline page.

If the server is unreachable, navigation fails through normal browser/platform network-error behavior.

## Verification

### Go tests

Cover:

- instance-name parsing/trim/fallback/validation;
- 64-code-point bound;
- control/newline rejection;
- unset fallback to LANsurfer;
- manifest JSON content and content type;
- manifest no-store;
- worker route, content type, and no-store;
- public access for manifest/worker/icons;
- CSP worker-src and manifest-src;
- shell/title instance rendering;
- fixed brand/description remains unchanged;
- admin PWA server facts;
- insecure-public-origin warning behavior;
- no-JavaScript ordinary rendering;
- root deployment assumptions remain explicit.

### Focused browser PWA suite

Add a dedicated task:

task test:pwa

Keep task test:browser focused on its current interaction-hardening purpose.

The PWA suite may use the real LANsurfer HTTP stack with purpose-built test controls where needed, but must remain narrowly focused on browser behavior Go cannot prove.

Cover:

- registration succeeds in supported local secure-context testing;
- root scope;
- activation;
- immediate update/replacement;
- controller behavior;
- registration-failure degradation;
- zero Track-D CacheStorage;
- no fetch/offline interception;
- normal navigation remains network-backed;
- logout/back-navigation protection;
- installed/authenticated session transitions as far as browser automation can safely prove.

Update repository testing guidance so this narrow PWA browser suite is an explicit exception rather than silently expanding the existing general browser-test purpose.

### Manual platform validation

Track D must record real installability evidence for Desktop, Android, and iPhone.

Record actual tested Git revision, resolved instance name, icon/artwork revision, device/OS/browser versions, installation flow, standalone relaunch, start/deep-link behavior, worker registration, activation/update, login/logout, session expiry/revalidation, back-navigation behavior, unsupported/failure behavior, and blockers.

Track E later adds permission/subscription/real-push evidence.

Store Track D evidence in docs/validation/track-d-pwa.md.

## Configuration and operations

Add LANSURFER_INSTANCE_NAME to configuration parsing/tests, .env.example, Compose passthrough, and README/operator documentation.

Document the optional fallback, pilot explicit-name requirement, HTTPS/secure-context requirement, localhost/loopback exception, origin-change/reinstall implications, and icon preparation requirements.

Track D requires no database migration, no new runtime service, no persistent volume, no scheduler, no background worker, and no new frontend framework.

## Acceptance checklist

Track D is complete only when:

- one optional instance identity value exists and is visible without becoming branding;
- manifest identity is stable and minimal;
- native installation works on required platform categories;
- worker is root-scoped and has only approved lifecycle responsibilities;
- no application fetch interception/cache/offline behavior exists;
- logout/user switching cannot expose stale private content through the worker;
- bfcache restoration of authenticated pages revalidates safely;
- unsupported/failed PWA capability does not break ordinary LANsurfer;
- admin diagnostics are read-only and browser-local;
- CSP remains restrictive;
- Track E has one stable worker/registration contract to extend;
- Track G remains owner of final visual assets;
- final approved icon assets replace/approve any temporary wave artwork before Track D/Pilot acceptance;
- Go, focused PWA browser, and real-device verification evidence exists;
- configuration/operator docs are updated;
- stable-1.0 exclusions remain intact.
