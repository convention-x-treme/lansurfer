# Brand and artwork rights — pre-public inventory

This document distinguishes third-party bundled assets and LANsurfer's own branding/artwork from the software license in [LICENSE](LICENSE). It records **repository evidence**, not legal ownership or authorization to publish. The source-code AGPL-3.0 license must not be read as a grant of trademark rights in the LANsurfer name or its identity marks.

## Original project identity and visual assets

The project owner confirmed the creation context and public-repository decision on 2026-10-16 in [issue #92](https://github.com/convention-x-treme/lansurfer/issues/92). These are owner statements supported by repository file references, **not independent proof of exclusive intellectual-property rights**.

| Asset family | Source and distributed files | Owner-reported origin and repository evidence |
| --- | --- | --- |
| Name, logo and wordmark | `LANsurfer`; `docs/logo/logo-with-slogan.png`; styling in `DESIGN.md` | Visual logo created entirely with Claude, without external image templates. The LANsurfer name/mark remains a separate trademark question. |
| Product signet, app and PWA icons | `docs/logo/icon.png`; `docs/pwa-icons/*`; bundled `internal/web/assets/dist/images/icons/*` | Original icon created entirely with Claude, without external image templates. `DESIGN.md` identifies the favicons/PWA images as derived renditions of that icon. |
| Otter mascot illustrations | `docs/mascot/*.png`; `docs/mascot/web/*.webp`; bundled `internal/web/assets/dist/images/*.webp` | Original illustrations generated with ChatGPT, then refined with Claude; the owner reports 100% AI-generated imagery with no third-party reference pictures. `scripts/prepare-assets.mjs` identifies shipped renditions. |
| UI-kit frames and design exports | `docs/ui-kit/*.png`, `docs/ui-kit/index.html`, `docs/ui-kit/LANsurfer Colour Palette.html` | Screenshots and design compositions created with Claude. `docs/ui-kit/README.md` links to the original Claude Design project. Standalone HTML exports can embed fonts, scripts and media with separate third-party rights. |

### Creation context and consent for a public repository

- The owner used **personal ChatGPT and Claude accounts**, not a business/team account, for a **private, unpaid hobby project in the context of volunteer association activity**. The owner reports no paid commissioned work and no external image templates for the original project graphics.
- The owner **explicitly consents to keeping the mascot, logo, app/PWA icons and UI-kit design images in the repository when it becomes public**, where GitHub visitors will be able to view, clone and download them.
- This consent is **not a separate grant of a broadly reusable open artwork license or trademark license**. The source-code [AGPL-3.0 license](LICENSE) must not be represented as establishing exclusive trademark rights. The legal status and any independent copyright protection of AI-assisted visual material remain subject to applicable law and the relevant service terms.

### Remaining publication checks

The exact model versions, generation dates and terms applicable at the time of creation are not evidenced in the repository. The personal/unpaid context is documented but is not an independent legal determination of how each provider classifies association-related use. Check any separately embedded third-party fonts, scripts or media in the UI-kit HTML export, and verify whether the external Claude Design link should be treated as a public reference or removed. Rights to the LANsurfer name/mark have not been independently cleared.

**Public-visibility checkpoint:** The owner has approved publication of the named original graphic families, but that decision alone does not clear every third-party embedded component, trademark concern or historical file. Review outstanding findings in #92 and the public-switch prerequisite in #150 before changing repository visibility. Files present in historical commits can become accessible even if removed from the latest tree.

## Bundled third-party assets

Bundled Schibsted Grotesk and JetBrains Mono fonts (OFL-1.1), Phosphor Icons (MIT), htmx (0BSD), Tailwind CSS (MIT), and DaisyUI (MIT) retain their upstream license terms. Full license text and exact shipped versions are in [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md) and the build-produced `internal/web/assets/dist/licenses/` directory. Third-party fonts and icons do **not** inherit the LANsurfer logo/mascot rights decision.

## Follow-up ownership

- Original artwork source/permission decisions are tracked as a finding of [#92](https://github.com/convention-x-treme/lansurfer/issues/92).
- Dependency/attribution/license notices are maintained in [#89](https://github.com/convention-x-treme/lansurfer/issues/89).
- The one-time repository publication review, including whether external design references and the historical contents may be publicly exposed, is coordinated in [#150](https://github.com/convention-x-treme/lansurfer/issues/150).

This file is a technical provenance summary and is not a replacement for rights-holder permission or legal review.
