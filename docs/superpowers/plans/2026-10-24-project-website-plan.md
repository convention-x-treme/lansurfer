# Project website — implementation plan

Spec: `docs/superpowers/specs/2026-10-24-project-website-design.md`
Branch: `feat/site-project-website` · PR title: `feat(site): add static bilingual project website`

## Tasks

1. **Skeleton** — `cmd/lansurfer-site/main.go`: `build --out DIR [--release TAG]`, usage error otherwise, refuse non-empty DIR. Copy needed files from `internal/web/assets` embed FS into `DIR/assets`.
2. **Copy** — `content.go`: `Copy` struct + `en`, `de` values (hero, 3 steps, self-hosting, get involved, footer, nav labels, page titles/descriptions). Draft from README; mark for maintainer review.
3. **Templates** — `pages.templ`: `Page(lang, copy, alternates, body)`, `Home`, `Legal`. Head: canonical, hreflang (`en`, `de`, `x-default`→EN), meta description, favicon. Language switch link. `<picture>` screenshots per theme. No script tags. Run `task generate`.
4. **Stylesheet** — add `@source "../../cmd/lansurfer-site/**/*.templ";` to `web/src/app.css`; regenerate committed `internal/web/assets/dist/app.css`.
5. **Legal** — `content/{en,de}/{imprint,privacy}.md` placeholders clearly marked until maintainer supplies text; goldmark render. Build fails if a file is empty.
6. **Screenshots** — `playwright.site.config.ts` (webServer = browsertest fixture, color scheme per project dark/light), `tools/site-screenshots/capture.spec.ts` for landing, Game Requests, lobby; output `cmd/lansurfer-site/static/screenshots/`. Commit PNGs.
7. **Taskfile** — `site:build` (`go run ./cmd/lansurfer-site build --out .site` after removing `.site`), `site:screenshots`. Add `.site/` to `.gitignore`.
8. **Test** — `cmd/lansurfer-site/main_test.go`: pages exist, hreflang pairs resolve, all local links resolve, no `<script`.
9. **Workflow** — `.github/workflows/site.yml` (release published + dispatch with tag) → upload-pages-artifact → deploy-pages.
10. **Docs harvest** — ARCHITECTURE §20, TERMINOLOGY, README, CONTRIBUTING; delete this plan and the spec (or keep spec only if rationale not captured).
11. **Smoke** — `task site:build`, open EN/DE in browser, dark + light, 375 px and 1280 px widths; `task lint`, `task test`.

## Manual / operator (record in issue)

- Pages source = Actions, custom domain, DNS CNAME, HTTPS, apex redirect.
- Supply imprint/privacy texts EN + DE; review copy.
- First deploy via `workflow_dispatch` with current release tag.
