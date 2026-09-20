# Track D — PWA/Web Push real-device validation

Evidence template for issue [#27](https://github.com/convention-x-treme/lansurfer/issues/27) and
the Pilot Gate's "real Web Push/PWA verification" requirement
([ROADMAP.md](../ROADMAP.md#pilot-gate)). Fill in only rows with evidence actually collected on a
real device against a real deployment — do not fabricate a pass. An empty/"not yet exercised" row
is the correct state until someone runs the check.

Do not record a private event date or attendance estimate here (per the Pilot Gate's own
constraint); record only the deployment/browser/device facts and observed behavior.

**Responsible reviewer:** _(name the GitHub user who ran this validation)_
**Tested revision:** _(commit SHA or tag)_
**Deployment/base URL tested:** _(must be a secure context — HTTPS, or loopback/localhost — for
installability/Web Push to be reachable at all; see `SecureContextStatus` and the `/admin/pwa`
diagnostics page)_

## Desktop

| Check | Browser / version | Result | Notes |
| --- | --- | --- | --- |
| Manifest reachable, correct name/icons | | Not yet exercised | |
| Install prompt / installability | | Not yet exercised | |
| Service worker registers, activates, takes control | | Not yet exercised | |
| Reinstall/update replaces the worker immediately | | Not yet exercised | |
| Logout does not leave stale authenticated content visible on back/forward | | Not yet exercised | |

## Android

| Check | Browser / version | Result | Notes |
| --- | --- | --- | --- |
| Manifest reachable, correct name/icons | | Not yet exercised | |
| "Add to Home screen" / install banner | | Not yet exercised | |
| Maskable icon renders correctly (no clipped artwork) in the launcher | | Not yet exercised | |
| Service worker registers, activates, takes control | | Not yet exercised | |

## iPhone

| Check | Browser / version | Result | Notes |
| --- | --- | --- | --- |
| Manifest reachable, correct name | | Not yet exercised | |
| "Add to Home Screen" via Safari share sheet | | Not yet exercised | |
| Apple touch icon renders correctly | | Not yet exercised | |
| Service worker registers, activates, takes control | | Not yet exercised | |

## Notes

- Real Web Push subscription/delivery verification belongs to Track E (issue #28), not this
  document; this artifact covers Track D's installability/manifest/service-worker contract only.
- The icon artwork used above is the temporary wave icon documented in
  `docs/superpowers/pwa/2026-09-20-track-d-instance-pwa-design.md` — it is not final LANsurfer
  branding. Re-run this validation after Track G's canonical icon lands before treating Track D as
  complete.
