# 🌊 LANsurfer

**Find your crew. Catch the next game.**

**LANsurfer is back.**

Originally built for LAN parties in the early 2000s.<br>
Reimagined as an open-source **Looking for Group (LFG)** platform for gaming communities.

Self-hosted and built for LAN parties, gaming events, and the people who bring them to life.

Hundreds of players. Dozens of games. And somewhere in the room, someone else is thinking: *“Anyone up for Warcraft III?”*

**You just need to find each other.**

[The idea](#-what-is-lansurfer) · [How it works](#-how-it-works) · [Project status](#-project-status) · [Get involved](#-get-involved)

> 🚧 **Prototype:** the core player and administrator workflows are implemented and hardened for a controlled LAN-event pilot. Packaging and operator documentation for the first general self-hosting release are still in progress.

## 🎮 What is LANsurfer?

Maybe it's a quick round of Counter-Strike. Maybe it's FlatOut 2, an all-night OpenTTD session, or that obscure game you haven't launched in twenty years.

Chances are, someone brought it too.

LANsurfer aims to give everyone at the event a shared place to find out what others want to play, follow their favorite games, and get a group together. From Warcraft III and Age of Empires II to Trackmania, Quake III Arena, and Unreal Tournament — old favorites deserve another round.

The original use case is **CXT**, a LAN party with around **400 participants**. The goal is to make those spontaneous “let's play something” moments easier for LAN organizers and gaming communities everywhere.

## 🏄 How it works

The first prototype will focus on a simple browser-based flow:

1. **Follow your games.** Pick the games you'd like to play and find them again in My Games.
2. **Find players.** Create a Game Request with a start time, an optional player limit, and a short description.
3. **Join a game.** Browse requests, see who's still looking for players, and join in. Live updates keep player counts and new requests visible in the browser.

A request looks like this:

```text
Warcraft III
2 / 4 players · Starts now

“Casual 2v2 — getting back into WC3.
 Rusty players welcome 😄”

[ Join ]
```

Once the group is ready, it's time to launch the game.

**Follow games → Find players → Play together**

## 🌊 Built for your community

- **Your LAN, your instance.** Organizers can host LANsurfer on their own infrastructure and maintain the games available at their event.
- **A place for the classics.** The local game catalog can include the games your community actually plays, including the less obvious ones.
- **Easy to join in.** The first release targets desktop and mobile browsers, with quick, readable game requests.
- **Built in the open.** Players, organizers, designers, and developers can help shape the project.

## 🦦 Meet the LANsurfer Otter

<img src="docs/Otter02.png" alt="LANsurfer otter wearing a headset and holding a surfboard" width="280">

An otter with a headset and a surfboard feels right at home here: social, at ease in the water, and ready for the next game.

The otter brings a little personality to LANsurfer's ocean-inspired identity. In the application, the wording stays familiar: games, players, events, and joining a game.

The [visual branding guide](docs/BRANDING.md) defines the palette, typography, mascot usage, and dark/light interface direction for the first prototype.

## 🗺️ Project status

The **web-only v0.1 prototype** is ready for a controlled LAN-event pilot. It includes generic OIDC login, Event and game administration, game subscriptions, Game Requests, concurrency-safe joining, and live SSE updates on desktop and mobile browsers. The next milestone packages and documents it for a general open-source self-hosting release.

The [roadmap](docs/ROADMAP.md#progress) tracks completed work and what's next. The [product definition](docs/PRODUCT.md#5-v01-scope) describes the first release in detail.

Discord, Web Push, external game catalogs, and a separate Ready-to-Play status are possibilities for later. They are outside the first prototype. The design and database may still change during development.

## 🐳 Try the development foundation

With this repository checked out and Docker with Docker Compose installed, start LANsurfer and PostgreSQL:

```sh
docker compose up --build
```

This starts the application, including automatic database migrations and the responsive LANsurfer web UI. Open <http://localhost:8080> or check that the service is running:

```sh
curl http://localhost:8080/healthz
curl http://localhost:8080/readyz
```

Expect `ok` from `/healthz` and `ready` from `/readyz`. Stop the stack with:

```sh
docker compose down
```

### Configure OIDC login

LANsurfer uses generic OpenID Connect discovery and server-side PostgreSQL sessions. The local Compose environment defaults to the CXT development realm and client below. The provider must allow the exact callback URL `http://localhost:8080/auth/callback`:

```sh
LANSURFER_BASE_URL=http://localhost:8080
LANSURFER_OIDC_ISSUER=https://auth.convention-x-treme.de/realms/local-dev
LANSURFER_OIDC_CLIENT_ID=lansurver
# Leave empty for the local public PKCE client.
LANSURFER_OIDC_CLIENT_SECRET=
```

Set the issuer URL itself rather than its `/.well-known/openid-configuration` endpoint; LANsurfer discovers that endpoint automatically. The client secret is optional for public PKCE clients and required when the provider configures a confidential client. For an exposed deployment, use an HTTPS base URL; LANsurfer then marks its opaque session and login-flow cookies `Secure`. The identity provider subject is linked to a separate local LANsurfer user, and matching email addresses are never used to merge accounts.

For the local Keycloak client, enable **Standard flow**, leave **Client authentication** disabled, require the `S256` PKCE method, and configure `http://localhost:8080/auth/callback` as the exact valid redirect URI. A production server deployment should normally use a confidential client with client authentication enabled and provide its secret through `LANSURFER_OIDC_CLIENT_SECRET`.

When LANsurfer runs behind a reverse proxy, set `LANSURFER_BASE_URL` to the
public HTTPS origin. Set `LANSURFER_TRUSTED_PROXIES` to a comma-separated list
of the proxy IP addresses or CIDR ranges only when client-IP logging should use
`X-Forwarded-For` (for example, `10.20.0.5,10.21.0.0/16`). LANsurfer ignores
forwarded client addresses from every other peer; do not use a broad range that
includes untrusted clients. Proxy TLS headers do not control cookie security—the
public HTTPS `LANSURFER_BASE_URL` does.

### Promote the first administrator

Every new account starts with the `user` role. Log in once to create your local
account, then list the users in the running Compose stack:

```sh
task admin:users
```

Copy your user ID from the output and promote it:

```sh
task admin:promote USER_ID=2c359aee-7690-4975-a2f8-1730cf37f42b
```

Reload LANsurfer after the command succeeds. Existing sessions resolve the local
role on every request, so you do not need to log out. The same operations are
available directly from the binary as `lansurfer admin users` and
`lansurfer admin promote --user-id <UUID>` when
`LANSURFER_DATABASE_URL` is configured.

The Compose credentials are development defaults; do not use them for an exposed deployment. See the [contributing guide](CONTRIBUTING.md#development-setup) for local Go development, configuration, and checks.

## 🤝 Get involved

Have you organized a LAN? Spent half the evening trying to find a fourth player? We'd love your perspective.

You can help with interface design, development, documentation, bug reports, and testing at LAN events. Share a concrete use case in an [issue](https://github.com/convention-x-treme/lansurfer/issues), or start with the [contributing guide](CONTRIBUTING.md).

For the technical side, LANsurfer is a modular Go monolith with server-rendered pages and PostgreSQL. The [architecture](docs/ARCHITECTURE.md) explains the choices; [development rules](AGENTS.md) cover repository conventions and commands.

## 📜 License

LANsurfer is free and open-source software under the [GNU Affero General Public License v3.0 (AGPL-3.0)](LICENSE).

---

Sometimes the best game at a LAN is the twenty-year-old one you suddenly discover ten other people also want to play.

**Built for LAN parties. Built for gaming communities. Built in the open.**
