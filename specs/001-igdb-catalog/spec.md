# Feature specification: IGDB catalog import and rich game metadata

## Status

Draft pilot specification.

This document evaluates the SDD workflow. It is derived from the existing approved IGDB scope on `main` and does not authorize implementing a later roadmap milestone ahead of the repository's current milestone.

## Problem

LANsurfer has an authoritative local Game catalog, but administrators currently need to maintain catalog data manually. That makes it expensive to add useful descriptive metadata and cover art and increases the chance of inconsistent naming or incomplete Game information.

An optional external catalog should make administration faster without making LANsurfer dependent on that provider or allowing remote data to silently replace local decisions.

## Goal

Allow a global administrator to deliberately search IGDB, preview provider data, create or enrich local Games, and later refresh selected metadata while preserving LANsurfer's local catalog as the source of truth.

Players should benefit from richer Game presentation, including cover art and selected metadata, while all core catalog and Game Request workflows continue to work when IGDB is unavailable or not configured.

## Non-goals

- Automatically synchronizing the local catalog with IGDB.
- Automatically merging Games by name or fuzzy matching.
- Making IGDB required for normal LANsurfer operation.
- Importing every field exposed by IGDB.
- Supporting catalog providers other than IGDB.
- Scraping IGDB or another website.
- Proxying or caching IGDB covers locally.
- Allowing ordinary users to import, attach, or refresh provider data.
- Replacing locally edited fields silently during refresh.

## Actors

- **Global administrator** — searches IGDB, previews results, creates local Games from a result, explicitly attaches an IGDB identity to an existing Game, and previews/applies refreshes.
- **Signed-in player** — sees approved rich metadata and cover presentation through normal Game and Game Request views; does not control provider synchronization.
- **Operator** — optionally configures Twitch/IGDB credentials and is responsible for understanding the provider terms and remote-cover privacy behavior.

## Functional requirements

### Provider availability

- **REQ-001:** IGDB integration must be optional. LANsurfer's local catalog and Game Request workflows remain usable when credentials are absent, IGDB is unreachable, or the provider returns an error.
- **REQ-002:** Only global administrators may invoke provider search, preview, import, attachment, and refresh actions.

### Search and preview

- **REQ-003:** An administrator can search IGDB by a user-supplied query and receive candidate results suitable for preview.
- **REQ-004:** Before creating or changing local Game data, LANsurfer presents the relevant provider data to the administrator for review.
- **REQ-005:** A provider result is never automatically matched or merged with a local Game solely because names are equal or similar.

### Creating and attaching

- **REQ-006:** From an IGDB result, an administrator can explicitly create a new local Game using selected provider data.
- **REQ-007:** An administrator can explicitly attach the IGDB external identity from a provider result to an existing local Game.
- **REQ-008:** Attaching an IGDB identity to an existing Game does not silently overwrite locally managed fields.

### Rich metadata

- **REQ-009:** The supported provider-derived metadata for 1.0 is limited to summary, first release date, platform names, and one cover reference, in addition to the external provider identity needed to reconnect the local Game to IGDB.
- **REQ-010:** Local Game data remains authoritative after import. Provider-derived fields become ordinary locally stored data once explicitly accepted.
- **REQ-011:** Game catalog views and relevant Game Request cards/details can present the stored rich metadata and cover when available, with graceful fallback when it is absent.

### Refresh

- **REQ-012:** An administrator can request a refresh preview for a local Game that has an attached IGDB identity.
- **REQ-013:** Refresh shows provider changes before applying them.
- **REQ-014:** The administrator chooses which supported fields to update. Unselected local fields remain unchanged.
- **REQ-015:** Refresh never overwrites local edits without explicit field-level administrator approval.

### Remote cover behavior

- **REQ-016:** IGDB cover images may load directly only from the approved `images.igdb.com` host.
- **REQ-017:** The application provides a local placeholder when a cover is absent or cannot be loaded.
- **REQ-018:** The application uses a fixed Content Security Policy allowlist for the approved IGDB image host and sends `Referrer-Policy: no-referrer` for the relevant application behavior.
- **REQ-019:** Operator/user documentation discloses that loading an IGDB-hosted cover exposes the viewer's IP address to IGDB.

### Attribution and provider requirements

- **REQ-020:** Required IGDB attribution and operator setup/provider-term information is visible in the appropriate administration or documentation surfaces.

## Scenarios

### Scenario: Search IGDB without changing the local catalog

**Given** IGDB credentials are configured and a global administrator is signed in

**When** the administrator searches for a Game

**Then** LANsurfer displays provider candidates for preview

**And** no local Game or external-ID relationship is created merely by searching or previewing.

### Scenario: Create a local Game from an IGDB result

**Given** an administrator has previewed an IGDB candidate

**When** they explicitly choose to create a local Game from that candidate and confirm the selected data

**Then** LANsurfer creates an ordinary local Game

**And** stores the approved IGDB external identity and selected supported metadata

**And** subsequent normal operation reads the local record rather than requiring IGDB.

### Scenario: Attach IGDB to an existing local Game

**Given** a local Game already exists

**And** the administrator has previewed an IGDB result

**When** they explicitly attach that result to the local Game

**Then** LANsurfer records the IGDB external identity

**And** existing local fields are not silently overwritten.

### Scenario: Refresh metadata selectively

**Given** a local Game has an attached IGDB identity

**And** local metadata differs from the current provider data

**When** the administrator requests a refresh

**Then** LANsurfer shows the supported field differences before changing the local record

**And when** the administrator selects only some fields and confirms

**Then** only those fields are updated.

### Scenario: Provider unavailable

**Given** IGDB is unconfigured, unreachable, rate-limited, or otherwise failing

**When** a player browses Games or uses Game Requests

**Then** normal local workflows continue to work

**And** provider-specific administrator actions fail gracefully without corrupting local catalog data.

### Scenario: Remote cover fails

**Given** a local Game contains an approved IGDB cover reference

**When** the remote image cannot be loaded

**Then** the UI uses the local placeholder

**And** the failure does not prevent catalog or Game Request interaction.

## Failure and degraded behavior

- Missing provider credentials disable or make unavailable provider-specific administrator operations without affecting the local catalog.
- Provider authentication, network, rate-limit, malformed-response, and upstream-server failures must surface as recoverable administration errors and must not mutate local data unless an administrator-confirmed operation has reached the local persistence step successfully.
- A cover-host failure affects only cover presentation.
- A provider failure during refresh leaves the existing local Game unchanged.

## Security and privacy constraints

- Twitch/IGDB credentials are operator configuration and are not exposed to ordinary users.
- Provider administration actions require global administrator authorization.
- Remote cover loading is restricted to the approved IGDB image host; this exception must not become a general remote-content mechanism.
- The privacy implication of direct cover loading (viewer IP disclosure to IGDB) must be documented.
- Provider data is untrusted external input and must be validated/escaped through the normal LANsurfer boundaries before persistence or rendering.

## Acceptance criteria

- An administrator can search and preview IGDB without modifying the local catalog.
- An administrator can explicitly create a local Game from a provider result.
- An administrator can explicitly attach an IGDB identity to an existing local Game without implicit field replacement.
- Supported rich fields are limited to summary, first release date, platform names, and one cover reference.
- Refresh presents changes first and only applies administrator-selected fields.
- No automatic name-based merge exists.
- Local catalog and Game Request workflows remain operational with no IGDB configuration and during simulated provider failure.
- IGDB covers render only through the approved host policy, use the documented privacy safeguards, and fall back locally on failure.
- Relevant provider attribution/setup/privacy documentation exists.

## Open questions

These are intentionally left for the implementation plan rather than silently decided in the specification:

- What exact IGDB result fields are needed in the search/preview projection beyond the supported persisted metadata?
- What local representation should platform names use (normalized relation vs. persisted display values) while staying proportional to the current product need?
- What exact administrative UI flow best distinguishes “create from IGDB”, “attach to existing”, and “refresh” without accidental destructive actions?
- What cover reference form should be persisted so the image URL/size variant policy stays controlled by LANsurfer rather than arbitrary remote URLs?

## Source constraints

This pilot specification is derived from the existing `main` branch documentation, especially:

- `docs/ROADMAP.md` — Milestone 12, **IGDB Catalog Import and Rich Game Metadata**.
- `docs/PRODUCT.md` — local authoritative Game catalog, administrator-controlled IGDB import/refresh, and remote-cover constraints.
- `docs/ARCHITECTURE.md` — optional external catalog provider boundary, generic external-ID relation, and local-operation independence.
- `AGENTS.md` — authoritative local catalog, no scraping, narrow abstractions, and current-roadmap sequencing.
