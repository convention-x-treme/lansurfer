# LANsurfer terminology

This file defines project vocabulary that is easy to confuse in code, documentation, issues, and agent prompts. Product behavior remains authoritative in [PRODUCT.md](PRODUCT.md); technical ownership remains authoritative in [ARCHITECTURE.md](ARCHITECTURE.md).

Use these terms consistently. Do not invent synonyms for domain concepts unless the product definition changes.

## Core product terms

- **User**: the local LANsurfer account record. Authentication providers attach identities to this local User.
- **Player**: a person using LANsurfer in user-facing language. Do not use provider account data as public player identity.
- **Game Nick**: the player's unique, case-insensitive public LANsurfer identity.
- **Provider identity**: an OIDC, Discord, Twitch, or other authentication-provider identity linked to a local User. Provider display names, usernames, email addresses, and pictures are not the public LANsurfer identity unless a separate approved feature explicitly uses them.
- **Game**: the authoritative local LANsurfer Game record.
- **Provider game / discovery result**: a provider-index result, such as an IGDB result, that is not yet necessarily an authoritative local Game.
- **Follow Game**: the user action represented by a `GameSubscription`.
- **Followed only**: the filter on the Games surface that restricts Games to those the current user follows.
- **My Games**: the Game Request discovery audience for requests whose Games the current user follows. It is not a replacement label for the Games screen's **Followed only** filter.
- **Game Request**: an instance-wide looking-for-group request for one Game. Its active lifecycle distinguishes a group still forming (`open`) from a session the Host explicitly started (`playing`).
- **Host**: the creator/owner of a Game Request for its lifetime. Host ownership is not transferable in the stable 1.0 model.
- **Playing**: the active Game Request state entered only by the Host's explicit **Start game** action. It is the authoritative signal that the session actually started; a playing request remains active until it is closed.
- **Capacity / Max players**: the optional maximum number of members of a Game Request, the Host included. "Capacity" names the concept and "Max players" is the form field. A finite value is a whole number from `2` to `999`; a legacy request may still hold `1`. Only the Host changes it, while the request is `open` or `playing`, and never below the current member count.
- **Unlimited**: a Game Request with no maximum (stored as no value). The form accepts `0` or a blank field for it, and the UI shows it as `∞`, `unlimited`, or `open round`.
- **Full**: an active Game Request whose member count has reached its finite maximum. Joining is refused until a place frees up. A request is never Full while unlimited, and Full is derived, not stored.
- **Free place(s)**: the places left between the member count and the finite maximum. The **Free places** discovery filter keeps active requests that are unlimited or have at least one free place; `open` and `playing` requests are treated alike.
- **Game Request activity**: durable request-local history for successful create/join/leave/start/close/cancel/capacity-change changes. It is also the transactional handoff consumed by notification processing; it is not a generic audit log or global activity feed.
- **Notification work / delivery**: durable processing state for external notification transports. It is separate from Game Request activity.
- **Recently viewed**: local/browser presentation history. It is not Game Request activity or notification state.

## Integrations and presentation

- **Discord authentication**: Discord used as a login identity provider.
- **Discord notification delivery**: the separately configured instance-level incoming-webhook destination. Do not couple it to Discord authentication.
- **Instance / installation / deployment**: one running LANsurfer installation. The current product model treats the installation itself as the LAN party or gaming-event context.
- **Instance identity**: the small operator-configurable identity needed to distinguish an installation, such as its instance name.
- **LANsurfer brand**: the fixed product brand, tokens, typography, themes, icons, and bundled brand assets. Instance identity must not mutate it.
- **Event**: a legacy prototype domain concept. The current product model does not use Event selection or membership for Game Requests. Do not reintroduce an Event/context abstraction without an approved scope change.

## Planning vocabulary

The historical **Track A–G** names were planning labels used during the pre-1.0 build-out. They are deprecated for new code, durable documentation, and new issue titles.

Use capability language instead, for example:

- Game Request core/discovery/activity
- authentication and Game Nick
- Game catalog and IGDB discovery
- PWA and instance identity
- notifications and delivery
- distribution and operations
- brand and UI system

Historical PRs, commits, retained design records, and closed issues do not need to be rewritten merely to remove Track terminology.

## Source-of-truth rule

- Durable product behavior and release scope: `docs/PRODUCT.md`
- Durable technical decisions and ownership: `docs/ARCHITECTURE.md`
- Terminology: this file
- Brand/UI rules: `docs/BRANDING.md` and `docs/ui-kit/`
- Current work, priorities, validation status, and release gates: GitHub Issues and the active release milestone
- Feature-branch specs/plans/tasks: temporary working artifacts; see `docs/superpowers/README.md`
