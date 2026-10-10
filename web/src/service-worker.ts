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

type PushKind = "created" | "joined" | "left" | "started" | "closed" | "cancelled" | "starting_soon" | "planned_start_changed";
type RequestStatus = "open" | "playing" | "closed" | "cancelled";

interface ActivityPushPayload {
  version: 4;
  kind: "created" | "joined" | "left" | "started" | "closed" | "cancelled";
  game: string;
  member_count: number;
  max_players?: number;
  creator_game_nick?: string;
  starts_at?: string;
  started_at?: string;
  status: RequestStatus;
  url: string;
  instance: string;
  actor_game_nick?: string;
}

interface TimePushPayload {
  version: 4;
  kind: "starting_soon" | "planned_start_changed";
  game: string;
  starts_at: string;
  url: string;
  instance: string;
}
type PushPayload = ActivityPushPayload | TimePushPayload;

function isPushKind(value: unknown): value is PushKind {
  return value === "created" || value === "joined" || value === "left" || value === "started" || value === "closed" || value === "cancelled" || value === "starting_soon" || value === "planned_start_changed";
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

function validTimestamp(value: unknown): value is string {
  if (typeof value !== "string" ||
    !/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})$/.test(value)) return false;
  const parsed = new Date(value);
  return Number.isFinite(parsed.getTime()) && parsed.toISOString() !== "0001-01-01T00:00:00.000Z";
}

function isPushPayload(value: unknown): value is PushPayload {
  if (typeof value !== "object" || value === null || Array.isArray(value)) return false;
  if (!("version" in value) || !("kind" in value) || !("game" in value) || !("instance" in value) || !("url" in value)) return false;
  if (value.version !== 4 || !isPushKind(value.kind) || typeof value.game !== "string" || value.game.trim() === "" || typeof value.instance !== "string" || value.instance.trim() === "") return false;
  if (gameRequestURL(value.url) === null) return false;
  if (value.kind === "starting_soon" || value.kind === "planned_start_changed") {
    return Object.keys(value).every((key) => ["version", "kind", "game", "starts_at", "url", "instance"].includes(key)) &&
      Object.keys(value).length === 6 && "starts_at" in value && validTimestamp(value.starts_at);
  }
  if (!("member_count" in value) || !("status" in value)) return false;
  if (value.status !== "open" && value.status !== "playing" && value.status !== "closed" && value.status !== "cancelled") return false;
  if (!Number.isSafeInteger(value.member_count) || (value.member_count as number) < 0) return false;
  if ("max_players" in value && (!Number.isSafeInteger(value.max_players) || (value.max_players as number) < 1 || (value.max_players as number) < (value.member_count as number))) return false;
  const creator = "creator_game_nick" in value ? value.creator_game_nick : undefined;
  if (creator !== undefined && (typeof creator !== "string" || creator.trim() === "")) return false;
  const hasActor = value.kind === "joined" || value.kind === "left";
  const actorGameNick = "actor_game_nick" in value ? value.actor_game_nick : undefined;
  if (hasActor && (typeof actorGameNick !== "string" || actorGameNick.trim() === "")) return false;
  if (!hasActor && actorGameNick !== undefined) return false;
  const startsAt = "starts_at" in value ? value.starts_at : undefined;
  const startedAt = "started_at" in value ? value.started_at : undefined;
  if (startsAt !== undefined && !validTimestamp(startsAt)) return false;
  if (startedAt !== undefined && !validTimestamp(startedAt)) return false;
  return !((value.status === "open" || value.status === "cancelled") && startedAt !== undefined);
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
    if (payload.kind === "starting_soon" && Date.parse(payload.starts_at) <= Date.now()) return;
    const windows = await self.clients.matchAll({ type: "window", includeUncontrolled: true });
    if (windows.some((client) => {
      try { return new URL(client.url).origin === self.location.origin && client.visibilityState === "visible"; }
      catch { return false; }
    })) return;
    if (payload.kind === "starting_soon" || payload.kind === "planned_start_changed") {
      const label = payload.kind === "starting_soon" ? "Starting soon" : "Start time changed";
      const time = new Intl.DateTimeFormat(undefined, { dateStyle: "full", timeStyle: "short" }).format(new Date(payload.starts_at));
      await self.registration.showNotification(payload.game, {
        body: `${label} · Planned start: ${time} · ${payload.instance}`,
        data: { url: payload.url },
      });
      return;
    }
    if (!("status" in payload)) return;
    const title = payload.status === "open" && payload.creator_game_nick
      ? `${payload.creator_game_nick} is looking for ${payload.game}`
      : payload.game;
    const hasActualStart = payload.status === "playing" || (payload.status === "closed" && payload.started_at !== undefined);
    const timeValue = hasActualStart ? payload.started_at : payload.starts_at;
    const timeLabel = timeValue
      ? `${hasActualStart ? "Started at" : "Planned start"}: ${new Intl.DateTimeFormat(undefined, { dateStyle: "full", timeStyle: "short" }).format(new Date(timeValue))} · `
      : "";
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
      body: `${prefix}${timeLabel}${players} · ${payload.instance}`,
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
