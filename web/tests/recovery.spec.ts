import { expect, test } from "@playwright/test";
import { htmxCounters, instrumentHtmx, resetFixture } from "./helpers";

// "A failed request must not leave the application unusable" is an acceptance
// criterion of issue #15. Every failure mode has to release pending UI state
// and leave the page usable without a reload.

test.beforeEach(async ({ request }) => {
  await resetFixture(request);
});

test("a conflicting Join explains itself and leaves the page usable", async ({ page, request }) => {
  await page.goto("/game-requests");
  await request.post("/__test__/mode?fail=conflict");

  const row = page.locator("article[data-game-request-id]").first();
  await row.getByRole("button", { name: "Join" }).click();

  // The conflict is retargeted into the live notice region rather than
  // replacing the row with an error.
  await expect(page.locator("#live-notices")).toContainText("already joined");
  await expect(page.locator("[data-dismiss-live-toast]").first()).toBeVisible();

  // The row survived and can still be used.
  await expect(row.getByRole("button", { name: "Join" })).toBeEnabled();
});

test("a server error surfaces a dismissible notice instead of freezing", async ({ page, request }) => {
  await page.goto("/game-requests");
  await request.post("/__test__/mode?fail=error");

  const row = page.locator("article[data-game-request-id]").first();
  await row.getByRole("button", { name: "Join" }).click();

  const notice = page.locator("#live-notices [data-live-toast]").first();
  await expect(notice).toBeVisible();
  await notice.getByRole("button", { name: "Dismiss" }).click();
  await expect(notice).toBeHidden();

  // A second action still works, so the failure did not wedge the page.
  await expect(row.getByRole("button", { name: "Join" })).toBeEnabled();
});

// Issue #119: toasts dismiss themselves after 10s, shown by a shrinking bar.
test("a notice auto-dismisses after 10 seconds with a countdown bar", async ({ page, request }) => {
  await page.goto("/game-requests");
  await request.post("/__test__/mode?fail=error");
  await page.locator("article[data-game-request-id]").first().getByRole("button", { name: "Join" }).click();

  const notice = page.locator("#live-notices [data-live-toast]").first();
  await expect(notice).toBeVisible();
  await page.waitForTimeout(2000);
  const scale = await notice.evaluate((el) => new DOMMatrix(getComputedStyle(el, "::after").transform).a);
  expect(scale).toBeLessThan(0.9);
  expect(scale).toBeGreaterThan(0.5);
  await expect(notice).toBeHidden({ timeout: 10_000 });
  await expect(page.locator("#live-notices [data-live-toast]")).toHaveCount(0);
});

// An aborted request is the case hx-sync produces on every debounced keystroke.
// htmx fires afterRequest for an abort, and app.ts relies on that to release
// its in-flight counter; if it ever stopped, live refreshes would be deferred
// forever and the UI would look frozen exactly as issue #15 described.
test("aborted requests still balance htmx lifecycle events", async ({ page, request }) => {
  await request.post("/__test__/mode?delay=400");
  await page.goto("/game-requests");
  await instrumentHtmx(page);

  const search = page.locator("#request-search");
  for (const term of ["w", "wa", "war", "warc"]) {
    await search.fill(term);
    await page.waitForTimeout(320);
  }

  await expect
    .poll(async () => await htmxCounters(page), { timeout: 15_000 })
    .toEqual(expect.objectContaining({}));
  const counters = await htmxCounters(page);
  expect(counters.before).toBeGreaterThan(0);
  expect(counters.after).toBe(counters.before);
  await request.post("/__test__/mode?delay=0");
});
