import { expect, test, type Page } from "@playwright/test";
import { expectStreams, resetFixture, waitForRealtime } from "./helpers";

const sessions = (session: string) => ({
  cookies: [{
    name: "lansurfer_session",
    value: session,
    domain: "127.0.0.1",
    path: "/",
    expires: -1,
    httpOnly: true,
    secure: false,
    sameSite: "Lax" as const,
  }],
  origins: [],
});

test("an offline lobby converges after reconnect without another domain event", async ({ browser, baseURL }) => {
  const admin = await browser.newContext({ baseURL, storageState: sessions("browser-test-session") });
  const other = await browser.newContext({ baseURL, storageState: sessions("browser-test-other") });
  const adminPage = await admin.newPage();
  const otherPage = await other.newPage();
  const reconnect = { states: [] as string[], instances: 0, rejected: [] as string[], failed: [] as string[] };
  let detailRefreshes = 0;
  try {
    await adminPage.addInitScript(() => {
      const state = window as Window & { __reconnect?: { states: string[]; instances: number; rejected: string[] } };
      state.__reconnect = { states: [], instances: 0, rejected: [] };
      const NativeEventSource = window.EventSource;
      window.EventSource = class extends NativeEventSource {
        constructor(url: string | URL, options?: EventSourceInit) {
          super(url, options);
          if (state.__reconnect) state.__reconnect.instances += 1;
          this.addEventListener("open", () => state.__reconnect?.states.push("open"));
          this.addEventListener("error", () => state.__reconnect?.states.push("error"));
        }
      };
      window.addEventListener("unhandledrejection", (event) => {
        state.__reconnect?.rejected.push(String(event.reason));
      });
    });
    adminPage.on("request", (request) => {
      if (/\/game-requests\/[^/]+\/refresh(?:\?|$)/.test(request.url())) detailRefreshes += 1;
    });
    adminPage.on("requestfailed", (request) => {
      if (/\/lobby(?:\?|$)|session-information/.test(request.url())) reconnect.failed.push(`${request.method()} ${request.url()}: ${request.failure()?.errorText}`);
    });
    adminPage.on("response", (response) => {
      if (response.status() >= 400 && /\/lobby(?:\?|$)|session-information/.test(response.url())) reconnect.failed.push(`${response.status()} ${response.url()}`);
    });

    await resetFixture(adminPage.request);
    await adminPage.goto("/game-requests/new?game_id=game-1");
    await adminPage.getByRole("button", { name: "Create Game Request" }).click();
    await adminPage.waitForURL(/\/game-requests\/[0-9a-f-]+/);
    const requestID = await adminPage.locator("[data-game-request-detail]").getAttribute("data-game-request-id");
    expect(requestID).not.toBeNull();
    await otherPage.goto(`/game-requests/${requestID}`);
    await otherPage.getByRole("button", { name: "Join", exact: true }).click();
    await expect(otherPage.getByRole("button", { name: "Leave", exact: true })).toBeVisible();
    await expectStreams(adminPage.request, 2);
    await expect(adminPage.locator("#game-request-lobby")).toContainText("created the request");
    await adminPage.waitForTimeout(250);
    await expect(adminPage.locator(".htmx-request")).toHaveCount(0);

    const refreshCountBeforeOffline = detailRefreshes;
    await admin.setOffline(true);
    await expect.poll(() => adminPage.evaluate(() => navigator.onLine)).toBe(false);
    await expect.poll(async () => adminPage.evaluate(async () => {
      try {
        await fetch("/__test__/state");
        return true;
      } catch {
        return false;
      }
    })).toBe(false);
    const message = `offline chat ${Date.now()}`;
    await otherPage.getByRole("textbox", { name: "Message the lobby" }).fill(message);
    await otherPage.getByRole("button", { name: "Send", exact: true }).click();
    await expect(otherPage.locator("#game-request-lobby .chat-message")).toContainText(message);
    expect(await adminPage.locator("#game-request-lobby").innerText()).not.toContain(message);


    await admin.setOffline(false);
    await expect.poll(() => adminPage.evaluate(() => navigator.onLine)).toBe(true);
    await expectStreams(adminPage.request, 2);
    await expect.poll(() => detailRefreshes).toBeGreaterThan(refreshCountBeforeOffline);
    await expect(adminPage.locator("#game-request-lobby .chat-message")).toContainText(message, { timeout: 10_000 });

    reconnect.states = await adminPage.evaluate(() => (window as Window & { __reconnect?: { states: string[] } }).__reconnect?.states ?? []);
    reconnect.instances = await adminPage.evaluate(() => (window as Window & { __reconnect?: { instances: number } }).__reconnect?.instances ?? 0);
    reconnect.rejected = await adminPage.evaluate(() => (window as Window & { __reconnect?: { rejected: string[] } }).__reconnect?.rejected ?? []);
    expect(reconnect.states).toEqual(["open"]);
    expect(reconnect.instances).toBe(1);
    expect(reconnect.rejected).toEqual([]);
    expect(reconnect.failed.every((failure) => /ERR_INTERNET_DISCONNECTED/.test(failure) && /\/lobby(?:\?|:|$)|session-information/.test(failure))).toBe(true);
  } finally {
    await admin.close();
    await other.close();
  }
});
type EventProbeWindow = Window & { __eventProbe?: { source?: EventSource; unhandled: string[] } };

async function instrumentEventProbe(page: Page): Promise<void> {
  await page.addInitScript(() => {
    const state = window as EventProbeWindow;
    state.__eventProbe = { unhandled: [] };
    const NativeEventSource = window.EventSource;
    window.EventSource = class extends NativeEventSource {
      constructor(...args: ConstructorParameters<typeof EventSource>) {
        super(...args);
        if (state.__eventProbe) state.__eventProbe.source = this;
      }
    };
    window.addEventListener("unhandledrejection", (event) => {
      state.__eventProbe?.unhandled.push(String(event.reason));
    });
  });
}

async function emitLobbySignal(page: Page, requestID: string): Promise<void> {
  await page.evaluate((id) => {
    const source = (window as EventProbeWindow).__eventProbe?.source;
    if (!source) throw new Error("EventSource was not recorded");
    source.dispatchEvent(new MessageEvent("game-request-chat", { data: JSON.stringify({ requestId: id }) }));
  }, requestID);
}

test("a failed offline Lobby refresh is handled and later state converges", async ({ browser, baseURL }) => {
  const admin = await browser.newContext({ baseURL, storageState: sessions("browser-test-session") });
  const other = await browser.newContext({ baseURL, storageState: sessions("browser-test-other") });
  const adminPage = await admin.newPage();
  const otherPage = await other.newPage();
  const failedLobbyRequests: string[] = [];
  try {
    await instrumentEventProbe(adminPage);
    adminPage.on("requestfailed", (request) => {
      if (/\/lobby(?:\?|$)/.test(request.url())) failedLobbyRequests.push(`${request.url()}: ${request.failure()?.errorText}`);
    });
    await resetFixture(adminPage.request);
    await adminPage.goto("/game-requests/new?game_id=game-1");
    await adminPage.getByRole("button", { name: "Create Game Request" }).click();
    await adminPage.waitForURL(/\/game-requests\/[0-9a-f-]+/);
    const requestID = await adminPage.locator("[data-game-request-detail]").getAttribute("data-game-request-id");
    if (!requestID) throw new Error("Game Request ID was not rendered");
    await otherPage.goto(`/game-requests/${requestID}`);
    await otherPage.getByRole("button", { name: "Join", exact: true }).click();
    await expect(otherPage.getByRole("button", { name: "Leave", exact: true })).toBeVisible();
    await expect(adminPage.locator("#game-request-lobby")).toContainText("created the request");
    await expectStreams(adminPage.request, 2);
    await adminPage.waitForTimeout(250);
    await expect(adminPage.locator(".htmx-request")).toHaveCount(0);

    const message = `offline refresh ${Date.now()}`;
    await admin.setOffline(true);
    await expect.poll(() => adminPage.evaluate(() => navigator.onLine)).toBe(false);
    await otherPage.getByRole("textbox", { name: "Message the lobby" }).fill(message);
    await otherPage.getByRole("button", { name: "Send", exact: true }).click();
    await expect(otherPage.locator("#game-request-lobby .chat-message")).toContainText(message);
    await expect.poll(() => failedLobbyRequests.length).toBe(1);
    expect(failedLobbyRequests[0]).toContain("ERR_INTERNET_DISCONNECTED");
    expect(await adminPage.evaluate(() => (window as EventProbeWindow).__eventProbe?.unhandled)).toEqual([]);

    await admin.setOffline(false);
    await expect.poll(() => adminPage.evaluate(() => navigator.onLine)).toBe(true);
    await expect(adminPage.locator("#game-request-lobby .chat-message")).toContainText(message, { timeout: 10_000 });
    expect(await adminPage.evaluate(() => (window as EventProbeWindow).__eventProbe?.unhandled)).toEqual([]);
  } finally {
    await admin.close();
    await other.close();
  }
});

test("an online Lobby refresh failure remains observable", async ({ page, request }) => {
  await resetFixture(request);
  await instrumentEventProbe(page);
  const failedLobbyRequests: string[] = [];
  page.on("requestfailed", (failed) => {
    if (/\/lobby(?:\?|$)/.test(failed.url())) failedLobbyRequests.push(failed.failure()?.errorText ?? "");
  });
  await page.goto("/game-requests/new?game_id=game-1");
  await page.getByRole("button", { name: "Create Game Request" }).click();
  await page.waitForURL(/\/game-requests\/[0-9a-f-]+/);
  const requestID = await page.locator("[data-game-request-detail]").getAttribute("data-game-request-id");
  if (!requestID) throw new Error("Game Request ID was not rendered");
  await waitForRealtime(page, request, 1);
  await expect(page.locator("#game-request-lobby")).toContainText("created the request");
  await page.waitForTimeout(250);
  await expect(page.locator(".htmx-request")).toHaveCount(0);
  expect(await page.evaluate(() => navigator.onLine)).toBe(true);
  await page.route(`**/game-requests/${requestID}/lobby*`, (route) => route.abort("failed"));

  await emitLobbySignal(page, requestID);
  await expect.poll(() => failedLobbyRequests.length).toBe(1);
  await expect.poll(() => page.evaluate(() => (window as EventProbeWindow).__eventProbe?.unhandled ?? [])).toEqual(["undefined"]);
});
