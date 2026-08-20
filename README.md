# 🌊 LANsurfer

**Find your crew. Catch the next game.**

LANsurfer is an open-source **Looking for Group (LFG)** platform built for LAN parties, gaming events, and gaming communities.

Instead of wondering who else might be interested in playing a specific game, simply start a **Wave**.

Players following that game can be notified, join the Wave, and start playing.

> 🎮 Someone wants to play Warcraft III.
> 🌊 They start a Wave.
> 🔔 Interested players get notified.
> 👥 The crew comes together.
> 🚀 Game on.

---

## 🌊 What is LANsurfer?

Finding players at a LAN party sounds easy — until hundreds of people are sitting in the same venue playing dozens or even hundreds of different games.

You might want to play:

* Warcraft III
* Age of Empires II
* Counter-Strike 2
* Trackmania
* Quake III Arena
* Unreal Tournament
* OpenTTD
* or some obscure game from 20 years ago

Chances are, someone else wants to play it too.

**You just need to find each other.**

That's what LANsurfer is for.

LANsurfer provides a central place where players can discover what others want to play, subscribe to their favorite games, and quickly form groups.

---

## 🏄 How it works

LANsurfer uses a simple concept:

### 🌊 Waves

A **Wave** is an LFG request.

For example:

```text
Warcraft III
2 / 4 players

Mode: 2v2
Style: Casual
Starts: Now

"Getting back into WC3 — no pros please 😄"

[ Join Wave ]
```

Creating a Wave should take only a few seconds.

---

### 👥 Crew

Players joining a Wave become the **Crew**.

Once enough players have joined, it's time to play.

No complicated matchmaking.

No endless Discord channel scrolling.

Just:

**Start Wave → Find Players → Play**

---

### 🔔 Follow Games

Players can follow games they are interested in.

```text
Warcraft III          🔔 Following
Age of Empires II     🔔 Following
Quake III Arena       🔔 Following
Counter-Strike 2      ○
```

When someone starts a Wave for a followed game, LANsurfer can notify interested players.

---

## 🔥 Discover what's happening

LANsurfer isn't just about actively searching for players.

It also makes the current gaming interests of a community visible.

```text
🔥 Trending Now

Counter-Strike 2       23 interested
Trackmania             17 interested
Warcraft III           11 interested
Age of Empires II       8 interested
Quake III Arena         6 interested
```

At a large LAN party, this can reveal gaming opportunities that would otherwise go unnoticed.

---

## 🟢 Ready to Play

Players can optionally signal that they are currently available for specific games.

```text
🟢 Ready to Play

Warcraft III
Age of Empires II
Quake III Arena
Trackmania
```

This makes it easy to see which games could be started **right now**.

---

## 🎯 Built for LAN parties

LANsurfer was created with physical LAN events in mind.

The original use case is **CXT**, a LAN party with around 400 participants.

With hundreds of players in the same building, the challenge isn't necessarily finding *people*.

It's finding the people who want to play **the same game at the same time**.

LANsurfer aims to solve exactly that.

---

## 🔌 Integrations

LANsurfer is designed around the idea that LFG requests shouldn't live in just one place.

Planned integrations include:

### 🌐 Web

The main LANsurfer application.

Browse Waves, follow games, manage your profile, and find players.

### 💬 Discord

Discord integration can notify communities about new Waves and allow players to interact with LANsurfer directly from Discord.

### 🔔 Web Push

Get notified when someone wants to play one of your followed games.

### 📺 Twitch

Potential future integrations may allow Waves or currently trending games to appear in event streams.

### 🔗 API & Webhooks

LANsurfer aims to provide APIs and webhooks so communities can build their own integrations.

---

## 🗺️ Project Status

> ⚠️ **LANsurfer is currently in early development.**

The architecture, APIs, database schema, and UI may change significantly.

The initial goal is to build a working MVP and test it at a real LAN event.

---

## 🚀 MVP

The first version of LANsurfer will focus on the core experience:

* [ ] User authentication
* [ ] Discord login
* [ ] Create and manage events / communities
* [ ] Browse games
* [ ] Follow games
* [ ] Create Waves
* [ ] Join and leave Waves
* [ ] Player limits
* [ ] Start time / "Play now"
* [ ] Casual / Competitive tags
* [ ] Ready-to-Play status
* [ ] Notifications
* [ ] Discord integration
* [ ] Responsive web interface
* [ ] Dark mode
* [ ] Self-hosting
* [ ] Docker deployment

The guiding principle is simple:

> **A player should be able to find or start a game within seconds.**

---

## 🔮 Ideas for the Future

LANsurfer may eventually support:

* Steam library import
* Discord game roles
* scheduled Waves
* recurring gaming sessions
* skill / experience preferences
* team creation
* voice channel integration
* automatic temporary Discord channels
* tournament integration
* event displays / big-screen dashboards
* public and private communities
* multiple LAN events per instance
* federation between LANsurfer instances
* Matrix integration
* Twitch integration
* mobile notifications
* public API
* webhooks
* bots
* community statistics

And probably a few things we haven't thought of yet.

---

## 🎨 The LANsurfer Language

LANsurfer uses a light surfing theme throughout the project.

| LANsurfer            | Meaning                               |
| -------------------- | ------------------------------------- |
| 🌊 **Wave**          | Looking for Group request             |
| 👥 **Crew**          | Players joining a Wave                |
| 📍 **Spot**          | LAN event or gaming community         |
| 🏄 **Catch a Wave**  | Join an LFG                           |
| 🔔 **Follow**        | Subscribe to notifications for a game |
| 🟢 **Ready to Play** | Available to play right now           |

The theme should remain fun without getting in the way of usability.

---

## 🦦 Meet the LANsurfer Otter

LANsurfer's mascot is an otter.

Why an otter?

Otters are social, spend a lot of time in the water, and — most importantly — look great with a headset and a surfboard.

The mascot may appear in:

* onboarding
* empty states
* Discord
* documentation
* stickers
* event displays
* community content

The application itself remains focused on a clean and modern UI.

---

## 🎨 Design

LANsurfer combines a modern gaming interface with an ocean-inspired visual identity.

Primary colors:

| Color       | Hex       |
| ----------- | --------- |
| Deep Ocean  | `#0D1B2A` |
| Ocean Depth | `#1B263B` |
| Surf Blue   | `#00B4D8` |
| Aqua Wave   | `#48CAE4` |
| Sea Foam    | `#90E0EF` |
| White Sand  | `#F8F9FA` |
| Sunset      | `#F4D35E` |

The interface is designed **Dark Mode first**.

The goal is to avoid the typical overloaded RGB/cyberpunk gaming aesthetic and instead create something clean, modern, and community-focused.

---

## 🐳 Self-Hosting

LANsurfer is intended to be easy to self-host.

The goal is for LAN organizers and gaming communities to run their own instance:

```text
your-lan.example
lfg.your-community.example
games.example.org
```

Docker-based deployment is planned as the primary installation method.

More documentation will follow as the project develops.

---

## 🤝 Contributing

Contributions are welcome!

LANsurfer is being developed as an open-source project, and we'd love to see it used and improved by other LAN parties and gaming communities.

You can help with:

* 💻 Development
* 🎨 UI / UX
* 🐛 Bug reports
* 💡 Feature ideas
* 📖 Documentation
* 🌍 Translations
* 🔌 Integrations
* 🧪 Testing at LAN events

If you have an idea, feel free to open an **Issue** or start a **Discussion**.

Contribution guidelines will be added as the project matures.

---

## 💡 Philosophy

LANsurfer is built around a few simple principles:

**Gaming is better together.**

LAN parties should make it easy to discover what other people want to play.

**Finding players shouldn't require searching through dozens of Discord channels.**

Open protocols and integrations are better than platform lock-in.

**Communities should be able to host their own infrastructure.**

And sometimes the best game at a LAN isn't the newest one.

It's the 20-year-old game you suddenly discover ten other people also want to play.

---

## 📜 License

LANsurfer is free and open-source software licensed under the **GNU Affero General Public License v3.0 (AGPL-3.0)**.

You are free to use, modify, and distribute LANsurfer under the terms of the license.

See [`LICENSE`](LICENSE) for details.

---

## 🌊 LANsurfer

**Find your crew. Catch the next game.**

Built for LAN parties.
Built for gaming communities.
Built in the open.
