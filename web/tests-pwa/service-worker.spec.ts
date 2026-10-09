import { expect, test, type Page, type Worker } from "@playwright/test";

// Track D's service-worker contract is deliberately narrow: install/activate
// only, registered once at root scope, swallowing its own failures. These
// tests cover exactly the interaction-hardening behavior no Go test can
// reach — the compiled bundle's own content (no fetch handler, headers,
// route reachability) is covered by internal/web's Go tests instead.

test("registers at root scope and takes control of the page", async ({ page, baseURL }) => {
  await page.goto("/game-requests");

  await page.waitForFunction(() => navigator.serviceWorker.controller !== null);

  const scope = await page.evaluate(async () => (await navigator.serviceWorker.getRegistration("/"))?.scope);
  expect(scope).toBe(`${baseURL}/`);
});

test("a reload replaces the worker immediately without leaving one waiting", async ({ page }) => {
  await page.goto("/game-requests");
  await page.waitForFunction(() => navigator.serviceWorker.controller !== null);

  await page.reload();
  await page.waitForFunction(() => navigator.serviceWorker.controller !== null);

  const waiting = await page.evaluate(async () => (await navigator.serviceWorker.getRegistration("/"))?.waiting ?? null);
  expect(waiting).toBeNull();
});

// Registration failure must never surface as a player-facing error: the rest
// of LANsurfer works identically whether or not the worker is available.
test("a registration failure degrades without a page error", async ({ page }) => {
  const pageErrors: Error[] = [];
  page.on("pageerror", (error) => pageErrors.push(error));

  await page.addInitScript(() => {
    Object.defineProperty(navigator, "serviceWorker", {
      configurable: true,
      value: { register: () => Promise.reject(new Error("registration boom")) },
    });
  });

  await page.goto("/game-requests");
  await expect(page.locator("#game-request-list")).toBeVisible();

  expect(pageErrors).toHaveLength(0);
});

test("the worker never creates a CacheStorage entry", async ({ page }) => {
  await page.goto("/game-requests");
  await page.waitForFunction(() => navigator.serviceWorker.controller !== null);

  const cacheNames = await page.evaluate(() => caches.keys());
  expect(cacheNames).toEqual([]);
});

test("no response is served from the service worker (no fetch interception)", async ({ page }) => {
  await page.goto("/game-requests");
  await page.waitForFunction(() => navigator.serviceWorker.controller !== null);

  const fromServiceWorker: boolean[] = [];
  page.on("response", (response) => fromServiceWorker.push(response.fromServiceWorker()));

  // Not "networkidle": the page holds an open SSE stream (the realtime update
  // path), which never goes idle and would time this out.
  await page.reload();
  await page.waitForSelector("[data-authenticated-shell]");

  expect(fromServiceWorker.some(Boolean)).toBe(false);
});

// The application's own reaction to a bfcache restore, exercised the way this
// suite already exercises visibilitychange elsewhere: dispatch the event the
// browser would dispatch and assert the application's listener acts on it.
test("a bfcache restore on an authenticated page forces a fresh navigation", async ({ page }) => {
  await page.goto("/game-requests");
  await page.waitForSelector("[data-authenticated-shell]");

  const navigation = page.waitForEvent("framenavigated");
  await page.evaluate(() => {
    window.dispatchEvent(new PageTransitionEvent("pageshow", { persisted: true }));
  });
  await navigation;
});

for (const state of [
  { name: "unsubscribe succeeds", failure: false, available: true, unsubscribed: true },
  { name: "unsubscribe fails", failure: true, available: true, unsubscribed: true },
  { name: "Push APIs are absent", failure: false, available: false, unsubscribed: false },
]) {
  test(`logout continues when ${state.name}`, async ({ page }) => {
    let logoutPosts = 0;
    await page.route("**/auth/logout", async (route) => {
      logoutPosts++;
      expect(route.request().method()).toBe("POST");
      await route.fulfill({ status: 200, body: "logged out" });
    });
    await page.addInitScript((options) => {
      const container = navigator.serviceWorker;
      const register = container.register.bind(container);
      Object.defineProperty(container, "register", {
        configurable: true,
        value: async (...args: Parameters<ServiceWorkerContainer["register"]>) => {
          const registration = await register(...args);
          return new Proxy(registration, {
            get(target, property) {
              if (property === "pushManager") {
                if (!options.available) return undefined;
                return {
                  getSubscription: async () => ({
                    unsubscribe: async () => {
                      localStorage.setItem("logout-unsubscribed", "true");
                      if (options.failure) throw new Error("unsubscribe failed");
                      return true;
                    },
                  }),
                };
              }
              if (property === "unregister") {
                return async () => {
                  localStorage.setItem("logout-unregistered", "true");
                  return target.unregister();
                };
              }
              const value = Reflect.get(target, property, target);
              return typeof value === "function" ? value.bind(target) : value;
            },
          });
        },
      });
    }, state);
    await page.goto("/game-requests");
    await page.waitForFunction(() => navigator.serviceWorker.controller !== null);
    await page.getByRole("button", { name: "Log out" }).click();
    await expect(page.getByText("logged out")).toBeVisible();
    expect(logoutPosts).toBe(1);
    expect(await page.evaluate(() => localStorage.getItem("logout-unsubscribed"))).toBe(state.unsubscribed ? "true" : null);
    expect(await page.evaluate(() => localStorage.getItem("logout-unregistered"))).toBeNull();
    expect(await page.evaluate(async () => (await navigator.serviceWorker.getRegistration("/"))?.scope)).toBeTruthy();
  });
}


const pushPayload = (origin: string) => ({
  version: 4,
  kind: "created",
  game: "Demo",
  member_count: 1,
  max_players: 4,
  creator_game_nick: "Ada",
  starts_at: "2026-09-24T12:00:00Z",
  status: "open",
  url: `${origin}/game-requests/request-123`,
  instance: "LANsurfer",
});

async function openPushChannel(page: Page) {
  const cdp = await page.context().newCDPSession(page);
  const origin = new URL(page.url()).origin;
  let registrationId = "";
  cdp.on("ServiceWorker.workerRegistrationUpdated", ({ registrations }) => {
    registrationId = registrations.find((registration) => registration.scopeURL === `${origin}/`)?.registrationId ?? "";
  });
  await cdp.send("ServiceWorker.enable");
  await expect.poll(() => registrationId).not.toBe("");
  return { cdp, origin, registrationId };
}

async function deliverPush(page: Page, data: string): Promise<void> {
  const channel = await openPushChannel(page);
  await channel.cdp.send("ServiceWorker.deliverPushMessage", {
    origin: channel.origin,
    registrationId: channel.registrationId,
    data,
  });
  await channel.cdp.detach();
}

test("visible LANsurfer window suppresses OS push notification", async ({ page }) => {
  await page.context().grantPermissions(["notifications"]);
  await page.goto("/game-requests");
  await page.waitForFunction(() => navigator.serviceWorker.controller !== null);
  await deliverPush(page, JSON.stringify({ ...pushPayload(new URL(page.url()).origin), kind: "started", status: "playing", started_at: "2026-09-24T11:30:00Z", creator_game_nick: undefined, member_count: 3, max_players: 4 }));
  const notifications = await page.evaluate(async () => (await navigator.serviceWorker.getRegistration("/"))?.getNotifications());
  expect(notifications).toHaveLength(0);
});

test("push without visible LANsurfer window shows localized created, joined, and cancelled notifications", async ({ page }) => {
  await page.context().grantPermissions(["notifications"]);
  await page.goto("/game-requests");
  await page.waitForFunction(() => navigator.serviceWorker.controller !== null);
  const channel = await openPushChannel(page);
  const worker = await getServiceWorker(page);
  await page.goto("about:blank");
  await channel.cdp.send("ServiceWorker.deliverPushMessage", {
    origin: channel.origin,
    registrationId: channel.registrationId,
    data: JSON.stringify(pushPayload(channel.origin)),
  });
  await channel.cdp.send("ServiceWorker.deliverPushMessage", {
    origin: channel.origin,
    registrationId: channel.registrationId,
    data: JSON.stringify({ ...pushPayload(channel.origin), kind: "joined", actor_game_nick: "Bo", starts_at: undefined }),
  });
  await channel.cdp.send("ServiceWorker.deliverPushMessage", {
    origin: channel.origin,
    registrationId: channel.registrationId,
    data: JSON.stringify({ ...pushPayload(channel.origin), kind: "cancelled", status: "cancelled", creator_game_nick: undefined, starts_at: undefined, max_players: undefined, member_count: 7 }),
  });
  await channel.cdp.send("ServiceWorker.deliverPushMessage", {
    origin: channel.origin,
    registrationId: channel.registrationId,
    data: JSON.stringify({ ...pushPayload(channel.origin), kind: "started", status: "playing", started_at: "2026-09-24T11:30:00Z", creator_game_nick: undefined, member_count: 3, max_players: 4 }),
  });
  await channel.cdp.send("ServiceWorker.deliverPushMessage", {
    origin: channel.origin,
    registrationId: channel.registrationId,
    data: JSON.stringify({ ...pushPayload(channel.origin), kind: "closed", status: "closed", started_at: "2026-09-24T11:30:00Z", creator_game_nick: undefined }),
  });
  await channel.cdp.send("ServiceWorker.deliverPushMessage", {
    origin: channel.origin,
    registrationId: channel.registrationId,
    data: JSON.stringify({ ...pushPayload(channel.origin), kind: "closed", status: "closed", creator_game_nick: undefined }),
  });
  await expect.poll(() => worker.evaluate(async () => (await (globalThis as unknown as ServiceWorkerGlobalScope).registration.getNotifications()).length)).toBe(6);
  await page.goto("/game-requests");
  const notifications = await page.evaluate(async () => (await navigator.serviceWorker.getRegistration("/"))?.getNotifications().then((items) => items.map(({ title, body }) => ({ title, body }))));
  expect(notifications).toHaveLength(6);
  expect(notifications?.map(({ title }) => title)).toEqual(["Ada is looking for Demo", "Ada is looking for Demo", "Demo", "Demo", "Demo", "Demo"]);
  const localStart = new Intl.DateTimeFormat(undefined, { dateStyle: "full", timeStyle: "short" }).format(new Date("2026-09-24T12:00:00Z"));
  const localActual = new Intl.DateTimeFormat(undefined, { dateStyle: "full", timeStyle: "short" }).format(new Date("2026-09-24T11:30:00Z"));
  expect(notifications?.map(({ body }) => body)).toContain(`Planned start: ${localStart} · 1/4 players · LANsurfer`);
  expect(notifications?.map(({ body }) => body)).toContain("Bo joined · 1/4 players · LANsurfer");
  expect(notifications?.map(({ body }) => body)).toContain("Request cancelled · 7 players (unlimited) · LANsurfer");
  expect(notifications?.map(({ body }) => body)).toContain(`Game started · Started at: ${localActual} · 3/4 players · LANsurfer`);
  expect(notifications?.map(({ body }) => body)).toContain(`Request closed · Started at: ${localActual} · 1/4 players · LANsurfer`);
  expect(notifications?.map(({ body }) => body)).toContain(`Request closed · Planned start: ${localStart} · 1/4 players · LANsurfer`);
  await channel.cdp.detach();
});

test("planned-start and starting-soon pushes use planned-time labels and discard expired reminders", async ({ page }) => {
  await page.context().grantPermissions(["notifications"]);
  await page.goto("/game-requests");
  await page.waitForFunction(() => navigator.serviceWorker.controller !== null);
  const channel = await openPushChannel(page);
  const worker = await getServiceWorker(page);
  await page.goto("about:blank");
  const startsAt = new Date(Date.now() + 10 * 60 * 1000).toISOString();
  for (const kind of ["planned_start_changed", "starting_soon"]) {
    await channel.cdp.send("ServiceWorker.deliverPushMessage", {
      origin: channel.origin,
      registrationId: channel.registrationId,
      data: JSON.stringify({
        version: 4, kind, game: "Demo", starts_at: startsAt,
        url: `${channel.origin}/game-requests/request-123`, instance: "LANsurfer",
      }),
    });
  }
  await channel.cdp.send("ServiceWorker.deliverPushMessage", {
    origin: channel.origin,
    registrationId: channel.registrationId,
    data: JSON.stringify({
      version: 4, kind: "starting_soon", game: "Demo", starts_at: "2020-01-01T00:00:00Z",
      url: `${channel.origin}/game-requests/request-123`, instance: "LANsurfer",
    }),
  });
  for (const invalid of [
    {
      version: 4, kind: "starting_soon", game: "Demo", starts_at: startsAt, member_count: 1,
      url: `${channel.origin}/game-requests/request-123`, instance: "LANsurfer",
    },
    {
      version: 4, kind: "planned_start_changed", game: "Demo",
      url: `${channel.origin}/game-requests/request-123`, instance: "LANsurfer",
    },
  ]) {
    await channel.cdp.send("ServiceWorker.deliverPushMessage", {
      origin: channel.origin, registrationId: channel.registrationId, data: JSON.stringify(invalid),
    });
  }
  await expect.poll(() => worker.evaluate(async () => (await (globalThis as unknown as ServiceWorkerGlobalScope).registration.getNotifications()).length)).toBe(2);
  await page.goto("/game-requests");
  const notifications = await page.evaluate(async () => (await navigator.serviceWorker.getRegistration("/"))?.getNotifications().then((items) => items.map(({ title, body }) => ({ title, body }))));
  expect(notifications).toHaveLength(2);
  expect(notifications?.map(({ title }) => title)).toEqual(["Demo", "Demo"]);
  const localStart = new Intl.DateTimeFormat(undefined, { dateStyle: "full", timeStyle: "short" }).format(new Date(startsAt));
  expect(notifications?.map(({ body }) => body)).toEqual([
    `Start time changed · Planned start: ${localStart} · LANsurfer`,
    `Starting soon · Planned start: ${localStart} · LANsurfer`,
  ]);
  await channel.cdp.detach();
});

test("malformed and unsupported pushes do not crash or notify", async ({ page }) => {
  await page.context().grantPermissions(["notifications"]);
  await page.goto("/game-requests");
  await page.waitForFunction(() => navigator.serviceWorker.controller !== null);
  const channel = await openPushChannel(page);
  await page.goto("about:blank");
  for (const data of [
    "{",
    JSON.stringify({ ...pushPayload(channel.origin), version: 3 }),
    JSON.stringify({ ...pushPayload(channel.origin), status: "playing" }),
    JSON.stringify({ ...pushPayload(channel.origin), status: "unknown" }),
    JSON.stringify({ ...pushPayload(channel.origin), creator_game_nick: 12 }),
    JSON.stringify({ ...pushPayload(channel.origin), starts_at: "invalid" }),
    JSON.stringify({ ...pushPayload(channel.origin), url: `${channel.origin}/elsewhere/request-123` }),
    JSON.stringify({ ...pushPayload(channel.origin), member_count: -1 }),
    JSON.stringify({ ...pushPayload(channel.origin), max_players: 0 }),
  ]) {
    await channel.cdp.send("ServiceWorker.deliverPushMessage", { origin: channel.origin, registrationId: channel.registrationId, data });
  }
  await page.goto("/game-requests");
  const notifications = await page.evaluate(async () => (await navigator.serviceWorker.getRegistration("/"))?.getNotifications());
  expect(notifications).toHaveLength(0);
  await channel.cdp.detach();
});

async function getServiceWorker(page: Page): Promise<Worker> {
  await page.waitForFunction(() => navigator.serviceWorker.controller !== null);
  const worker = page.context().serviceWorkers().find((candidate) => candidate.url().endsWith("/service-worker.js"));
  if (!worker) throw new Error("Service worker is unavailable.");
  return worker;
}

async function clickNotification(worker: Worker, targetURL: string, withClient: boolean) {
  return worker.evaluate(async ({ targetURL, withClient }) => {
    // Worker.evaluate executes this callback inside the real service worker.
    const scope = globalThis as unknown as ServiceWorkerGlobalScope;
    const outcome = { closed: false, focused: false, navigated: "", opened: "" };
    const client = {
      url: `${scope.location.origin}/game-requests/old-request`,
      focus: async () => {
        outcome.focused = true;
        return client;
      },
      navigate: async (url: string) => {
        outcome.navigated = url;
        return client;
      },
    };
    Object.defineProperty(scope.clients, "matchAll", {
      configurable: true,
      value: async () => withClient ? [client] : [],
    });
    Object.defineProperty(scope.clients, "openWindow", {
      configurable: true,
      value: async (url: string) => {
        outcome.opened = url;
        return null;
      },
    });
    const waits: Promise<unknown>[] = [];
    const event = new Event("notificationclick");
    Object.defineProperties(event, {
      notification: { value: { data: { url: targetURL }, close: () => { outcome.closed = true; } } },
      waitUntil: { value: (promise: Promise<unknown>) => waits.push(promise) },
    });
    scope.dispatchEvent(event);
    await Promise.all(waits);
    return outcome;
  }, { targetURL, withClient });
}

test("notification click focuses and navigates existing LANsurfer client", async ({ page }) => {
  await page.goto("/game-requests");
  const worker = await getServiceWorker(page);
  const targetURL = new URL("/game-requests/request-123", page.url()).href;
  const outcome = await clickNotification(worker, targetURL, true);
  expect(outcome).toEqual({ closed: true, focused: true, navigated: targetURL, opened: "" });
});

test("notification click opens deep link when no LANsurfer client exists", async ({ page }) => {
  await page.goto("/game-requests");
  const worker = await getServiceWorker(page);
  const targetURL = new URL("/game-requests/request-123", page.url()).href;
  const outcome = await clickNotification(worker, targetURL, false);
  expect(outcome).toEqual({ closed: true, focused: false, navigated: "", opened: targetURL });
});

for (const targetURL of ["https://outside.invalid/game-requests/request-123", "not a URL"]) {
  test(`notification click rejects invalid target ${targetURL}`, async ({ page }) => {
    await page.goto("/game-requests");
    const worker = await getServiceWorker(page);
    const outcome = await clickNotification(worker, targetURL, true);
    expect(outcome).toEqual({ closed: true, focused: false, navigated: "", opened: "" });
  });
}