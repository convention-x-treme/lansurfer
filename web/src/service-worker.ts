// LANsurfer uses one service worker for installability and Web Push. It claims
// clients but does not intercept fetches or cache responses, and no second
// worker or registration path is introduced. LANsurfer has no offline shell.

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

type PushKind = "created" | "joined" | "left" | "started" | "closed" | "cancelled";

interface PushPayload {
  version: 3;
  kind: PushKind;
  game: string;
  member_count: number;
  max_players?: number;
  creator_game_nick?: string;
  starts_at?: string;
  url: string;
  instance: string;
  actor_game_nick?: string;
}

function isPushKind(value: unknown): value is PushKind {
  return value === "created" || value === "joined" || value === "left" || value === "started" || value === "closed" || value === "cancelled";
}

function gameRequestURL(value: unknown): URL | null {
  if (typeof value !== "string") return null;
  try {
    const url = new URL(value);
    const requestID = url.pathname.slice("/game-requests/".length);
    if (url.origin !== self.location.origin || !url.pathname.startsWith("/game-requests/") || !requestID || requestID.includes("/") || url.search || url.hash) return null;
    return url;
  } catch {
    return null;
  }
}

function isPushPayload(value: unknown): value is PushPayload {
  if (typeof value !== "object" || value === null || Array.isArray(value)) return false;
  if (!("version" in value) || !("kind" in value) || !("game" in value) || !("member_count" in value) || !("instance" in value) || !("url" in value)) return false;
  if (value.version !== 3 || !isPushKind(value.kind)) return false;
  if (typeof value.game !== "string" || value.game.trim() === "") return false;
  if (typeof value.instance !== "string" || value.instance.trim() === "") return false;
  if (!Number.isSafeInteger(value.member_count) || (value.member_count as number) < 0) return false;
  if ("max_players" in value && (!Number.isSafeInteger(value.max_players) || (value.max_players as number) < 1 || (value.max_players as number) < (value.member_count as number))) return false;
  const creator = "creator_game_nick" in value ? value.creator_game_nick : undefined;
  if (creator !== undefined && (typeof creator !== "string" || creator.trim() === "")) return false;

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

  return gameRequestURL(value.url) !== null;
}


function parsePushPayload(data: PushMessageData | null): PushPayload | null {
  try {
    const value: unknown = JSON.parse(data?.text() ?? "");
    if (!isPushPayload(value)) return null;
    const url = gameRequestURL(value.url);
    return url ? { ...value, url: url.href } : null;
  } catch {
    return null;
  }
}

function notificationURL(data: unknown): URL | null {
  if (typeof data !== "object" || data === null || Array.isArray(data) || !("url" in data)) return null;
  return gameRequestURL(data.url);
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

    const title = payload.kind === "started"
      ? payload.game
      : payload.creator_game_nick
        ? `${payload.creator_game_nick} is looking for ${payload.game}`
        : payload.game;
    const startsAt = payload.starts_at
      ? new Intl.DateTimeFormat(undefined, { dateStyle: "full", timeStyle: "short" }).format(new Date(payload.starts_at))
      : "Not set";
    const prefix = payload.kind === "joined" || payload.kind === "left"
      ? `${payload.actor_game_nick} ${payload.kind} · `
      : payload.kind === "started"
        ? "Game started · "
        : payload.kind === "closed" || payload.kind === "cancelled"
          ? `Request ${payload.kind} · `
          : "";
    const players = payload.max_players === undefined
      ? `${payload.member_count} players (unlimited)`
      : `${payload.member_count}/${payload.max_players} players`;
    await self.registration.showNotification(title, {
      body: `${prefix}${startsAt} · ${players} · ${payload.instance}`,
      data: { url: payload.url },
    });
  })());
});

self.addEventListener("notificationclick", (event) => {
  event.notification.close();
  const url = notificationURL(event.notification.data);
  if (!url) return;

  event.waitUntil((async () => {
    const windows = await self.clients.matchAll({ type: "window", includeUncontrolled: true });
    const client = windows.find((windowClient) => {
      try {
        return new URL(windowClient.url).origin === self.location.origin;
      } catch {
        return false;
      }
    });
    if (client) {
      await client.focus();
      // iOS WebKit rejects or returns null from navigate() on a resumed PWA
      // window, which left the app open on whatever page it had. The page
      // then navigates itself on this message instead.
      const navigated = await client.navigate(url.href).catch(() => null);
      if (!navigated) client.postMessage({ type: "open-game-request", url: url.href });
      return;
    }
    await self.clients.openWindow(url.href);
  })());
});
