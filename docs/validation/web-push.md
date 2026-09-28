# Web Push real-device validation

Real Web Push evidence for Web Push. Record only behavior observed against a real HTTPS deployment and supported browser/device. Automated browser tests are separate and do not count as real-device evidence. Never record Push endpoints, `p256dh`, `auth`, subscription secrets, or private VAPID keys.

## Deployment under test

The initial E2-C9 automated-validation run had no real deployment available. Later manual observations are recorded below without an exact deployment, revision, or browser/device version.

- **Tested Git revision:** `8d650d648f5710f0fea4774e7c49b90e1284977d` (initial E2-C9 automated-validation run; no real-device validation performed in that run)
- **Instance name:** Not observed
- **Base URL:** Not available; no reachable HTTPS test instance was supplied or configured for this run

## Real browser/device validation

**Status: PARTIALLY VERIFIED; remaining manual validation is open.** Real-browser permission/subscription registration, real Push receipt, and visible-window suppression were later observed. Exact test environment and revision were not recorded. No platform/version matrix is claimed.

| Environment | Browser / OS / device version | Permission flow | Subscription creation | Real Push receipt | Visible-window suppression | Notification click / deep link | Logout / current-device removal | Session expiry | Limits / blockers |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| Real browser/device; details not recorded | Not recorded | Observed | Observed | Observed | Observed | Not recorded | Not recorded | Not recorded | Exact revision, deployment, browser/OS/device versions unavailable; click, logout/removal, and session-expiry evidence remains open. |

## Automated checks (not real-device evidence)

The E2-C9 automation baseline supplied for this run reports:

- `task test:pwa`: **31/31 PASS**
- Frontend build: **PASS**
- TypeScript: **PASS**
- `git diff --check`: **PASS**

These automated checks do not verify real Push service delivery, real browser permission prompts, or real-device behavior. The browser test contract explicitly requires no real third-party Push provider.

## Remaining manual validation

- Observe notification click/deep-link behavior.
- Observe logout and current-device removal behavior.
- Observe session-expiry behavior.
- Record exact tested revision, deployment, supported browser, OS, and device versions.

The earlier “no notification” observation was the intentional service-worker suppression while a LANsurfer window was visible; it was not a failed Push receipt.
