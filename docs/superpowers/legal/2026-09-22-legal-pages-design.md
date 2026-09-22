# Public Terms, Privacy, and Contact/Reporting Surface — Design

Status: approved design, not yet implemented.
Issue: [#39](https://github.com/convention-x-treme/lansurfer/issues/39), section "1. Public Terms, Privacy and support/reporting surface".

## Goal

Discord's Developer Terms (§5(a)) require a publicly reachable privacy policy that describes collected data, use/sharing, retention, and deletion requests. The Developer Policy requires a way for users to report issues and violations. The Developer Portal needs stable Terms of Service and Privacy Policy URLs.

This design delivers:

- stable public `/terms` and `/privacy` pages
- operator legal identity and contact configured through environment variables
- a contact/reporting path (email for private matters, GitHub issues for software bugs)
- links to all of the above from every page shell, including the login screen where Discord is offered

Out of scope (remaining #39 sections): Discord API-data deletion procedure, encryption at rest, Track E webhook installation, consent copy for notification setup, incident response. The privacy text references the email request path those sections build on.

## Scope change

PRODUCT §14 and ARCHITECTURE ("Minimum instance identity") currently state that 1.0 does not require a configurable contact link and must not reintroduce one through configuration. This design deliberately adds a narrow exception: **legal operator identity and contact** (entity name, postal address, contact email, imprint URL) are operator configuration, because each deployment is its own data controller. This is not a branding surface: no welcome text, logo, colors, or free-form legal text. PRODUCT §14 and ARCHITECTURE are updated to state this exception.

The "Powered by …" attribution (`branding.OperatorName` / `OperatorURL`) stays unchanged. It names the operating community; the legal entity is a separate value.

## Configuration

Loaded in `internal/platform/config` into a `Legal` value; all values trimmed and optional.

| Variable | Meaning | Validation |
|---|---|---|
| `LANSURFER_LEGAL_ENTITY_NAME` | Controller / service provider name | no control characters, ≤ 200 runes |
| `LANSURFER_LEGAL_ADDRESS` | Postal address, single line (commas separate parts) | no control characters, ≤ 300 runes |
| `LANSURFER_LEGAL_CONTACT_EMAIL` | Privacy, abuse, Discord and deletion contact | bare address parsed by `net/mail` |
| `LANSURFER_IMPRINT_URL` | External imprint (Impressum) | absolute `http`/`https` URL |

Startup fails when Discord login is configured and `LANSURFER_LEGAL_ENTITY_NAME` or `LANSURFER_LEGAL_CONTACT_EMAIL` is empty, because Discord requires a privacy policy naming a responsible party with a contact. Without Discord, missing values render as "not configured" on the pages.

`.env.example` values:

```dotenv
LANSURFER_LEGAL_ENTITY_NAME=Computerfreunde Karlsdorf-Neuthard e.V.
LANSURFER_LEGAL_ADDRESS=<Street and number>, <Postal code> Karlsdorf-Neuthard, Germany
LANSURFER_LEGAL_CONTACT_EMAIL=datenschutz@convention-x-treme.de
LANSURFER_IMPRINT_URL=<https://www.convention-x-treme.de/impressum>
```

The project issue tracker (`https://github.com/convention-x-treme/lansurfer/issues`) is a fixed constant in `internal/web/branding`, because it belongs to the software, not the deployment.

## Routes and pages

- `GET /terms` and `GET /privacy`, public (no session required), rendered with the normal `Layout` so signed-in and anonymous viewers get their usual shell.
- One `internal/web/pages/legal.templ` holds both documents as fixed English text with configured values interpolated (templ escaping; mailto/imprint links built from validated values). Deployment facts that are not configuration (log retention, hosting location) are part of the fixed text; another operator edits the template, as with `branding.OperatorName`.
- Each page shows a "Last updated" date constant; changing the text means changing that date.
- Handler lives with other public page handlers in `internal/web/handler`; it only renders.

The pages are a drafted baseline, not legal advice. The operator must have them reviewed before registering the URLs in the Discord Developer Portal.

### Privacy Policy content

Sections, grounded in the current schema and code:

1. **Controller and contact** — entity name, address, contact email, imprint link.
2. **What LANsurfer stores**
   - Account: local user ID, Game Nick, role, creation/update timestamps.
   - Linked login identities: provider kind, provider subject ID, provider display name (private account metadata; also used as a Game Nick suggestion). Email only when a generic OIDC provider supplies it. Discord: user ID and username/global display name; Discord email is never requested; OAuth access/refresh tokens are not stored after login.
   - Sessions: hashed session token with creation, last-seen, and expiry timestamps (7-day lifetime).
   - Login flows: short-lived state (10 minutes) including provider metadata, removed after expiry.
   - Participation: Game Requests you create, memberships, followed Games, recently viewed requests, and request activity history.
   - Server request logs: client IP address, method, path, request ID. Kept for at most 30 days, then deleted.
3. **Cookies** — session, CSRF, login browser binding, theme preference. All strictly necessary or preference; no analytics, advertising, or tracking.
4. **Purposes and legal bases** — providing the service (GDPR Art. 6(1)(b)); security and abuse prevention via logs and sessions (Art. 6(1)(f)).
5. **Recipients / third parties** — the identity provider you choose (OIDC provider, Discord, Twitch) during login; Hosting: no third-party hosting provider processes the data. LANsurfer runs on the operator's own server, housed in a colocation space at the Nuremberg (Germany) data center of Core Backbone GmbH, which provides space, power, and network connectivity but does not operate the server. Game covers are fetched server-side from IGDB; your browser never contacts IGDB and no user data is sent. No sale or advertising use of data.
6. **Retention and deletion** — data kept while the account exists; unlinking an identity removes it; account or Discord data deletion on request via the contact email.
7. **Your rights** — access, rectification, erasure, restriction, portability, objection (Art. 15–21); complaint to a supervisory authority, e.g. the Landesbeauftragter für den Datenschutz und die Informationsfreiheit Baden-Württemberg.
8. **Security** — TLS in transit (operator reverse proxy), hashed session tokens, no stored OAuth tokens. Encryption at rest is covered by a later #39 section.
9. **Changes** — date of last update.

Discord notification webhooks (Track E) are not described because they do not exist yet; Track E must extend this page when it ships.

### Terms of Service content

1. **The service** — LANsurfer instance operated by the configured entity for its LAN events; free, no guaranteed availability.
2. **Accounts** — login only via configured third-party providers; those providers' terms apply to their accounts.
3. **Acceptable use** — no impersonation through Game Nicks, harassment, hate, spam, illegal content, or interfering with the service.
4. **Your content** — Game Requests and Game Nicks are visible to other players of the instance; you are responsible for them.
5. **Moderation** — the operator may remove content or suspend accounts that break these terms.
6. **Liability** — unlimited for intent, gross negligence, and injury to life, body, or health; otherwise limited as permitted by German law.
7. **Governing law** — German law.
8. **Changes** — date of last update.
9. **Contact & reporting** (`id="contact"`) — email for support, abuse, policy violations, Discord-related issues, and data requests; GitHub issues for software bugs.

## UI placement

One shared `components.Footer` renders in all three shells:

```text
Terms · Privacy · Contact & reporting · Imprint · Powered by Convention-X-Treme
```

- "Contact & reporting" links to `/terms#contact`.
- Imprint is omitted when `LANSURFER_IMPRINT_URL` is unset.
- **Not `position: fixed` or `sticky`.** A fixed bar costs mobile viewport height and collides with the bottom-right `#live-notices` toast region. Instead each shell uses `min-h-screen flex flex-col` with `main` as `flex-1`, so the footer sits at the viewport bottom on short pages and after the content on long ones. The public layout already works this way.

Per shell:

- `Layout`, anonymous: existing `Footer` gains the link row.
- `Layout`, authenticated app shell: footer added under `main` inside the content column; sidebar attribution unchanged.
- `AuthLayout` (login, onboarding, login errors): footer added; the login panel additionally shows "By logging in you agree to the Terms and acknowledge the Privacy Policy." under the provider buttons.

Legal values reach templates the same way the instance name does: a `middleware.Legal(cfg)` sets them on the request context and `middleware.LegalInfo(ctx)` reads them, mirroring `middleware.Instance` / `InstanceName`. Every shell and page reads from there, so no page handler signature changes.

## Testing

- Config: parsing, trimming, email and URL validation, Discord-without-legal-values startup error.
- Handler: `/terms` and `/privacy` return 200 without a session and render configured entity, address, and email.
- Layout: footer links present in all three shells; imprint link absent when unset.
- No browser tests: no interaction or browser-platform behavior involved.

## Documentation updates

- PRODUCT §14: legal identity/contact exception; public Terms/Privacy pages.
- ARCHITECTURE config section: the four variables and the Discord startup rule.
- README configuration list and `.env.example`.
