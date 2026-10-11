import { test, expect, chromium } from "@playwright/test";
import { readFile } from "node:fs/promises";
import { homedir } from "node:os";
import { join } from "node:path";

declare global {
  interface Window {
    __sseOpen?: boolean;
    __sseCreated?: number;
    __sseErrors?: number;
    __events?: number;
  }
}

test("isolated app exposes authenticated list and native SSE", async () => {
  // Allow a bounded navigation and an independent SSE handshake, without
  // letting a hung HTTP request consume Playwright's entire default timeout.
  test.setTimeout(45_000);
  const baseURL = process.env.BASE_URL ?? "http://127.0.0.1:18080";
  if (!/^http:\/\/(127\.0\.0\.1:(18080|18081)|lansurfer:8080|proxy:8080)$/.test(baseURL)) throw new Error("BASE_URL is not allowed");
  const sessionsPath = process.env.LANSURFER_LOADTEST_SESSIONS_FILE ?? join(homedir(), "lansurfer-loadtest-sessions.jsonl");
  const [line] = (await readFile(sessionsPath, "utf8")).trim().split("\n");
  const session = JSON.parse(line).session_token;
  if (typeof session !== "string" || !session) throw new Error("session file has no session token");
  const browser = await chromium.launch({ headless: true });
  try {
    const context = await browser.newContext();
    await context.addCookies([{ name: "lansurfer_session", value: session, url: baseURL, httpOnly: true, sameSite: "Lax" }]);
    await context.addInitScript(() => {
      const Native = window.EventSource;
      window.EventSource = class extends Native {
        constructor(...args: ConstructorParameters<typeof Native>) {
          super(...args);
          window.__sseCreated = (window.__sseCreated ?? 0) + 1;
          this.addEventListener("open", () => { window.__sseOpen = true; });
          this.addEventListener("error", () => { window.__sseErrors = (window.__sseErrors ?? 0) + 1; });
          for (const type of ["game-request", "game-request-chat", "game-request-activity"]) this.addEventListener(type, () => { window.__events = (window.__events ?? 0) + 1; });
        }
      };
    });
    const page = await context.newPage();
    const sseStatuses: number[] = [];
    let pageErrors = 0;
    page.on("pageerror", () => { pageErrors++; });
    page.on("response", (response) => {
      // Only record status codes. Never log session cookies or request headers.
      if (new URL(response.url()).pathname === "/events/game-requests") sseStatuses.push(response.status());
    });

    // An unfiltered list can be tens of MB after a long soak. This smoke test
    // verifies the authenticated list shell and native SSE, not unbounded-list
    // capacity (which is tracked as a separate product finding).
    const listURL = `${baseURL}/game-requests/?q=loadtest-no-match`;
    const response = await page.goto(listURL, { waitUntil: "domcontentloaded", timeout: 20_000 });
    expect(response?.status(), "authenticated list should answer HTTP 200").toBe(200);
    await expect(page, "session must not redirect to sign-in").toHaveURL(listURL);
    const list = page.locator("[data-game-request-list]");
    await expect(list).toBeVisible();
    await expect(list.locator("[data-game-request-id]")).toHaveCount(0);
    await expect(page.locator("[data-realtime-url]")).toHaveAttribute("data-realtime-url", "/events/game-requests");

    await expect.poll(async () => ({
      ...(await page.evaluate(() => ({
        open: window.__sseOpen === true,
        constructed: window.__sseCreated ?? 0,
        errors: window.__sseErrors ?? 0,
      }))),
      sseStatuses: [...sseStatuses],
      pageErrors,
    }), { timeout: 12_000, message: "native SSE must open on an authenticated, filtered Game Request page" })
      .toMatchObject({ open: true });
    await context.close();
  } finally {
    await browser.close();
  }
});
