import { test, expect, chromium } from "@playwright/test";
import { readFile } from "node:fs/promises";
import { homedir } from "node:os";
import { join } from "node:path";

declare global {
  interface Window {
    __sseOpen?: boolean;
    __sseErrors?: number;
    __events?: number;
  }
}

test("isolated app exposes authenticated list and native SSE", async () => {
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
          this.addEventListener("open", () => { window.__sseOpen = true; });
          this.addEventListener("error", () => { window.__sseErrors = (window.__sseErrors ?? 0) + 1; });
          for (const type of ["game-request", "game-request-chat", "game-request-activity"]) this.addEventListener(type, () => { window.__events = (window.__events ?? 0) + 1; });
        }
      };
    });
    const page = await context.newPage();
    await page.goto(`${baseURL}/game-requests/`, { waitUntil: "domcontentloaded" });
    await expect(page.locator("[data-game-request-list]")).toBeVisible();
    await page.waitForFunction(() => window.__sseOpen === true, { timeout: 20_000 });
    await context.close();
  } finally {
    await browser.close();
  }
});
