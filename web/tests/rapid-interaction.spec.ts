import { expect, test } from "@playwright/test";
import { fixtureState, htmxCounters, instrumentHtmx, resetFixture } from "./helpers";

// Issue #15 turned up a second defect while the first was being hardened: six
// rapid clicks on Create Game Request produced six POSTs and five Game
// Requests. The guard that prevents it is in web/src/app.ts, and the server
// would still accept duplicates, so this test is the only thing standing
// between a refactor and five identical Game Requests.

test.beforeEach(async ({ request }) => {
  await resetFixture(request);
});

// Chromium refuses to submit the same form twice while a submission is already
// in flight, so this passes even with app.ts's guard removed. It is kept as a
// backstop against a future change that makes creation an htmx request — where
// the browser would no longer protect it — not as proof that the guard works.
// The test that exercises a guard the application actually owns is the htmx
// burst below.
test("rapid clicks on Create Game Request create exactly one", async ({ page, request }) => {
  await page.goto("/game-requests/new?game_id=game-1");

  // The clicks are dispatched inside one task in the page rather than driven
  // one at a time over the protocol. Driving them from the test would let the
  // navigation start between clicks, so every click after the first would land
  // on a page that is already leaving and the burst would never happen.
  await page.evaluate(() => {
    const button = document.querySelector<HTMLButtonElement>("form[action='/game-requests'] button[type=submit]");
    for (let index = 0; index < 6; index += 1) button?.click();
  });
  await page.waitForURL(/\/game-requests\/[0-9a-f-]+/, { timeout: 15_000 });

  expect((await fixtureState(request)).creates).toBe(1);
});

// A page restored from the back/forward cache keeps the DOM it was frozen
// with, including a submit guard whose navigation finished long ago. Without
// the pageshow handler in app.ts the form comes back permanently disabled and
// the player cannot create anything until they reload — the same "stuck UI"
// symptom issue #15 was opened for, reached a different way.
test("going back to a submitted form leaves it usable", async ({ page }) => {
  await page.goto("/game-requests/new?game_id=game-1");
  await page.getByRole("button", { name: "Create Game Request" }).click();
  await page.waitForURL(/\/game-requests\/[0-9a-f-]+/);

  await page.goBack();

  const submit = page.getByRole("button", { name: "Create Game Request" });
  await expect(submit).toBeEnabled();
  await expect(page.locator("form[data-submitting='true']")).toHaveCount(0);

  // And it still works: the guard was released, not merely repainted -- the
  // bfcache-restored page still carries the same selected game (Warcraft III)
  // in its hidden identity field, so no reselection is needed.
  await submit.click();
  await page.waitForURL(/\/game-requests\/[0-9a-f-]+/, { timeout: 15_000 });
});

// A burst of clicks on Join, asserted as a request count rather than as the
// resulting membership: the server resolves duplicates to one winner either
// way, so counting members would hide a client that fired six times.
//
// Measured: this also passes with hx-sync and hx-disabled-elt removed, because
// htmx already queues per element. Like the creation test above it is a
// backstop for a future change that removes that protection, not proof that
// the attributes in the template are doing the work.
test("a burst of Join clicks issues exactly one request", async ({ page }) => {
  await page.goto("/game-requests");

  const posts: string[] = [];
  page.on("request", (sent) => {
    if (sent.method() === "POST" && sent.url().includes("/join")) posts.push(sent.url());
  });

  await page.evaluate(() => {
    const button = document.querySelector<HTMLButtonElement>(
      "article[data-game-request-id] form[action$='/join'] button[type=submit]",
    );
    for (let index = 0; index < 6; index += 1) button?.click();
  });
  await page.waitForTimeout(1500);

  expect(posts).toHaveLength(1);
});

// Join and Leave are htmx swaps rather than navigations, and both carry
// hx-sync and hx-disabled-elt. Alternating them quickly is the stress case the
// issue asked for: every request must resolve, and the row must end in a state
// that matches what the server actually recorded.
test("alternating Join and Leave leaves the row consistent", async ({ page }) => {
  await page.goto("/game-requests");
  await instrumentHtmx(page);

  const row = page.locator("article[data-game-request-id]").first();
  for (let round = 0; round < 4; round += 1) {
    const action = row.getByRole("button", { name: /Join|Leave/ });
    await action.click({ noWaitAfter: true });
    await page.waitForTimeout(150);
  }

  // Whatever the final state, the control is usable and not stuck pending.
  const finalAction = row.getByRole("button", { name: /Join|Leave/ });
  await expect(finalAction).toBeEnabled({ timeout: 10_000 });

  await expect
    .poll(async () => {
      const counters = await htmxCounters(page);
      return counters.before === counters.after;
    }, { timeout: 10_000 })
    .toBe(true);
});
