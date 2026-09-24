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
