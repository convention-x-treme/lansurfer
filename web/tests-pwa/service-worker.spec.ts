import { expect, test } from "@playwright/test";

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
