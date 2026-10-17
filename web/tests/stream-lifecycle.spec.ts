import { expect, test } from "@playwright/test";
import { expectStreams, fixtureState, resetFixture, simulateRealtimeReconnect, waitForRealtime } from "./helpers";

// The hang reported in issue #15 was not a rendering bug. Every page held its
// EventSource open for its whole lifetime, and a browser speaking HTTP/1.1
// allows six connections per origin, so a handful of pages each holding a
// stream consumed the pool and every further request — navigation included —
// waited forever. The fix releases the stream when a page stops being visible.
//
// That fix lives entirely in web/src/app.ts. No Go test can reach it, which is
// why removing the listener would otherwise reintroduce the hang silently.

test.beforeEach(async ({ request }) => {
  await resetFixture(request);
});

test("a page opens exactly one realtime stream and releases it on close", async ({ page, request }) => {
  await page.goto("/game-requests");
  await waitForRealtime(page, request, 1);

  await page.close();

  await expectStreams(request, 0);
});

test("a reconnect resynchronizes filtered discovery and detail state without another SSE event", async ({ page, request }) => {
  await page.addInitScript(() => {
    const browserWindow = window as Window & { __eventSource?: EventSource };
    const NativeEventSource = window.EventSource;
    window.EventSource = class extends NativeEventSource {
      constructor(url: string | URL, eventSourceInitDict?: EventSourceInit) {
        super(url, eventSourceInitDict);
        browserWindow.__eventSource = this;
      }
    };
  });
  const requestID = "00000000-0000-0000-0000-000000000003";

  await page.goto("/game-requests?q=age");
  await waitForRealtime(page, request, 1);
  const filteredRequest = page.locator(`article[data-game-request-id="${requestID}"]`);
  await expect(filteredRequest).toContainText("3/4");
  await expect(page.locator("#game-request-list article[data-game-request-id]")).toHaveCount(1);

  const discoveryChange = await request.get("/__test__/lifecycle?status=open&starts_at=2026-10-20T18:00:00Z&joined=1");
  expect(discoveryChange.status()).toBe(204);
  await simulateRealtimeReconnect(page);
  await expect(filteredRequest).toContainText("4/4");
  await expect(page.locator("#game-request-list article[data-game-request-id]")).toHaveCount(1);
  await expect(page.locator("#game-request-list")).toHaveAttribute("data-filter-query", /q=age/);
  await expect(page.locator("#request-search")).toHaveValue("age");

  await resetFixture(request);
  await page.goto(`/game-requests/${requestID}`);
  await waitForRealtime(page, request, 1);
  await expect(page.locator("#game-request-detail")).toContainText("3/4");
  await expect(page.locator("#session-information")).toContainText("Join this Game Request to see session information.");
  await expect(page.locator("#game-request-lobby")).toContainText("Join this Game Request to access the Lobby.");

  const detailChange = await request.get("/__test__/lifecycle?status=open&starts_at=2026-10-20T18:00:00Z&joined=1");
  expect(detailChange.status()).toBe(204);
  await simulateRealtimeReconnect(page);
  await expect(page.locator("#game-request-detail")).toContainText("4/4");
  await expect(page.locator("#session-information")).toContainText("No session information yet.");
  await expect(page.locator("#game-request-lobby")).toContainText("Host created the request");
});

test("native EventSource reconnect retries failed resync after an outage", async ({ page, request }) => {
  await page.addInitScript(() => {
    const browserWindow = window as Window & { __eventSourceStates?: string[] };
    const NativeEventSource = window.EventSource;
    window.EventSource = class extends NativeEventSource {
      constructor(url: string | URL, eventSourceInitDict?: EventSourceInit) {
        super(url, eventSourceInitDict);
        browserWindow.__eventSourceStates = [];
        this.addEventListener("open", () => browserWindow.__eventSourceStates?.push("open"));
        this.addEventListener("error", () => browserWindow.__eventSourceStates?.push("error"));
      }
    };
  });
  let refreshAttempts = 0;
  let failReconnectRefresh = false;
  let failedReconnectRefresh = false;
  let refreshFailureAt = 0;
  let retryDelay = 0;
  await page.route("**/game-requests/00000000-0000-0000-0000-000000000003/refresh", async (route) => {
    refreshAttempts += 1;
    if (failReconnectRefresh && !failedReconnectRefresh) {
      failedReconnectRefresh = true;
      refreshFailureAt = Date.now();
      await route.fulfill({ status: 503, body: "temporary refresh failure" });
      return;
    }
    if (failedReconnectRefresh) retryDelay = Date.now() - refreshFailureAt;
    await route.continue();
  });
  let releaseRetry!: () => void;
  let signalRetry!: () => void;
  let attempts = 0;
  const retryStarted = new Promise<void>((resolve) => { signalRetry = resolve; });
  const holdRetry = new Promise<void>((resolve) => { releaseRetry = resolve; });

  await page.route("**/events/game-requests**", async (route) => {
    attempts += 1;
    if (attempts === 1) {
      await route.fulfill({
        status: 200,
        headers: { "content-type": "text/event-stream" },
        body: "retry: 50\n\n",
      });
      return;
    }
    if (attempts === 2) {
      signalRetry();
      await holdRetry;
    }
    await route.continue();
  });

  const requestID = "00000000-0000-0000-0000-000000000003";
  await page.goto(`/game-requests/${requestID}`);
  await retryStarted;
  const mutation = await request.get("/__test__/lifecycle?status=open&starts_at=2026-10-20T18:00:00Z&joined=1");
  failReconnectRefresh = true;
  releaseRetry();
  await waitForRealtime(page, request, 1);
  await expect(page.locator("#game-request-detail")).toContainText("4/4");
  await expect(page.locator("#session-information")).toContainText("No session information yet.");
  await expect(page.locator("#game-request-lobby")).toContainText("Host created the request");
  expect(refreshAttempts).toBe(2);
  expect(retryDelay).toBeGreaterThanOrEqual(500);
  await expect.poll(() => page.evaluate(() => {
    const browserWindow = window as Window & { __eventSourceStates?: string[] };
    return browserWindow.__eventSourceStates;
  })).toEqual(["open", "error", "open"]);
});

test("a hidden page releases its stream and reclaims it when visible again", async ({ page, request }) => {
  await page.goto("/game-requests");
  await waitForRealtime(page, request, 1);

  // document.hidden is read-only, so the test sets what the browser would set
  // and fires the event the browser would fire. What is under test is the
  // application's reaction to the page becoming hidden.
  await page.evaluate(() => {
    Object.defineProperty(document, "hidden", { configurable: true, get: () => true });
    document.dispatchEvent(new Event("visibilitychange"));
  });

  await expectStreams(request, 0);

  await page.evaluate(() => {
    Object.defineProperty(document, "hidden", { configurable: true, get: () => false });
    document.dispatchEvent(new Event("visibilitychange"));
  });

  await expectStreams(request, 1);
});

// The realtime element lives in the shared application shell, so every
// authenticated page opens a stream. Navigation must therefore hand the
// connection over rather than accumulate one stream per page visited, which is
// exactly how the original pool exhaustion built up.
test("navigating hands the stream over instead of accumulating one per page", async ({ page, request }) => {
  await page.goto("/game-requests");
  await waitForRealtime(page, request, 1);

  for (const path of ["/games", "/game-requests", "/games", "/game-requests"]) {
    await page.goto(path);
    await expectStreams(request, 1);
  }
});

// The original failure needed more open pages than the browser has connections
// for. Six is the Chromium per-origin HTTP/1.1 limit, so six pages holding a
// stream was the exact point where every further request stalled.
test("six open pages do not exhaust the connection pool", async ({ browser, request }) => {
  const context = await browser.newContext({
    storageState: {
      cookies: [
        {
          name: "lansurfer_session",
          value: "browser-test-session",
          domain: "127.0.0.1",
          path: "/",
          expires: -1,
          httpOnly: true,
          secure: false,
          sameSite: "Lax",
        },
      ],
      origins: [],
    },
    baseURL: "http://127.0.0.1:8123",
  });

  const pages = [];
  for (let index = 0; index < 6; index += 1) {
    const page = await context.newPage();
    await page.goto("/game-requests");
    pages.push(page);
  }

  // Hide every page, as a player switching to a game or another application
  // does. Each one must give its connection back.
  for (const page of pages) {
    await page.evaluate(() => {
      Object.defineProperty(document, "hidden", { configurable: true, get: () => true });
      document.dispatchEvent(new Event("visibilitychange"));
    });
  }
  await expectStreams(request, 0);

  // With the pool free, a seventh page still navigates. Before the fix this is
  // where the browser stalled indefinitely.
  const seventh = await context.newPage();
  await seventh.goto("/game-requests", { timeout: 15_000 });
  await expect(seventh.locator("#game-request-list")).toBeVisible();

  expect((await fixtureState(request)).streams).toBeLessThanOrEqual(1);
  await context.close();
});
