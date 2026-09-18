# IGDB Discovery UX Refinement Design

**Date:** 2026-09-18  
**Status:** Approved design; implementation plan pending user review  
**Scope:** Refinement of the existing Track C IGDB implementation in PR #36  
**Related design:** `docs/superpowers/specs/2026-09-17-igdb-discovery-design.md`

## 1. Problem

The current IGDB implementation is technically correct but exposes internal catalog mechanics to players.

On a fresh instance, `/games` is empty until a local Game has been materialized, even when a complete IGDB index already exists. A player must infer that search is required before provider results appear.

Provider search results are richer than local Games: an IGDB result can show a cover, type, platform, and release year, but after the player follows it the materialized local Game is rendered through the older local read model and loses that enrichment. The UI also exposes the internal state label `Not yet in LANsurfer catalog`.

The Game Request composer has a similar discontinuity. A provider result can link into the composer, but the selected Game is represented only as `Selected from IGDB search results.`; the general `Create Game Request` flow only offers already-local Games.

The refinement must keep the important architectural guarantees from the original Track C design:

- the local PostgreSQL Game catalog remains authoritative domain state;
- IGDB remains optional discovery/enrichment infrastructure;
- player-facing requests never call IGDB live;
- provider/external ID remains the only identity bridge;
- there is no name- or slug-based auto-merge;
- materialization remains a deliberate side effect of a real player action;
- no Redis, broker, SPA, generic provider framework, or additional runtime service is introduced.

## 2. Product model

### 2.1 Games is discovery-first

`/games` is the central place where a player finds Games.

Players should not need to understand whether a Game is already materialized locally or still exists only in the synchronized IGDB index.

The player-facing mental model is:

```text
Games
├─ no search text
│  ├─ Find your game
│  └─ Games on this LAN
└─ search >= 2 characters
   └─ matching Games
```

Materialization is an implementation detail and must not be represented as a player-facing status.

### 2.2 Local Game remains authoritative

A materialized local Game owns the domain fields:

- name;
- slug;
- active state;
- local aliases.

A later IGDB refresh must not rename the local Game, replace its slug, or rewrite its local aliases.

### 2.3 IGDB becomes durable enrichment

A materialized Game may have a local last-known-good IGDB enrichment snapshot containing:

- `cover_image_id`;
- `game_type`;
- `platform_names`;
- `first_release_date`;
- `provider_updated_at`.

The enrichment snapshot does **not** duplicate local name, slug, or aliases.

Provider alternative names remain one-time materialization input for local aliases, as in the current implementation. Later provider alias changes do not automatically rewrite local aliases.

## 3. Games page

### 3.1 Empty search state

An empty search must not render `0 games` / `No games found` as the primary experience.

Instead it renders a discovery-first state with a prominent search field and copy equivalent to:

> Find your game  
> Search the game catalog to follow something you play or start a Game Request.

Below the search, render **Games on this LAN** when active local Games exist.

`Games on this LAN` contains every active local Game, regardless of whether it was manually created or materialized from IGDB.

Sort this section:

1. Games followed by the current player;
2. remaining Games alphabetically.

If no local Games exist and provider discovery is available, keep the discovery-first search state without a misleading zero-count catalog message.

If no local Games exist and provider discovery is not usable yet, render the explicit availability state described in section 8.

### 3.2 Live search

The search field uses server-rendered HTMX live search:

- debounce approximately 300 ms;
- search starts at 2 characters;
- keep the normal submit button as the no-JavaScript fallback;
- update/replace the URL rather than pushing a history entry for every pause;
- clearing the search returns immediately to the no-search discovery state.

With exactly one non-whitespace character, render a small prompt equivalent to:

> Type at least 2 characters to search.

Do not execute the broad provider search for a one-character term.

### 3.3 Search scope

A non-empty search searches the same player-facing discovery model for:

- active local Games;
- active-generation IGDB provider records.

There is no separate "local search" and "IGDB search" experience in the player UI.

The query remains PostgreSQL-only.

### 3.4 Search ranking

Rank search results by match relevance:

1. exact normalized name or alias match;
2. name or alias starts with the search term;
3. name or alias contains the search term.

Within the same relevance class:

1. Games followed by the current player;
2. Games already materialized locally;
3. alphabetical name;
4. stable identity tie-breaker.

The local/materialized preference is only a tie-breaker. It must not cause a worse textual match to outrank a better textual match.

### 3.5 Result limit

Keep discovery bounded to at most 50 rendered results for one query. The repository/read model must also return an explicit `has_more`/truncated signal, for example by querying at most 51 rows and rendering only the first 50. Do not infer truncation merely because exactly 50 rows were returned.

Do not add pagination or infinite scrolling in this refinement.

When that explicit signal indicates that the search is too broad, render guidance equivalent to:

> Too many matches — refine your search.

The page is a search/discovery surface, not a general-purpose IGDB browser.

## 4. Unified Game presentation

### 4.1 One player-facing Game card

Materialized and unmaterialized discovery results use the same visual Game-card concept.

A card can render:

- cover image when available;
- abbreviation tile fallback when no cover is available;
- Game name;
- game type;
- platforms;
- release year;
- `Find Players`;
- `Follow Game` or `Following`.

The player-facing card must not render:

- `Not yet in LANsurfer catalog`;
- `materialized`;
- provider-record terminology;
- sync-generation terminology;
- `Local` versus `IGDB` origin badges.

A player may need the metadata to distinguish same-name Games, but should not need the storage origin.

### 4.2 Same-name Games

Different provider IDs and different local Games may share the same display name.

No name-based merge is introduced.

Disambiguate naturally with available presentation metadata:

- cover;
- game type;
- platform;
- release year.

A manually-created same-name Game with no enrichment may remain less richly described and use the abbreviation fallback. Do not add technical IDs or require a new local metadata editor in this refinement.

### 4.3 Follow transition

Following an unmaterialized provider result may still materialize the Game internally.

After success, the card stays visually stable. Only the follow control changes to `Following`; `Find Players` remains available.

The materialization transition must not degrade the Game from a rich card to a local abbreviation-only card.

### 4.4 Find Players and Follow are independent

`Find Players` is available for any discoverable Game, whether or not the current player follows it.

Creating a Game Request does not automatically follow the Game.

Follow means long-lived player interest. Find Players means current intent to play.

Unfollow only removes the player's subscription. It never deletes, dematerializes, or removes enrichment from the local Game.

## 5. Enrichment persistence

### 5.1 Storage boundary

Add IGDB-specific local enrichment storage associated with a materialized local Game. A dedicated IGDB metadata table is preferred over adding provider-specific columns directly to `games`.

The table should be keyed so that at most one current IGDB enrichment snapshot belongs to the linked local Game/provider identity.

It stores only the five enrichment fields from section 2.3 plus keys/timestamps required for persistence.

This is deliberately **not** a generic provider metadata framework.

### 5.2 Materialization

When an IGDB provider result is first materialized:

1. create or reuse the authoritative local Game using the existing provider/external-ID identity rules;
2. preserve the existing local name/slug/alias behavior;
3. persist the current IGDB enrichment snapshot;
4. perform the requested action, such as Follow or Game Request creation.

Concurrent materialization must retain the existing convergence guarantee on provider + external ID.

### 5.3 Sync updates

A successful IGDB generation activation updates enrichment for materialized provider identities that are present in the new generation.

For each enrichment field:

- a new non-empty/non-null provider value replaces the stored value;
- an empty/null provider value does not erase an existing stored value.

This is a **last-known-good** enrichment policy.

If a provider record disappears, is archived, or IGDB is disabled, the existing local enrichment snapshot remains.

If a previously missing provider record later reappears, successful syncs resume normal enrichment updates.

Deactivating a local Game also retains its enrichment so reactivation immediately restores the same presentation.

### 5.4 Player reads

For an already materialized local Game, player-facing read models use:

```text
local Game domain fields + local enrichment snapshot
```

They do not switch back and forth between snapshot metadata and the currently active IGDB generation.

For an unmaterialized discovery result, the active IGDB generation supplies the presentation metadata.

This gives a stable boundary:

```text
unmaterialized -> active provider index
materialized   -> local Game + local enrichment
```

## 6. Read-model consistency

Introduce or extend player-facing Game projections so the same presentation data can be used consistently without making each web page know how to join IGDB storage.

The reusable projection should be able to represent, where relevant:

- local Game ID;
- optional provider reference for unmaterialized discovery;
- name;
- slug when local;
- cover image ID;
- game type;
- platform names;
- first release date;
- followed state;
- materialized/local state internally.

The internal materialized flag may remain useful to application code but is not rendered as player-facing status.

Use this enriched projection across:

- `/games`;
- `/games/mine`;
- Game Request composer selection;
- Game Request list;
- Game Request detail.

Trending remains compact and text-based; do not add covers to the Trending rail in this refinement.

## 7. My Games

`My Games` continues to mean only Games the current player explicitly follows.

Do not implicitly include a Game because the player:

- created a Game Request for it;
- joined a Game Request for it;
- interacted with it in another way.

Render followed Games using the same enriched Game presentation as elsewhere, including the retained cover when one is known.

## 8. Provider availability states

Technical provider state remains an administration concern.

### 8.1 IGDB disabled

Player discovery continues to search and show local Games normally.

Already persisted enrichment remains visible.

Do not hide retained covers just because IGDB is currently disabled.

### 8.2 Initial synchronization in progress

Local Games remain usable.

When no active provider baseline exists yet, player-facing copy may say:

> Game discovery is getting ready.

Do not expose sync-generation or token details.

### 8.3 Initial provider unavailable and no local Games

Render:

> No games are available yet.

For an administrator, include a route/link to the relevant Game/IGDB administration surface.

A normal player sees only the availability message.

### 8.4 Refresh failure after a successful baseline

Continue using the last successful active provider generation for unmaterialized discovery.

Continue using local enrichment snapshots for materialized Games.

Do not show the provider refresh failure to ordinary players. Detailed status and error information remains in administration.

## 9. Game Request composer

### 9.1 Discovery-first creation flow

The global `Create Game Request` flow must no longer depend on an already-materialized local Game.

The composer flow becomes:

```text
Create Game Request
        ↓
Find your game
        ↓
shared Game discovery
        ↓
select Game
        ↓
selected Game card + Change game
        ↓
request details
        ↓
Create
```

The composer uses the **same** discovery semantics as `/games`:

- same PostgreSQL-backed search;
- same relevance ordering;
- same 2-character threshold;
- same covers and disambiguation metadata;
- same local + provider result model;
- no live IGDB requests;
- no materialization merely from searching or selecting.

Before the first search, render the discovery state and `Games on this LAN` when available.

If neither local Games nor provider discovery are available, use the same `No games are available yet` state as section 8.

### 9.2 Game-first form structure

Game selection is the first step.

After selection, display a compact selected-Game card with:

- cover/abbreviation;
- name;
- available type/platform/year context;
- `Change game`.

Then display/edit:

- start time;
- maximum players;
- description.

Changing the selected Game must preserve already-entered request details.

### 9.3 Side-effect boundary

GET, live search, and Game selection remain read-only.

An unmaterialized provider Game is materialized only during the final successful Create Game Request POST, after normal request-field validation has passed.

This preserves the existing rule that invalid request input causes no provider materialization side effect.

If materialization succeeds but later request creation fails for another reason, the local Game is allowed to remain materialized. Do not attempt compensating deletion.

### 9.4 Provider record disappears before submit

If the chosen provider record is no longer resolvable when the final request is submitted:

- do not fabricate a Game from browser-supplied metadata;
- do not materialize from stale form values;
- preserve the other entered form fields;
- keep the user in the composer;
- render a clear message equivalent to:

> This game is no longer available from the game catalog. Choose another game.

Provide `Change game` from that state.

## 10. Game Request presentation

Extend the Game Request read model so request list/detail rendering can use the Game's local enrichment snapshot.

The request list uses a small cover where available, with the abbreviation tile as fallback.

The request detail and composer may use the same cover/enrichment context at their appropriate sizes.

The Trending rail remains text-only.

The Game Request domain itself does not own IGDB data. This is a read-model join/projection concern.

## 11. Manual local Games and future linking

A manually-created local Game and an IGDB-backed Game with the same name remain distinct.

Do not introduce automatic linking or merging by name, slug, alias, or fuzzy similarity.

A future administrator workflow may explicitly link an existing local Game to an IGDB provider identity, but that is outside this refinement.

Ordinary players never resolve such ambiguity by choosing a local merge target.

## 12. Error handling

Player-facing errors should describe the action the player can take, not provider internals.

Examples:

- search too short: `Type at least 2 characters to search.`
- too many matches: `Too many matches — refine your search.`
- provider baseline not ready: `Game discovery is getting ready.`
- no usable catalog at all: `No games are available yet.`
- selected provider Game disappeared before request submit: `This game is no longer available from the game catalog. Choose another game.`

Provider HTTP errors, sync generation failures, credentials, and last-error diagnostics remain admin/log concerns.

## 13. Implementation boundaries

This refinement may change:

- Game discovery service/repository contracts;
- PostgreSQL schema and sqlc queries;
- local Game subscription/discovery projections;
- IGDB sync activation/update behavior;
- Game Request read models;
- Games templates/handlers;
- Game Request composer templates/handlers;
- related tests and generated templ/sqlc output.

It must not introduce:

- live player-facing IGDB requests;
- generic provider abstractions beyond what already exists;
- a Game detail page;
- player free-form Game creation;
- provider-origin badges as required player UI;
- provider-driven local renaming;
- recurring alias synchronization;
- pagination/infinite scroll for provider discovery;
- automatic Game Follow when creating a Game Request;
- deletion/dematerialization on Unfollow.

## 14. Testing requirements

### 14.1 Discovery service/repository

Cover:

- empty search returns local Games enriched from local snapshots;
- followed local Games sort first on the no-search LAN list;
- one-character search does not execute provider discovery;
- exact/prefix/contains relevance ordering;
- followed/materialized/alphabetical tie-breaking within the same relevance class;
- result bound and broad-search indication;
- materialized results are represented once;
- unmaterialized visible provider results remain searchable;
- player reads never require live IGDB.

### 14.2 Enrichment persistence

Cover:

- initial materialization writes enrichment;
- successful later sync updates non-empty values;
- empty later values preserve previous non-empty snapshot fields;
- provider disappearance preserves snapshot;
- provider reappearance resumes updates;
- IGDB disabled does not hide snapshot;
- local Game deactivation/reactivation preserves snapshot;
- concurrent materialization still converges on one local Game and one enrichment record.

### 14.3 Games UI

Cover:

- fresh usable provider instance renders discovery-first state, not `0 games`;
- local LAN section renders without a search;
- live search starts at the defined threshold;
- clearing search restores the start state;
- provider and materialized cards share the same presentation;
- following a provider result keeps its cover/metadata and changes only follow state;
- `Not yet in LANsurfer catalog` is absent;
- `Find Players` is available independently of Follow;
- same-name Games remain distinct and show available disambiguation metadata;
- local no-cover fallback still renders.

### 14.4 Game Request composer

Cover:

- global create flow can discover an unmaterialized IGDB Game;
- composer discovery uses the same ordering/threshold as `/games`;
- selection itself does not materialize;
- selected Game renders name and enrichment;
- changing Game preserves other request fields;
- final valid POST materializes before request creation;
- invalid request fields do not materialize;
- request creation failure may leave the materialized Game;
- missing provider record on final submit preserves form input and returns actionable copy.

### 14.5 Game Request list/detail

Cover:

- request read models expose local enrichment;
- cover renders when known;
- abbreviation fallback renders when no cover exists;
- Trending remains text-only;
- following state semantics remain unchanged.

### 14.6 Regression verification

Run the repository's normal generated-code and verification workflow:

```bash
task generate
task lint
task test
task test:integration
task build
```

The final implementation review must also confirm that no player-facing handler calls Twitch/IGDB directly and that the local Game remains authoritative for name/slug/aliases.

## 15. Explicitly deferred

The following are intentionally not solved here:

- a full IGDB browsing experience;
- pagination/infinite scroll;
- Game detail pages;
- free-form player Game creation;
- administrator UI for linking an existing local Game to an IGDB ID;
- local metadata editing solely to enrich manual Games;
- automatic merge of same-name Games;
- generalized multi-provider enrichment architecture;
- covers in Trending.

These require separate product decisions if they become necessary.
