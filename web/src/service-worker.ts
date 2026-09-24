// Track D owns this worker. It claims clients but does not intercept fetches
// or cache responses. Track E extends this worker for Web Push without adding
// another worker or registration path. LANsurfer has no offline shell.

// An empty export makes this file a module rather than a global script, so
// the narrower ServiceWorkerGlobalScope declaration below can shadow the
// ambient (generic worker) `self` the "WebWorker" lib provides instead of
// colliding with it.
export {};

// A dedicated Service Worker global scope, not a DOM window: `self` here is
// ServiceWorkerGlobalScope.
declare const self: ServiceWorkerGlobalScope;

self.addEventListener("install", () => {
  // Replace any previously installed worker immediately: this worker caches
  // nothing, so there is no stale content an earlier version could still be
  // serving that a delayed activation would protect.
  void self.skipWaiting();
});

self.addEventListener("activate", (event) => {
  event.waitUntil(self.clients.claim());
});

type PushKind = "created" | "joined" | "left" | "closed" | "cancelled";

interface E2PushPayload {
  version: 1;
  kind: PushKind;
  game: string;
  starts_at?: string;
  url: string;
  instance: string;
  actor_game_nick?: string;
  title: string;
  body: string;
}

function isPushKind(value: unknown): value is PushKind {
  return value === "created" || value === "joined" || value === "left" || value === "closed" || value === "cancelled";
}

function isPushPayload(value: unknown): value is E2PushPayload {
  if (typeof value !== "object" || value === null || Array.isArray(value)) return false;
  if (!("version" in value) || !("kind" in value) || !("game" in value) || !("instance" in value) || !("title" in value) || !("body" in value) || !("url" in value)) return false;
  if (value.version !== 1 || !isPushKind(value.kind)) return false;
  if (typeof value.game !== "string" || value.game.trim() === "") return false;
  if (typeof value.instance !== "string" || value.instance.trim() === "") return false;
  if (typeof value.title !== "string" || typeof value.body !== "string" || typeof value.url !== "string") return false;

  const hasActor = value.kind === "joined" || value.kind === "left";
  const actorGameNick = "actor_game_nick" in value ? value.actor_game_nick : undefined;
  if (hasActor && (typeof actorGameNick !== "string" || actorGameNick.trim() === "")) return false;
  if (!hasActor && actorGameNick !== undefined) return false;
  const startsAt = "starts_at" in value ? value.starts_at : undefined;
  if (startsAt !== undefined && (
    typeof startsAt !== "string" ||
    !/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})$/.test(startsAt) ||
    !Number.isFinite(Date.parse(startsAt))
  )) return false;

  let url: URL;
  try {
    url = new URL(value.url);
  } catch {
    return false;
  }
  if (url.origin !== self.location.origin || !url.pathname.startsWith("/game-requests/") || url.pathname === "/game-requests/" || url.search || url.hash) return false;

  const actor = typeof actorGameNick === "string" ? actorGameNick : "";
  const title = value.kind === "created"
    ? `New ${value.game} Game Request`
    : value.kind === "joined" || value.kind === "left"
      ? `${actor} ${value.kind} ${value.game}`
      : `${value.game} Game Request ${value.kind}`;
  const body = value.instance + (startsAt === undefined ? "" : ` · Starts ${startsAt}`);
  return value.title === title && value.body === body;
}


function parsePushPayload(data: PushMessageData | null): E2PushPayload | null {
  try {
    const value: unknown = JSON.parse(data?.text() ?? "");
    if (!isPushPayload(value)) return null;
    return { ...value, url: new URL(value.url).href };
  } catch {
    return null;
  }
}

self.addEventListener("push", (event) => {
  event.waitUntil((async () => {
    const payload = parsePushPayload(event.data);
    if (!payload) return;

    const windows = await self.clients.matchAll({ type: "window", includeUncontrolled: true });
    if (windows.some((client) => {
      try {
        return new URL(client.url).origin === self.location.origin && client.visibilityState === "visible";
      } catch {
        return false;
      }
    })) return;

    await self.registration.showNotification(payload.title, {
      body: payload.body,
      data: { url: payload.url },
    });
  })());
});
