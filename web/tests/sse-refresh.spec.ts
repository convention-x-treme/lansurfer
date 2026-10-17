import { expect, test, type Page } from "@playwright/test";
import { resetFixture, waitForRealtime } from "./helpers";

type RefreshProbe = { count: number; active: number; peak: number };
type TestWindow = Window & { __eventSource?: EventSource; __refreshProbe?: RefreshProbe; __swapPrevented?: boolean[] };

const requestId = "00000000-0000-0000-0000-000000000003";

async function openList(page: Page, request: Parameters<typeof resetFixture>[0], path = "/game-requests?status=open"): Promise<void> {
  await page.clock.install();
  await page.addInitScript(() => {
    const native = window.EventSource;
    window.EventSource = class extends native {
      constructor(...args: ConstructorParameters<typeof EventSource>) {
        super(...args);
        (window as TestWindow).__eventSource = this;
      }
    };
    const probe: RefreshProbe = { count: 0, active: 0, peak: 0 };
    (window as TestWindow).__refreshProbe = probe;
    document.addEventListener("htmx:beforeRequest", (event) => {
      const detail = (event as CustomEvent<{ requestConfig?: { path?: string } }>).detail;
      if (detail.requestConfig?.path?.includes("/refresh")) {
        probe.count++;
        probe.active++;
        probe.peak = Math.max(probe.peak, probe.active);
      }
    });
    document.addEventListener("htmx:afterRequest", (event) => {
      const detail = (event as CustomEvent<{ requestConfig?: { path?: string } }>).detail;
      if (detail.requestConfig?.path?.includes("/refresh")) probe.active--;
    });
  });
  await page.goto(path);
  if (path.startsWith("/game-requests?")) await expect(page.locator("#game-request-list")).toBeVisible();
  else await expect(page.locator(".detail-shell")).toBeVisible();
  await waitForRealtime(page, request, 1);
  await page.clock.pauseAt(new Date("2026-10-20T18:00:00Z"));
}

async function emit(page: Page, type: string, id = requestId): Promise<void> {
  await page.evaluate(({ type, id }) => {
    const source = (window as TestWindow).__eventSource;
    if (!source) throw new Error("EventSource was not recorded");
    source.dispatchEvent(new MessageEvent("game-request", {
      data: JSON.stringify({ type, requestId: id }),
    }));
  }, { type, id });
}

async function emitChat(page: Page, id = requestId): Promise<void> {
  await page.evaluate((requestId) => {
    const source = (window as TestWindow).__eventSource;
    if (!source) throw new Error("EventSource was not recorded");
    source.dispatchEvent(new MessageEvent("game-request-chat", {
      data: JSON.stringify({ requestId }),
    }));
  }, id);
}

async function refreshProbe(page: Page): Promise<RefreshProbe> {
  return await page.evaluate(() => ({ ...(window as TestWindow).__refreshProbe! }));
}

test.beforeEach(async ({ request }) => {
  await resetFixture(request);
});


test("detail refresh ignores events for other requests", async ({ page, request }) => {
  await openList(page, request, `/game-requests/${requestId}`);
  await emit(page, "GameRequestJoined", "00000000-0000-0000-0000-000000000004");
  await page.clock.runFor(250);
  expect((await refreshProbe(page)).count).toBe(0);
  await emit(page, "GameRequestJoined");
  await page.clock.runFor(200);
  await expect.poll(async () => (await refreshProbe(page)).count).toBe(1);
});

test("lobby refresh does not bypass failed detail refresh retry delay", async ({ page, request }) => {
  let detailCalls = 0;
  let lobbyCalls = 0;
  await page.route(`**/game-requests/${requestId}/refresh*`, async (route) => {
    detailCalls++;
    if (detailCalls === 1) await route.fulfill({ status: 503, body: "unavailable" });
    else await route.continue();
  });
  await page.route(`**/game-requests/${requestId}/lobby*`, async (route) => {
    lobbyCalls++;
    await route.fulfill({ status: 200, body: '<ol id="game-request-lobby" data-refresh-url="/game-requests/' + requestId + '/lobby"></ol>' });
  });
  await openList(page, request, `/game-requests/${requestId}`);
  await page.locator("[data-game-request-detail]").evaluate((detail, id) => {
    const lobby = document.createElement("ol");
    lobby.id = "game-request-lobby";
    lobby.dataset.refreshUrl = `/game-requests/${id}/lobby`;
    detail.append(lobby);
  }, requestId);
  await emit(page, "GameRequestJoined");
  await page.clock.runFor(200);
  await expect.poll(() => detailCalls).toBe(1);
  await emitChat(page);
  await expect.poll(() => lobbyCalls).toBe(1);
  await page.clock.runFor(999);
  expect(detailCalls).toBe(1);
  await page.clock.runFor(1);
  await expect.poll(() => detailCalls).toBe(2);
});

test("an automatic lobby load does not invalidate a trailing detail refresh", async ({ page, request }) => {
  let releaseFirst!: () => void;
  let releaseTrailing!: () => void;
  let notifyFirst!: () => void;
  let notifyTrailing!: () => void;
  let detailCalls = 0;
  let lobbyCalls = 0;
  const firstHold = new Promise<void>((resolve) => { releaseFirst = resolve; });
  const trailingHold = new Promise<void>((resolve) => { releaseTrailing = resolve; });
  const firstStarted = new Promise<void>((resolve) => { notifyFirst = resolve; });
  const trailingStarted = new Promise<void>((resolve) => { notifyTrailing = resolve; });
  await page.route(`**/game-requests/${requestId}/refresh*`, async (route) => {
    detailCalls++;
    if (detailCalls === 1) {
      notifyFirst();
      await firstHold;
    } else if (detailCalls === 2) {
      notifyTrailing();
      await trailingHold;
    }
    await route.fulfill({ response: await route.fetch() });
  });
  await page.route(`**/game-requests/${requestId}/lobby*`, async (route) => {
    lobbyCalls++;
    await route.fulfill({ status: 200, body: `<ol id="game-request-lobby" data-refresh-url="/game-requests/${requestId}/lobby"></ol>` });
  });
  const lifecycle = await request.get("/__test__/lifecycle?status=open&starts_at=2026-10-20T18:00:00Z&joined=1");
  expect(lifecycle.status()).toBe(204);
  await openList(page, request, `/game-requests/${requestId}`);
  await page.evaluate(() => {
    const browserWindow = window as TestWindow;
    browserWindow.__swapPrevented = [];
    document.addEventListener("htmx:beforeSwap", (event) => {
      const detail = (event as CustomEvent<{ target?: Element }>).detail;
      if (detail.target?.id === "game-request-detail") browserWindow.__swapPrevented?.push(event.defaultPrevented);
    });
  });
  await emit(page, "GameRequestJoined");
  await page.clock.runFor(200);
  await firstStarted;
  await emit(page, "GameRequestLeft");
  await page.clock.runFor(200);
  releaseFirst();
  await trailingStarted;
  await page.clock.runFor(20);
  await expect.poll(() => lobbyCalls).toBe(2);
  releaseTrailing();
  await page.clock.runFor(1000);
  const swapsPrevented = await page.evaluate(() => (window as TestWindow).__swapPrevented ?? []);
  expect(swapsPrevented).toEqual([false, false]);
  expect(detailCalls).toBe(2);
});
test("coalesces list events inside a fixed 200 ms window", async ({ page, request }) => {
  await openList(page, request);
  await emit(page, "GameRequestJoined");
  await page.clock.runFor(50);
  await emit(page, "GameRequestLeft");
  await page.clock.runFor(50);
  await emit(page, "GameRequestJoined");
  await page.clock.runFor(50);
  await emit(page, "GameRequestLeft");
  await page.clock.runFor(49);
  expect((await refreshProbe(page)).count).toBe(0);
  await page.clock.runFor(1);
  await expect.poll(async () => (await refreshProbe(page)).count).toBe(1);
});
test("keeps the latest Created highlight and active filter across a burst", async ({ page, request }) => {
  const refreshURLs: string[] = [];
  page.on("request", (request) => {
    const url = new URL(request.url());
    if (url.pathname === "/game-requests/refresh") refreshURLs.push(url.href);
  });
  await openList(page, request, "/game-requests?q=warcraft");
  await emit(page, "GameRequestCreated", "request-a");
  await emit(page, "GameRequestJoined", "request-b");
  await emit(page, "GameRequestCreated", "request-c");
  await page.clock.runFor(200);
  await expect.poll(() => refreshURLs.length).toBe(1);
  const refreshURL = new URL(refreshURLs[0]);
  expect(refreshURL.searchParams.get("q")).toBe("warcraft");
  expect(refreshURL.searchParams.get("highlight")).toBe("request-c");
});

test("keeps one pending list refresh while response remains active", async ({ page, request }) => {
  let release!: () => void;
  let notifyStarted!: () => void;
  let routeCalls = 0;
  const held = new Promise<void>((resolve) => { release = resolve; });
  const started = new Promise<void>((resolve) => { notifyStarted = resolve; });
  await page.route("**/game-requests/refresh*", async (route) => {
    if (routeCalls++ === 0) {
      notifyStarted();
      await held;
    }
    await route.continue();
  });
  await openList(page, request);
  await emit(page, "GameRequestJoined");
  await page.clock.runFor(200);
  await started;
  await emit(page, "GameRequestLeft");
  await page.clock.runFor(500);
  expect((await refreshProbe(page)).count).toBe(1);
  expect((await refreshProbe(page)).peak).toBe(1);
  release();
  await expect.poll(async () => (await refreshProbe(page)).count).toBe(2);
});

test("a failed refresh retries once after 1000 ms and stops after second failure", async ({ page, request }) => {
  let attempts = 0;
  await page.route("**/game-requests/refresh*", async (route) => {
    attempts++;
    await route.fulfill({ status: 503, body: "unavailable" });
  });
  await openList(page, request);
  await emit(page, "GameRequestJoined");
  await page.clock.runFor(200);
  await expect.poll(() => attempts).toBe(1);
  await expect.poll(async () => (await refreshProbe(page)).active).toBe(0);
  await page.clock.runFor(999);
  expect(attempts).toBe(1);
  await page.clock.runFor(1);
  await expect.poll(() => attempts).toBe(2);
  await page.clock.runFor(5000);
  expect(attempts).toBe(2);
});

test("keeps a pending refresh through hidden and pagehide suspension", async ({ page, request }) => {
  await openList(page, request);
  await emit(page, "GameRequestJoined");
  await page.clock.runFor(100);
  await page.evaluate(() => {
    Object.defineProperty(document, "hidden", { configurable: true, value: true });
    document.dispatchEvent(new Event("visibilitychange"));
  });
  await page.clock.runFor(500);
  expect((await refreshProbe(page)).count).toBe(0);
  await page.evaluate(() => {
    Object.defineProperty(document, "hidden", { configurable: true, value: false });
    document.dispatchEvent(new Event("visibilitychange"));
  });
  await page.clock.runFor(1);
  await expect.poll(async () => (await refreshProbe(page)).count).toBe(1);
  await page.clock.runFor(200);
  expect((await refreshProbe(page)).count).toBe(1);

  await emit(page, "GameRequestLeft");
  await page.clock.runFor(100);
  await page.evaluate(() => window.dispatchEvent(new PageTransitionEvent("pagehide")));
  await page.clock.runFor(500);
  expect((await refreshProbe(page)).count).toBe(1);
  await page.evaluate(() => window.dispatchEvent(new PageTransitionEvent("pageshow")));
  await page.clock.runFor(1);
  await expect.poll(async () => (await refreshProbe(page)).count).toBe(2);
});
test("retries an aborted refresh once", async ({ page, request }) => {
  let attempts = 0;
  await page.route("**/game-requests/refresh*", async (route) => {
    attempts++;
    if (attempts === 1) await route.abort();
    else await route.continue();
  });
  await openList(page, request);
  await emit(page, "GameRequestJoined");
  await page.clock.runFor(200);
  await expect.poll(() => attempts).toBe(1);
  await expect.poll(async () => (await refreshProbe(page)).active).toBe(0);
  await page.clock.runFor(999);
  expect(attempts).toBe(1);
  await page.clock.runFor(1);
  await expect.poll(() => attempts).toBe(2);
});
test("preserves a queued detail refresh when a mutation replaces its target", async ({ page, request }) => {
  let releaseRefresh!: () => void;
  let notifyRefresh!: () => void;
  let refreshCalls = 0;
  const held = new Promise<void>((resolve) => { releaseRefresh = resolve; });
  const started = new Promise<void>((resolve) => { notifyRefresh = resolve; });
  await page.route(`**/game-requests/${requestId}/refresh*`, async (route) => {
    refreshCalls++;
    if (refreshCalls === 1) {
      notifyRefresh();
      await held;
    }
    await route.continue();
  });
  await page.route("**/test/replace-detail", async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "text/html",
      body: `<section id="game-request-detail" data-game-request-detail data-game-request-id="${requestId}" data-refresh-url="/game-requests/${requestId}/refresh" data-replaced></section>`,
    });
  });
  await page.route(`**/game-requests/${requestId}`, async (route) => {
    const response = await route.fetch();
    const html = await response.text();
    await route.fulfill({
      response,
      body: html.replace("</body>", '<form hx-post="/test/replace-detail" hx-target="#game-request-detail" hx-swap="outerHTML"><button type="submit" data-test-replace-detail>Replace</button></form></body>'),
    });
  });
  await openList(page, request, `/game-requests/${requestId}`);
  await emit(page, "GameRequestJoined");
  await page.clock.runFor(200);
  await started;

  await page.locator("[data-test-replace-detail]").click();
  await expect(page.locator("#game-request-detail[data-replaced]")).toHaveAttribute("data-replaced", "");
  await emit(page, "GameRequestLeft");
  await page.clock.runFor(200);
  releaseRefresh();
  await page.clock.runFor(200);

  await expect.poll(() => refreshCalls).toBe(2);
});
