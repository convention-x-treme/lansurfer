# Brand and artwork rights — pre-public inventory

This document distinguishes third-party bundled assets and LANsurfer's own branding/artwork from the software license in [LICENSE](LICENSE). It records **repository evidence**, not legal ownership or authorization to publish. The source-code AGPL-3.0 license must not be read as a grant of trademark rights in the LANsurfer name or its identity marks.

## Original project identity and visual assets

| Asset family | Source and distributed files | Evidence in the repository | Publication rights |
| --- | --- | --- | --- |
| Name, wordmark and lockup | `LANsurfer`; `docs/logo/logo-with-slogan.png`; wordmark styling in `DESIGN.md` | `DESIGN.md` specifies canonical use | **Unconfirmed:** no independently documented authorship, trademark clearance or specific artwork reuse grant found |
| Product signet and app icon | `docs/logo/icon.png`; `docs/pwa-icons/*`; bundled `internal/web/assets/dist/images/icons/*` | `DESIGN.md` states that PWA and favicon files are renditions of `icon.png` | **Unconfirmed:** rights to underlying artwork and its derived variants need owner confirmation |
| Otter mascot illustrations | `docs/mascot/*.png`; `docs/mascot/web/*.webp`; bundled `internal/web/assets/dist/images/*.webp` | `DESIGN.md` and `scripts/prepare-assets.mjs` identify masters and web renditions | **Unconfirmed:** source/commission details and reuse rights are not evidenced here |
| UI-kit frames and design exports | `docs/ui-kit/*.png`, `docs/ui-kit/index.html`, `docs/ui-kit/LANsurfer Colour Palette.html` | `docs/ui-kit/README.md` links to a Claude Design project, and describes the HTML as a standalone snapshot with embedded fonts/images/scripts | **Unconfirmed:** rights for exports, embedded media and onward distribution must be checked, including whether the source link is accessible after public release |

The inventory is limited to visual source and generated files at current `main` revision `b970de039756931589292783e90efafb9bc701eb` on 2026-10-16. Generated renderings are not separate proof of permission to redistribute the underlying originals.

The project owner reports that some LANsurfer artwork was AI-generated, but did not identify the files, provider/model, applicable terms, or resulting rights. This report does not establish permission to publish or redistribute the assets.

**Publication checkpoint (manual decision):** A maintainer with knowledge of how each original logo, mascot and UI-kit export was produced or licensed must confirm publication rights, including any third-party source material or generated-art terms. If a family cannot be cleared, replace or omit it **before** making the entire Git history public. History can still contain old copies even if the latest tree no longer does. This document intentionally does not claim a completed legal clearance or grant a new artwork license.

## Bundled third-party assets

Bundled Schibsted Grotesk and JetBrains Mono fonts (OFL-1.1), Phosphor Icons (MIT), htmx (0BSD), Tailwind CSS (MIT), and DaisyUI (MIT) retain their upstream license terms. Full license text and exact shipped versions are in [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md) and the build-produced `internal/web/assets/dist/licenses/` directory. Third-party fonts and icons do **not** inherit the LANsurfer logo/mascot rights decision.

## Follow-up ownership

- Original artwork source/permission decisions are tracked as a finding of [#92](https://github.com/convention-x-treme/lansurfer/issues/92).
- Dependency/attribution/license notices are maintained in [#89](https://github.com/convention-x-treme/lansurfer/issues/89).
- The one-time repository publication review, including whether external design references and the historical contents may be publicly exposed, is coordinated in [#150](https://github.com/convention-x-treme/lansurfer/issues/150).

This file is a technical provenance summary and is not a replacement for rights-holder permission or legal review.
