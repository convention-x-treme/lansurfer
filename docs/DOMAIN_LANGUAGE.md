# LANsurfer Domain Language

This document defines the canonical language used for LANsurfer product concepts.

Use these terms consistently in product discussion, feature designs, implementation plans, code, database names, tests, and user-facing copy where the same concept is meant. Prefer the established term over a synonym.

This is a domain-language reference, not a technical glossary. Frameworks, libraries, HTTP concepts, database implementation details, and project-management labels do not belong here unless they carry LANsurfer-specific meaning.

When the model changes, update this document together with the canonical product or architecture document that owns the behavior.

## Language rules

- Use one canonical term for one domain concept.
- Do not introduce a synonym merely because it sounds nicer in one feature.
- Use explicit domain names in code; surf metaphors are branding/marketing language only.
- A UI may use natural grammar around a canonical concept, but it must not imply a different model.
- Project-management labels such as Track A–G are not domain terms and must not appear in new domain models or APIs.
- Historical/legacy terms may remain in migrations and historical documents when changing them would destroy useful context; do not reuse them for new 1.0 behavior.

## Core concepts

### Instance

One running LANsurfer installation.

For stable 1.0, an Instance itself supplies the shared LAN party / gaming-event context. Players do not select or join a separate Event before using core flows.

**Do not use as synonyms:** Event, Space, Community, Context.

### Instance identity

The minimum configurable display identity that distinguishes one Instance from another.

It is separate from the fixed LANsurfer product brand. In 1.0 this is intentionally narrow and must not grow into a general branding/custom-theme model.

**Not the same as:** LANsurfer brand, theme.

### User

LANsurfer's local account/security principal.

A User has a local LANsurfer ID and may have one or more linked authentication Identities. Provider accounts do not replace the local User.

**Not the same as:** Player, Identity, Game Nick.

### Identity

An external authentication identity linked to a User, such as one OIDC identity or one Discord identity.

Identity data authenticates an account. Provider display names and email addresses are account/private data and are not LANsurfer public player identity.

**Not the same as:** User, Player, Game Nick.

When ambiguity is possible, prefer **authentication identity**.

### Player

A User participating in player-facing LANsurfer flows with a configured Game Nick.

Use Player when talking about people in Games/Game Requests and other public gameplay-facing behavior. Use User when talking about account/authentication/security ownership.

### Game Nick

The unique, case-insensitive public LANsurfer name of a Player.

Game Nick is the public identity used in Game Request membership/activity and other player-facing references. Provider usernames, display names, subjects, and emails are not Game Nicks.

**Not the same as:** Discord display name, OIDC name, provider identity.

### Administrator

A User with LANsurfer's global administrator role.

Use **administrator** or **admin** for this role. Do not imply Event- or community-scoped management roles in stable 1.0.

## Game catalog concepts

### Game

An authoritative local LANsurfer Game domain object.

Core writes such as following or creating a Game Request ultimately refer to a local Game. Provider data may help discover or enrich a Game but is not authoritative domain state.

**Not the same as:** Provider Game, IGDB record.

### Provider Game

A read-only game record offered by an external catalog/discovery provider.

A Provider Game may be shown in discovery before a corresponding local Game exists. It is identified by provider + external ID.

Currently IGDB is the only approved catalog provider, but use **Provider Game** where the concept is provider-neutral and **IGDB record** only where behavior is IGDB-specific.

**Not the same as:** Game.

### Materialization

The concurrency-safe resolution of a Provider Game reference into one authoritative local Game when a write needs a local Game.

Materialization is keyed by provider + external ID and must never merge Games by name, slug, alias, or fuzzy similarity.

Materialization is not the same operation as provider synchronization.

### Enrichment

Last-known-good provider presentation metadata attached to an authoritative local Game, such as cover image ID, game type, platform names, or release date.

Enrichment may improve presentation but never replaces local Game authority such as local ID, name, slug, active state, or aliases.

### Follow Game

The Player action that records interest in one Game.

Use **Following** for the state/relationship and **My Games** for the player-facing collection derived from followed Games.

Do not introduce parallel terms such as subscription/favorite in product language. Internal persistence names may remain historical where changing them has no value.

## Game Request concepts

### Game Request

An instance-wide invitation to play one Game at a planned time.

A Game Request is not scoped to an Event, Clan, lobby, chat room, or generic parent/context in stable 1.0.

Use the explicit code/domain name `GameRequest`.

### Creator

The Player who creates a Game Request.

Creator remains a Player; it is a role in relation to one Game Request, not a separate account type.

### Game Request Member

A Player who currently belongs to a Game Request.

Use `GameRequestMember` in code when referring to the membership record/concept. User-facing copy may naturally say players or participants when that does not imply a different domain model.

### Join / Leave

The explicit actions that add/remove a Player's membership in a Game Request.

Use **Join** and **Leave** in product language rather than project-specific metaphors.

### Activity

Durable history of successful Game Request state changes such as create, join, leave, close, or cancel.

Activity is written consistently with the successful domain change and can also serve as durable notification intent where defined by ARCHITECTURE.

**Not the same as:** page/view history, realtime SSE event, notification delivery.

When ambiguity is possible, prefer **Game Request activity**.

## Notification concepts

### Notification

An asynchronous external delivery derived from eligible durable LANsurfer state/activity.

Stable 1.0 notification transports are personal Web Push and one instance-level Discord destination. Delivery/retry state belongs to notification behavior and must not determine whether the originating Game Request change succeeds.

**Not the same as:** Game Request activity, SSE/realtime update, persistent in-app inbox.

### Realtime update

An ephemeral in-browser update delivered through the supported realtime mechanism.

A realtime update improves live browser behavior but is not durable notification state and is not a replayable source of truth.

## Provider language

### Authentication provider

An external service used to authenticate an Identity, currently generic OIDC or Discord.

### Catalog provider

An external service used for Game discovery/enrichment, currently IGDB.

Use the qualified term when **provider** by itself could be ambiguous.

## Terms deliberately outside the stable 1.0 model

### Event

A legacy/prototype and possible future product concept.

Stable 1.0 does not require an Event domain object for Game Request flows; the Instance supplies the event context. Event may still appear in migration/history discussions.

Do not introduce new 1.0 dependencies on Event unless product scope changes.

### Clan

A post-1.0 candidate. It is not part of the current stable 1.0 domain model.

### Context

Do not use **Context** as a generic Game Request parent/domain abstraction.

The word may still appear in ordinary technical meanings such as Go `context.Context`; this rule concerns the product/domain model.

### Track A–G

Delivery/project-management labels used by the 1.0 roadmap.

They are not domain concepts. New product behavior, architecture contracts, code, and feature specs should describe the actual concepts involved instead, for example **Game Request → Notification contract** or **service worker → Web Push integration**.
