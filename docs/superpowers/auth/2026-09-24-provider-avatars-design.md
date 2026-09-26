# Provider-Backed User Avatars — Design

Issue: [#64](https://github.com/convention-x-treme/lansurfer/issues/64)
Branch: `feat/issue-64-provider-avatars`
Status: approved design, 2026-09-24

## 0. Approval gate

AGENTS.md says these changes need explicit approval:

- **1.0 scope.** PRODUCT §157 and ROADMAP §184 leave avatars out of 1.0 ("avatar uploads"). This design adds no uploads, but it does add a user-visible 1.0 feature. PRODUCT and ROADMAP get updated in the same PR.
- **Data ownership.** The avatar selection is new persistent user state. §3 puts it in `auth` (`user_identities`), not in `users.avatar_identity_id` as the issue suggests. The reasons are in §3.
- **Security model.** The server makes a new outbound fetch to URLs supplied by identity providers (§5). The existing IGDB cover-proxy core moves from `game` into `platform` (§5.1), so two modules share it.

GDPR constraint: a player's browser never contacts a Discord, Twitch, or OIDC host. Every avatar byte comes through LANsurfer, under the same policies already approved for IGDB covers in `docs/superpowers/igdb/2026-09-22-cover-proxy-design.md`.

## 1. Goal

Show one optional avatar per LANsurfer user. The image comes from one of the user's linked identities (Discord, Twitch, or generic OIDC), which the user picks explicitly. Browsers only contact LANsurfer. When no provider image is selected or it can't be loaded, show the existing local Game Nick initial.

Success criteria are the acceptance criteria in #64.

## 2. Decisions

| # | Decision |
|---|---|
| Owner | The `auth` module owns avatar metadata and the avatar selection. The `user` module stays unchanged. |
| Selection model | Add `user_identities.avatar_selected boolean`, with a partial unique index that allows at most one selected identity per user. No selected row means Default. |
| Stored metadata | Add `user_identities.avatar_ref text NULL`. Discord stores the avatar **hash**. Twitch and OIDC store the `picture` **https URL**. The stored value never reaches HTML. |
| Refresh | Every login or link through an identity refreshes that identity's `avatar_ref` (the existing `UpdateMetadata` path). The selection is never changed on refresh. |
| New account | During registration, the identity is created with `avatar_selected = (avatar_ref IS NOT NULL)`. |
| Link / login | Never changes `avatar_selected`. |
| Unlink | The selected row is deleted, so the user falls back to Default without any extra code. |
| Route | `GET/HEAD /media/avatar/{avatarKey}`. It is public, like `/media/igdb/*`. `avatarKey` is a content key (§5). The request never supplies an upstream URL. |
| Proxy mechanism | The same mechanism and limits as IGDB covers: disk-or-memory store, known-ID check on a cache miss, a combined 5 s deadline, a 10 min / 4,096-entry negative cache, `singleflight`, 8 cold-miss slots with `503`, 1 MiB, jpeg/png/webp, no redirects, and `immutable` caching. |
| Storage | Covers and avatars share the store instance. `LANSURFER_COVER_CACHE_DIR` set means a disk cache (under a `avatar/` bucket). Unset means the shared 64 MiB in-process LRU. |
| Fallback | The existing initial (`components.ViewerInitial`) is shown, and the existing `data-cover-fallback` image-error enhancement hides a broken `<img>`. |
| CSP | Unchanged: `img-src 'self' data:`. |
| Fallback initials | One initial, as `ViewerInitial` does today. Two-letter initials are skipped until the design asks for them. |

## 3. Why `auth` owns the selection

Using `users.avatar_identity_id` (the issue's example) would make `users`, a table owned by `user`, reference `user_identities`, a table owned by `auth`. Enforcing "the identity belongs to the same user" would then need either a composite foreign key `(avatar_identity_id, id) → user_identities(id, user_id)` or code that checks across modules. Unlink would also have to write to `users` (`ON DELETE SET NULL` on a composite key would also null out `id`, so that doesn't work).

A flag on `user_identities` avoids all of that:

- A row can only be selected for its own `user_id`, so the ownership rule holds by construction.
- `CREATE UNIQUE INDEX … ON user_identities (user_id) WHERE avatar_selected` guarantees at most one selection per user.
- Unlinking deletes the row, which removes the selection. The unlink concurrency lock stays unchanged.
- Changing the selection is one `auth` transaction: clear the old flag and set the new one. The partial unique index makes concurrent selects safe, because the loser gets a unique violation, which is retried once or surfaced as an error.

## 4. Provider metadata

`auth.Profile` and `auth.Identity` get `AvatarRef string`. Following the email convention, `normalizeProfile` trims it and drops it (sets it to `""`) when it is invalid. An avatar value never causes `ErrInvalidProfile`.

| Provider | Source | Stored `avatar_ref` | Validation |
|---|---|---|---|
| Discord | `avatar` field on `/users/@me` (scope `identify` is unchanged) | the hash | `^(a_)?[0-9a-f]{32}$` |
| Twitch | `picture` claim, requested through the existing `claims` parameter: `{"id_token":{"preferred_username":null,"picture":null}}`. No new scope. | URL | `https`, host `static-cdn.jtvnw.net`, ≤ 2048 chars |
| Generic OIDC | `picture` claim if present (the `profile` scope is already requested) | URL | `https`, ≤ 2048 chars, no userinfo, default port |

The Discord URL is built server-side from constants: `https://cdn.discordapp.com/avatars/{subject}/{hash}.png?size=128`. The `subject` is the already-validated Discord ID. Animated `a_` hashes are also requested as `.png`.

Persistence:

- Migration `00014_identity_avatars.sql`:
  - `user_identities` gets `avatar_ref text NULL CHECK (avatar_ref IS NULL OR char_length(avatar_ref) <= 2048)`.
  - `user_identities` gets `avatar_key text NULL CHECK (avatar_key IS NULL OR avatar_key ~ '^[a-f0-9]{32}$')`. `auth` computes it in Go as `hex(sha256(provider_kind|subject|avatar_ref))[:32]` whenever it writes `avatar_ref`. It is `NULL` exactly when `avatar_ref` is `NULL`.
  - `user_identities` gets `avatar_selected boolean NOT NULL DEFAULT false`, plus the partial unique index from §3.
  - Add `CREATE INDEX user_identities_avatar_key_idx ON user_identities (avatar_key) WHERE avatar_selected`. This index backs the known-ID check.
  - `auth_flows` gets `avatar_ref`, so a pending registration carries it the same way it carries `display_name` and `email`.
  - The Down migration drops these columns and indexes.
- `auth.sql` changes:
  - `CreateIdentity` and `UpdateIdentityMetadata` write `avatar_ref`.
  - The pending-flow insert and select carry `avatar_ref`.
  - `CreateIdentity` takes `avatar_selected`.
  - New queries:
    - `SelectAvatarIdentity(user_id, identity_id | NULL)` runs in one transaction and matches `id AND user_id`.
    - `SelectedAvatarByKey(avatar_key)` returns `(provider_kind, subject, avatar_ref)`, but only for a row where `avatar_selected` is true. This is the known-ID check.
    - Read models get `avatar_key` of the selected identity per user.
- `IdentityStore.UpdateMetadata` changes signature and takes the `Profile` instead of 5 strings. There are 3 callers, all in `resolution.go`.
- `app.AccountRegistration.Register` passes `AvatarRef` and sets `AvatarSelected: pending.AvatarRef != ""`.

## 5. Avatar delivery: same proxy as IGDB covers

### 5.1 Shared proxy core: `internal/game/cover` becomes `internal/platform/mediaproxy`

The cover `Service` (validation hook, store, negative cache, `singleflight`, semaphore, deadline) and `MemoryStore`/`DiskStore` hold no IGDB knowledge. Only `Catalog`, `Fetcher`, and size validation are IGDB-specific. Copying about 200 lines of concurrency code into `auth` would give two copies to keep in sync, so the core moves to `platform`, where cross-cutting infrastructure (`sse`, `postgres`) already lives.

- `mediaproxy.Service` is built with `NewService(store, fetcher, catalog, validBucket func(string) bool, logger)`. All other behavior and limits stay exactly as approved for covers:
  - ID regex `^[a-z0-9]{1,32}$`
  - 10 min / 4,096-entry negative cache
  - 8 slots, with `ErrBusy` when all are taken
  - combined 5 s catalog-and-fetch deadline
  - catalog errors are not negative-cached
  - store write errors are logged and the bytes are still returned
- The "size" parameter is renamed to "bucket". Covers use `small`/`big`. Avatars use `avatar`.
- `DiskStore` creates the bucket directories passed to it (`small`, `big`, `avatar`).
- The composition root builds **one** store (disk or 64 MiB memory) and **two** services: covers (IGDB catalog + fetcher) and avatars (`auth` catalog + avatar fetcher). Each service has its own negative cache and its own 8 slots, so a burst of avatar requests can't starve covers.
- `internal/game/igdb/cover.go` (fetcher) and the `CoverImageKnown` catalog query stay in `game`. `handler.Media` keeps its interface.

### 5.2 Avatar catalog and fetcher (`internal/auth`)

```
GET /media/avatar/{avatarKey}        web/handler/media.go (public)
  └─ avatars.Get(ctx, "avatar", avatarKey)      mediaproxy.Service
       ├─ key regex, bucket == "avatar"          → ErrNotFound
       ├─ store hit                              → bytes (no DB, no upstream)
       ├─ negative cache                         → ErrNotFound
       └─ singleflight, slot, 5 s deadline:
            Catalog: SelectedAvatarByKey          none → negative, 404
            Fetcher: URL built from stored row only
```

- `auth.AvatarSource` implements both `mediaproxy.Catalog` and `mediaproxy.Fetcher`, and both run inside the same 5 s shared deadline:
  - `Known(key)` runs `EXISTS … WHERE avatar_key = $1 AND avatar_selected`.
  - `Fetch(key)` loads the row through `SelectedAvatarByKey`, builds the URL, and fetches. A row that disappears between the two steps counts as a fetch failure, so it is negative-cached.
  - This is one extra indexed query per cold miss. It keeps the `mediaproxy` interfaces exactly as they are for covers.
- Upstream URLs:
  - Discord: `https://cdn.discordapp.com/avatars/{subject}/{hash}.png?size=128`
  - Twitch/OIDC: the stored `https` URL.
- Fetch rules are identical to `igdb.CoverFetcher`:
  - own `http.Client` with `Timeout: 5s`
  - `CheckRedirect` returns `http.ErrUseLastResponse`
  - status `200` only
  - `io.LimitReader` at 1 MiB + 1, where more than 1 MiB is a failure
  - `http.DetectContentType` must be `image/jpeg`, `image/png`, or `image/webp`
- Additions only because OIDC hosts are operator-configured and not fixed like IGDB:
  - A `net.Dialer.Control` hook rejects loopback, private, link-local, unspecified, and multicast IPs at dial time.
  - `Proxy: nil`.
  - No cookies.
  - https on the default port only.

### 5.3 HTTP (identical table to covers §4)

Public routes sit next to `/media/igdb/*`, outside the authenticated group:

| Case | Status | Headers |
|---|---|---|
| hit or successful fetch | `200` | sniffed `Content-Type`, `Cache-Control: public, max-age=31536000, immutable` |
| bad key, unknown or unselected key, negative hit, upstream failure | `404` | `no-store` |
| slots full | `503` | `no-store`, `Retry-After: 1` |
| catalog or store read error | `500` (logged) | `no-store` |

`immutable` is valid because `avatar_key` hashes `avatar_ref`. A new Discord hash or a new Twitch URL gives a new key and a new URL. A changed selection renders a different key. Known limit: a generic OIDC provider that changes the image behind a stable `picture` URL keeps serving the cached bytes until the operator clears the cache directory. This matches the cover rule of no disk eviction.

### 5.4 GDPR and privacy

- The browser only ever sees `/media/avatar/{32 hex}`.
  - The key is a one-way hash. It reveals no provider name, subject, username, or email.
  - Only avatars a user actively selected resolve, so unselected identities are never served.
- Bytes are cached on demand only, the same as covers. Nothing is bulk-downloaded.
- The global `Referrer-Policy: no-referrer` and `nosniff` headers stay.
- The CSP stays `img-src 'self' data:`. This is the enforcement: a direct provider URL would be blocked by the browser.
- On a cache miss the server contacts `cdn.discordapp.com`, `static-cdn.jtvnw.net`, or the OIDC host. The server's IP is disclosed to them, never the player's. PRODUCT's privacy text states this, as it does for IGDB.
- Unlinking the identity or switching to Default makes the key unknown to the catalog. Cached bytes stay under an unguessable key and are never linked from HTML again. Operators can delete the directory, as with covers.

## 6. Rendering

- Add a read-model field `AvatarKey string` (empty means Default) next to `GameNick` wherever an avatar is drawn today:
  - `components.Viewer` (layout)
  - the member list (`game_requests.templ:651`)
- The `gamerequest` query package joins `user_identities` read-only for the member and creator rows. Per ARCHITECTURE, query packages may join across modules.
- Add a `components.Avatar(gameNick, avatarKey string)` templ component:
  - With a key, it renders `<img src="/media/avatar/{key}" alt="" loading="lazy" data-cover-fallback="true">` with the initial as its hidden fallback sibling. This follows the existing `games.templ` cover markup.
  - Without one, it renders only the initial span.
  - It replaces the inline initial in the viewer panel and member list.
- The existing `data-cover-fallback` TypeScript already hides a failed image and shows its sibling fallback. Reuse it and don't add new JS. If its selector is cover-specific, broaden it.

## 7. Account UI

- The account page gets an "Avatar" fieldset:
  - It shows the current avatar (`components.Avatar`).
  - It lists one radio per linked identity. Identities without `avatar_ref` are disabled and labeled "No picture".
  - It has a "Default" radio.
  - The form posts to `POST /account/avatar` with field `identity` (an identity ID or `default`).
- The handler calls `auth.AvatarSelector.SelectAvatar(ctx, userID, identityID)`, then redirects with 303 to `/account`. `ErrIdentityNotFound` (foreign or unknown ID) returns 404, and nothing changes.
- Radios show only the provider label, as the identity rows already do. Owner-only metadata stays where it is today.

## 8. Tests

| Area | Level | Case |
|---|---|---|
| Discord mapping | unit (`discord_test.go`) | hash captured; `null`/invalid hash gives empty ref; login still succeeds |
| Twitch/OIDC mapping | unit (`oidc_test.go`) | `picture` kept; `http:`, wrong Twitch host, or oversized value dropped; claims param includes `picture`; scopes unchanged |
| Selection | integration | select own identity; foreign identity gives not found and no change; Default clears; concurrent selects leave exactly one |
| Link/login | integration | linking a second provider, or logging in through it, keeps the existing selection; refresh updates `avatar_ref` |
| Registration | integration | new account with an avatar is auto-selected; without one, Default |
| Unlink | integration | unlinking the selected identity gives Default |
| Proxy core | unit (`mediaproxy`) | the existing cover suite moves over unchanged (validation, hit, singleflight, delayed caller, admission, cancellation, deadline, negative cache, stores), plus an unknown bucket gives `ErrNotFound` with no store access |
| Avatar source | unit (`httptest`) | exact Discord URL; stored Twitch/OIDC URL; `302`/non-`200`/HTML/>1 MiB/slow upstream rejected; private-IP dial rejected |
| Avatar catalog | integration | `SelectedAvatarByKey` finds a selected key; an unselected identity's key, an unlinked identity's key, and an unknown key all return not found |
| Handler | unit (`router_test.go`) | anonymous `200` with `immutable`; `404`/`503` with `no-store`; rendered pages contain no `discordapp`, `jtvnw`, or OIDC host |
| Rendering | unit (templ) | no provider host in HTML; with no avatar, only the initial is rendered |

No browser test is needed, because the fallback reuses the existing `data-cover-fallback` behavior.

## 9. Docs in the same PR

- PRODUCT: avatar behavior and terminology, replacing "no avatar" in the 1.0 scope wording.
- ARCHITECTURE: `auth` owns avatar metadata and the selection; `platform/mediaproxy` is shared by covers and avatars; the proxy contract section covers both origins (fixed Discord/Twitch hosts, https-only OIDC with the private-IP guard).
- AGENTS.md: the cover-proxy guardrail is widened to "IGDB covers and selected provider avatars are served same-origin". It still forbids a generic remote-asset proxy.
- PRODUCT privacy/GDPR text: the server-side disclosure to the avatar hosts.
- The cover-proxy design doc gets a note that its core moved to `platform/mediaproxy`.
- ROADMAP: add the item.

## 10. Out of scope

- uploads, cropping, galleries
- third-party generated avatars
- avatar-specific cache eviction beyond clearing the cover cache directory
- two-letter initials
- automatic switching based on the last login
