# LANsurfer terminology

This file defines project vocabulary that is easy to confuse in code, documentation, issues, and agent prompts. Product behavior remains authoritative in [PRODUCT.md](../PRODUCT.md); technical ownership remains authoritative in [ARCHITECTURE.md](ARCHITECTURE.md); the design system remains authoritative in [DESIGN.md](../DESIGN.md).

Use these terms consistently. Do not invent synonyms for domain concepts unless the product definition changes.

## Core product terms

- **User**: the local LANsurfer account record. Authentication providers attach identities to this local User.
- **Player**: a person using LANsurfer in user-facing language. Do not use provider account data as public player identity.
- **Game Nick**: the player's unique, case-insensitive public LANsurfer identity.
- **Provider identity**: an OIDC, Discord, Twitch, or other authentication-provider identity linked to a local User. Provider display names, usernames, email addresses, and pictures are not the public LANsurfer identity unless a separate approved feature explicitly uses them.
- **Landing page**: the public logged-out `GET /`. Signed-in players never see it; they are redirected to onboarding or Game Requests. Not to be confused with Game Requests, the signed-in home of the product.
- **Game**: the authoritative local LANsurfer Game record.
- **Provider game / discovery result**: a provider-index result, such as an IGDB result, that is not yet necessarily an authoritative local Game.
- **Follow Game**: the user action represented by a `GameSubscription`.
- **Followed only**: the filter on the Games surface that restricts Games to those the current user follows.
- **My Games**: the Game Request discovery audience for requests whose Games the current user follows. It is not a replacement label for the Games screen's **Followed only** filter.
- **Game Request**: an instance-wide looking-for-group request for one Game. Its active lifecycle distinguishes a group still forming (`open`) from a session the Host explicitly started (`playing`).
- **Host**: the creator/owner of a Game Request for its lifetime. Host ownership is not transferable.
- **Playing**: the active Game Request state entered only by the Host's explicit **Start game** action. It is the authoritative signal that the session actually started; a playing request remains active until it is closed.
- **Edit Game Request**: the Host-only detail-page action for mutable coordination details. While `open`, it may change Max players, planned start, and description; while `playing`, only Max players and description remain editable. Game, Host, lifecycle state, and actual `started_at` are never editable through this flow.
- **Capacity / Max players**: the optional maximum number of members of a Game Request, the Host included. "Capacity" names the concept and "Max players" is the form field. A finite value is a whole number from `2` to `999`; a legacy request may still hold `1`. Only the Host changes it through **Edit Game Request**, while the request is `open` or `playing`, and never below the current member count.
- **Unlimited**: a Game Request with no maximum (stored as no value). The form accepts `0` or a blank field for it, and the UI shows it as `∞`, `unlimited`, or `open round`.
- **Full**: an active Game Request whose member count has reached its finite maximum. Joining is refused until a place frees up. A request is never Full while unlimited, and Full is derived, not stored.
- **Free place(s)**: the places left between the member count and the finite maximum. The **Free places** discovery filter keeps active requests that are unlimited or have at least one free place; `open` and `playing` requests are treated alike.
- **Game Request activity**: durable request-local history for successful create/join/leave/start/close/cancel/request-update changes. A request update is one Activity entry per successful Host Save, with typed before/after data for capacity and planned-start changes and only a changed marker for description. Activity is also the transactional handoff consumed by notification processing; it is not a generic audit log or global activity feed.
- **Lobby**: the participant-only detail-page view that combines Game Request activity and chat. Current members may read it in every lifecycle state; leaving revokes access. Only active requests allow chat posting. It shows the latest 200 combined entries oldest-first.
- **Notification work / delivery**: durable processing state for external notification transports. It is separate from Game Request activity.
- **Recently viewed**: local/browser presentation history. It is not Game Request activity or notification state.

- **Session information**: request-local generic key/value coordination data. While a Game Request is active, the Host and currently joined players can read it; only the Host can change it.
- **Secret session information**: an entry marked secret for presentation privacy only. It uses the same Host/currently joined participant audience as other session information.

## Integrations and presentation

- **Discord authentication**: Discord used as a login identity provider.
- **Discord notification delivery**: the separately configured instance-level incoming-webhook destination. Do not couple it to Discord authentication.
- **Instance / installation / deployment**: one running LANsurfer installation. The current product model treats the installation itself as the LAN party or gaming-event context.
- **Instance identity**: the small operator-configurable identity needed to distinguish an installation, such as its instance name.
- **Demo mode**: deployment setting (`LANSURFER_DEMO_MODE`) for a public demo instance: banner, self-service administrator rights, and disabled destructive admin actions. Not a separate product or module.
- **Demo reset**: the daily operator command `lansurfer admin demo reset` that empties a demo instance's database except the IGDB catalog, legal texts, and metrics key. Not account deletion and not a backup restore.
- **LANsurfer brand**: the fixed product brand, tokens, typography, themes, icons, and bundled brand assets, defined in [DESIGN.md](../DESIGN.md). Instance identity must not mutate it.
- **Event**: a legacy prototype domain concept. The current product model does not use Event selection or membership for Game Requests. Do not reintroduce an Event/context abstraction without an approved scope change.

## Brand voice boundary

LANsurfer branding may use surf-related visuals and marketing language, but product and domain terminology stays plain. Users must not learn a surf vocabulary to operate LANsurfer.

- Use Game, Game Request, Player, Game Nick, Join and Leave, Follow Game, and My Games.
- Do not use Wave, Crew, Spot, Space, or a generic Context as product or domain concepts. Do not rename Game Requests to Waves, Players to Crew, available places to Spots, or Join to Catch in navigation, controls, code, or persistence.
- Brand personality may appear in marketing headlines and occasional supporting copy. "Crew" may remain in marketing copy such as the tagline.
- Use explicit domain names in code and the database.
- Say **places** for available spots and **Max players** for the field. "Seat" is not used.

## Source-of-truth rule

- Durable product behavior, approved scope, and product boundaries: `PRODUCT.md`
- Durable technical decisions and ownership: `docs/ARCHITECTURE.md`
- Terminology: this file
- Design system: `DESIGN.md`; specific screen compositions and interactions: `docs/ui-kit/`
- Software-development work, priorities, and validation status: GitHub Issues and milestones. There is no repository roadmap file.
- Feature-branch specs/plans/tasks: temporary working artifacts; see `docs/superpowers/README.md`
