# LANsurfer Product

## 1. Product Summary

LANsurfer is an open-source, self-hosted Looking-for-Group application for LAN parties, gaming events, and gaming communities.

The first concrete use case is the CXT LAN party with approximately 400 participants.

At a large LAN event, the problem is usually not that there are too few players in the building.

The problem is discovering:

> Who wants to play the same game as me right now?

LANsurfer should answer that question with as little friction as possible.

---

## 2. Core Product Flow

A typical flow:

1. A user logs into LANsurfer.
2. The user follows games they are interested in.
3. The user wants to play Warcraft III.
4. They create a GameRequest.
5. Other users interested in Warcraft III see the new request immediately.
6. They join.
7. Once enough players are present, the group starts playing.

LANsurfer is not intended to be a complex matchmaking engine.

The first version should feel closer to:

> "Who wants to play this?"

than:

> "Find me the mathematically perfect teammate."

---

## 3. Product Principles

### Fast

Creating a request should take only a few seconds.

Joining should require very little interaction.

### Obvious

Users should understand the UI without learning LANsurfer-specific terminology.

### Local/community focused

LANsurfer should work especially well when users already share a physical event or gaming community.

### Self-hosted

LAN parties and clubs should be able to operate their own instance.

### Open

The project is intended as open-source software.

### Maintainable

Avoid product features that require disproportionate technical complexity before they are proven useful.

---

## 4. Terminology

LANsurfer branding may use surf-related visuals and marketing language, but product/domain terminology should remain simple.

Use:

- Game
- Event
- Game Request
- Players
- Join
- Leave
- Follow Game
- My Games

Avoid requiring users to learn terms such as:

- Wave
- Crew
- Spot

The product name provides the personality; the UI should prioritize clarity.

---

## 5. v0.1 Scope

The first prototype is **web-only**.

### Included

- OIDC login
- local LANsurfer users
- basic user/admin roles
- event support
- game catalog
- game aliases/search
- game subscriptions
- manual admin game management
- game requests
- joining/leaving game requests
- closing/cancelling requests
- server-side rendered pages
- HTMX interactions
- SSE live updates
- basic web UI feedback/toasts/highlighting
- Docker Compose self-hosting
- automatic database migrations

### Explicitly deferred

- Discord bot
- Discord notifications
- Discord login unless added as a separate later milestone
- Twitch
- Web Push
- mobile application
- native mobile notifications
- teams
- chat
- voice integration
- ranks/skill matchmaking
- ratings
- tournaments
- public federation
- configurable instance branding
- external game catalog integration
- persistent notification inbox
- complex moderation tools

---

## 6. Users and Authentication

LANsurfer maintains its own local User record.

Users authenticate through external identity providers.

For v0.1:

- generic OpenID Connect
- first real provider: the club's Keycloak

LANsurfer does not store local passwords.

A user's external provider identifier is not the LANsurfer user ID.

Future versions may allow multiple linked identities per user.

Possible future login methods include:

- Discord OAuth2
- passkeys
- temporary guest access

---

## 7. Roles

Keep roles intentionally minimal in v0.1.

Initial roles:

- `user`
- `admin`

A user can use normal LANsurfer features.

An admin can manage the local game catalog and required basic event/application data.

Do not build a generic permission system yet.

---

## 8. Event

An Event represents a LAN party or gaming event.

Example:

```text
CXT 2027
```

Suggested initial properties:

- ID
- Name
- StartsAt
- EndsAt
- active/current state
- CreatedAt
- UpdatedAt

A single LANsurfer instance may retain multiple Events historically.

The v0.1 UI may simplify this by primarily working with one current Event.

Event membership/participant lists are deferred unless required by the prototype.

---

## 9. Game

LANsurfer owns a local game catalog.

Suggested properties:

- ID
- Name
- Slug
- Active
- CreatedAt
- UpdatedAt

Examples:

```text
Warcraft III
Counter-Strike 1.6
Counter-Strike: Source
Counter-Strike 2
Age of Empires II
Trackmania
Quake III Arena
```

Do not model franchises/editions in a complex hierarchy.

If two versions represent different practical player pools, they may simply be separate Games.

---

## 10. Game Aliases

Games may have aliases to improve search.

Example:

```text
Warcraft III
- WC3
- Warcraft 3
- War3
```

Aliases should allow a user searching for `wc3` to find Warcraft III.

---

## 11. Game External IDs

The schema should support optional external metadata sources later.

Use a generic external-ID model:

```text
Game
  -> provider
  -> external_id
```

Example providers later:

- IGDB
- Steam

Do not build those integrations in v0.1.

The local catalog remains authoritative.

---

## 12. Game Administration

Admins should be able to:

- list games
- add a game
- edit a game
- activate/deactivate a game
- manage aliases

A future version may add:

```text
Admin -> Games -> Import from external catalog
```

External sources should import/enrich local records rather than become runtime dependencies.

---

## 13. GameSubscription

A GameSubscription represents:

> This user is interested in this game.

Suggested relation:

```text
user_id
game_id
created_at
```

A user should be able to follow/unfollow games easily.

In v0.1 this can be used for:

- personalized game lists
- highlighting relevant GameRequests
- SSE-triggered UI feedback when a relevant request appears

Future notification transports may also use subscriptions.

---

## 14. GameRequest

GameRequest is the primary LANsurfer concept.

Definition:

> A user is looking for people to play a particular game at an Event.

Suggested properties:

- ID
- EventID
- GameID
- CreatorID
- MaxPlayers
- StartsAt
- Description
- Status
- CreatedAt
- UpdatedAt

Initial statuses:

- `open`
- `closed`
- `cancelled`

Do not persist a `full` status if it can be derived from member count and `MaxPlayers`.

---

## 15. Recommended GameRequest Behavior

Unless implementation feedback suggests a clearly better model, use these defaults:

### Creator participates

The creator automatically becomes the first member.

Example:

```text
MaxPlayers = 4
creator creates request
current members = 1 / 4
```

### MaxPlayers can be optional

Some games do not have a useful hard player limit.

Examples:

- Trackmania
- OpenTTD
- Minecraft
- community servers

Allow a request without a maximum if it remains simple to implement.

### Requests can be scheduled

`StartsAt` supports:

- now
- in 15 minutes
- a specific time

Use proper timezone-aware timestamps in persistence.

### Creator ownership

For v0.1, do not implement ownership transfer.

A creator should close/cancel their request instead of leaving it as an ordinary member.

### Concurrent joins must be safe

If a request has a player limit, concurrent join requests must never overfill it.

This is a correctness requirement, not only a UI concern.

---

## 16. GameRequestMember

Represents participation in a GameRequest.

Suggested properties:

- GameRequestID
- UserID
- JoinedAt

A user must not be inserted twice into the same request.

Typical actions:

- Join
- Leave

Creator behavior may differ as described above.

---

## 17. GameRequest UI

The primary list should make the important information immediately visible.

Example:

```text
Warcraft III

2 / 4 players
Starts now
Casual
"Getting back into WC3, no pros please."

[ Join ]
```

Avoid forcing users to open a detail page for every basic decision.

Potential main action wording:

- Find Players
- Create Game Request
- Looking for Players

Prefer user-friendly wording even when the domain object is called `GameRequest`.

---

## 18. Live Updates

The v0.1 web application should update active GameRequests without requiring manual page refreshes.

Examples:

- a new request appears
- a player joins
- a player leaves
- a request closes

SSE is used to trigger/update this experience.

If a request appears for a game the current user follows, LANsurfer may show a small toast or visual highlight.

Persistent notifications are not required for v0.1.

---

## 19. Game Availability / Ready-to-Play

A separate "Ready to Play" availability model has been discussed but is intentionally deferred.

Possible future behavior:

```text
User is currently available for:
- Warcraft III
- Age of Empires II
- Quake III Arena
```

Do not add this model until the initial GameRequest workflow has been validated.

---

## 20. Branding

The product is called:

# LANsurfer

Current tagline:

> Find your crew. Catch the next game.

The product may use ocean/surf visual identity.

For v0.1 the branding is static and centralized.

Do not implement configurable branding yet.

Future branding is expected to be per LANsurfer instance.

---

## 21. Self-Hosting Experience

The desired first-run user experience is:

```bash
docker compose up -d
```

The installation should then:

1. start PostgreSQL
2. wait until PostgreSQL is available
3. automatically run database migrations
4. start LANsurfer
5. become ready for browser access

An operator should not need to manually run migration commands for normal installation or upgrades.

---

## 22. Product Success Criteria for the Prototype

The prototype should be considered successful if it can be tested at a LAN/event and users can reliably:

1. authenticate
2. find games
3. follow games
4. create a GameRequest quickly
5. see new requests without manually refreshing
6. join/leave requests
7. see accurate player counts
8. avoid overfilled requests
9. use the product from desktop and mobile browsers
10. operate the application through a simple self-hosted deployment

The prototype does not need every future integration to prove the core concept.

---

## 23. Product Decisions Intentionally Left to Implementation

Codex may make reasonable implementation choices for details such as:

- exact form layout
- exact empty-state copy
- exact pagination style
- precise admin route layout
- exact internal validation helper structure
- whether a simple list or search-first UI is better for small screens

Codex should not independently change:

- the primary domain terms
- the web-only v0.1 scope
- local PostgreSQL as the game catalog source of truth
- OIDC/no-password authentication
- GameRequest as the main LFG concept
- self-hosting simplicity as a primary product requirement
