# PWA real-device validation

Evidence template for issue [#27](https://github.com/convention-x-treme/lansurfer/issues/27) and
the Pilot Gate's "real Web Push/PWA verification" requirement
([ROADMAP.md](../ROADMAP.md#pilot-gate)). Fill in only rows with evidence actually collected on a
real device against a real deployment — do not fabricate a pass. An empty/"not yet exercised" row
is the correct state until someone runs the check.

Do not record a private event date or attendance estimate here (per the Pilot Gate's own
constraint); record only the deployment/browser/device facts and observed behavior.

Real Web Push subscription/delivery verification belongs to notification delivery (issue #28), not this
document; this artifact covers PWA/instance identity's installability/manifest/service-worker contract only.

## Deployment under test

- **Responsible reviewer:** _(GitHub user who ran this validation)_
- **Tested Git revision:** _(commit SHA)_
- **Deployment/Base URL tested:** _(must be a secure context — HTTPS, or loopback/localhost — for
  installability/Web Push to be reachable at all; check `/admin/pwa` for the resolved status)_
- **Resolved instance name:** _(value shown at `/admin/pwa`, or "LANsurfer" if unset)_
- **Icon/artwork revision:** _(commit that produced `docs/pwa-icons/*`; note whether it is still the
  temporary wave artwork or brand/UI system work's approved final signet)_

## Desktop

| Check | Browser / version | Result | Notes |
| --- | --- | --- | --- |
| Manifest reachable, correct name/icons | | Not yet exercised | |
| Installation | | Not yet exercised | |
| Standalone relaunch | | Not yet exercised | |
| Start URL / deep link behavior | | Not yet exercised | |
| Service worker registration | | Not yet exercised | |
| Activation / update replaces the worker immediately | | Not yet exercised | |
| Login / logout | | Not yet exercised | |
| Session expiry / revalidation | | Not yet exercised | |
| Back-navigation (bfcache) does not show stale authenticated content | | Not yet exercised | |
| Unsupported/failure behavior degrades without breaking ordinary use | | Not yet exercised | |

## Android

| Check | Device / OS / browser / version | Result | Notes |
| --- | --- | --- | --- |
| Manifest reachable, correct name/icons | | Not yet exercised | |
| Installation ("Add to Home screen" / install banner) | | Not yet exercised | |
| Maskable icon renders correctly (no clipped artwork) in the launcher | | Not yet exercised | |
| Standalone relaunch | | Not yet exercised | |
| Start URL / deep link behavior | | Not yet exercised | |
| Service worker registration | | Not yet exercised | |
| Activation / update replaces the worker immediately | | Not yet exercised | |
| Login / logout | | Not yet exercised | |
| Session expiry / revalidation | | Not yet exercised | |
| Back-navigation (bfcache) does not show stale authenticated content | | Not yet exercised | |
| Unsupported/failure behavior degrades without breaking ordinary use | | Not yet exercised | |

## iPhone

| Check | Device / iOS / browser / version | Result | Notes |
| --- | --- | --- | --- |
| Manifest reachable, correct name | | Not yet exercised | |
| Installation (Safari share sheet "Add to Home Screen") | | Not yet exercised | |
| Apple touch icon renders correctly | | Not yet exercised | |
| Standalone relaunch | | Not yet exercised | |
| Start URL / deep link behavior | | Not yet exercised | |
| Service worker registration | | Not yet exercised | |
| Activation / update replaces the worker immediately | | Not yet exercised | |
| Login / logout | | Not yet exercised | |
| Session expiry / revalidation | | Not yet exercised | |
| Back-navigation (bfcache) does not show stale authenticated content | | Not yet exercised | |
| Unsupported/failure behavior degrades without breaking ordinary use | | Not yet exercised | |

## Blockers

_(list anything that prevented completing a row above, or that must be resolved before PWA/instance identity /
the Pilot Gate can rely on this evidence)_

## Notes

The icon artwork used above is the temporary wave icon documented in
`docs/superpowers/pwa/2026-09-20-track-d-instance-pwa-design.md` — it is not final LANsurfer
branding. Re-run this validation after brand/UI system work's canonical icon lands before treating PWA/instance identity or
the Pilot Gate's PWA requirement as satisfied.
