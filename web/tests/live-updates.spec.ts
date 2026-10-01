import { expect, test } from "@playwright/test";
import { htmxCounters, instrumentHtmx, resetFixture, waitForRealtime } from "./helpers";

// SSE updates arriving around an htmx swap were one of the suspected causes in
// issue #15, and Track A widened that surface: the list refresh now carries the
// active discovery filters, and the Trending rail is swapped out of band during
// the same refresh.

test.beforeEach(async ({ request }) => {
  await resetFixture(request);
});

// The capacity editor shares the detail refresh target. A refresh must still
// happen while editing, without throwing away the draft, its error or focus.
for (const scenario of ["draft", "validation error", "tab return"] as const) {
  test(`a detail refresh preserves the capacity ${scenario}`, async ({ page, request }) => {
    await page.goto("/game-requests/new?game_id=game-1");
    await page.locator("input[name='max_players']").fill("4");
    await page.getByRole("button", { name: "Create Game Request" }).click();
    await page.waitForURL(/\/game-requests\/[0-9a-f-]+/);

    const capacity = page.getByLabel("Maximum players");
    const value = scenario === "validation error" ? "1" : "6";
    await capacity.fill(value);
    if (scenario === "validation error") {
      await page.getByRole("button", { name: "Save" }).click();
      await expect(page.locator("#capacity-error")).toBeVisible();
    }
    await page.waitForLoadState("load");
    await capacity.focus();
    await waitForRealtime(page, request, 1);
    await instrumentHtmx(page);

    if (scenario === "tab return") {
      await page.evaluate(() => document.dispatchEvent(new Event("visibilitychange")));
    } else {
      const id = await page.locator("#game-request-detail").getAttribute("data-game-request-id");
      await request.post(`/__test__/publish?type=GameRequestJoined&request=${id}`);
    }

    await expect.poll(async () => (await htmxCounters(page)).after).toBeGreaterThan(0);
    await expect(capacity).toHaveValue(value);
    await expect(capacity).toBeFocused();
    if (scenario === "validation error") {
      await expect(page.locator("#capacity-error")).toBeVisible();
      await expect(capacity).toHaveAttribute("aria-invalid", "true");
    }
  });
}

test("a detail refresh updates an untouched capacity editor and removes a draft when cancelled", async ({ page, request }) => {
  await page.goto("/game-requests/new?game_id=game-1");
  await page.locator("input[name='max_players']").fill("4");
  await page.getByRole("button", { name: "Create Game Request" }).click();
  await page.waitForURL(/\/game-requests\/[0-9a-f-]+/);
  await waitForRealtime(page, request, 1);

  const detail = page.locator("#game-request-detail");
  const id = await detail.getAttribute("data-game-request-id");
  const csrf = await detail.locator("input[name='csrf_token']").first().inputValue();
  // A separate request changes the stored value; the untouched editor must
  // follow the resulting SSE refresh, rather than preserving a stale form.
  const changed = await page.request.post(`/game-requests/${id}/capacity`, {
    form: { max_players: "5", csrf_token: csrf },
  });
  expect(changed.ok()).toBe(true);
  const capacity = page.getByLabel("Maximum players");
  await expect(capacity).toHaveValue("5");

  await capacity.fill("6");
  const cancelled = await page.request.post(`/game-requests/${id}/cancel`, {
    form: { csrf_token: csrf },
  });
  expect(cancelled.ok()).toBe(true);
  await expect(capacity).toHaveCount(0);
  await expect(page.getByText("Cancelled", { exact: true }).first()).toBeVisible();
});

test("a live update refreshes the list without disturbing the page", async ({ page, request }) => {
  await page.goto("/game-requests");
  await waitForRealtime(page, request, 1);
  await instrumentHtmx(page);

  await request.post("/__test__/publish?type=GameRequestJoined");

  // The stream signal has to turn into a real fetch. Comparing the rendered
  // "updated" stamp instead would pass or fail on whether the refresh happened
  // to land in the same second.
  await expect
    .poll(async () => (await htmxCounters(page)).after, { timeout: 10_000 })
    .toBeGreaterThan(0);
  await expect(page.locator("#game-request-list")).toBeVisible();
  await expect(page.locator("article[data-game-request-id]")).toHaveCount(3);
});

// A live update must not silently widen the player's view back to everything.
// The list carries its filters in data-filter-query for exactly this reason.
test("a live update keeps the active discovery filter", async ({ page, request }) => {
  await page.goto("/game-requests?q=warcraft");
  await waitForRealtime(page, request, 1);
  await expect(page.locator("article[data-game-request-id]")).toHaveCount(1);

  await request.post("/__test__/publish?type=GameRequestJoined");
  await page.waitForTimeout(1000);

  await expect(page.locator("article[data-game-request-id]")).toHaveCount(1);
  await expect(page.locator("#game-request-list")).toHaveAttribute("data-filter-query", /q=warcraft/);
  await expect(page.locator("#request-search")).toHaveValue("warcraft");
});

// The Trending rail rides along on the same refresh as an out-of-band swap.
// An out-of-band target that stops matching silently drops the swap, so the
// rail has to keep its id through a live update.
test("a live update keeps the out-of-band Trending rail", async ({ page, request }) => {
  await page.goto("/game-requests");
  await waitForRealtime(page, request, 1);

  await request.post("/__test__/publish?type=GameRequestJoined");
  await page.waitForTimeout(1000);

  await expect(page.locator("#trending-rail")).toHaveCount(1);
  await expect(page.locator("#trending-rail")).toContainText("last 6 hours");
});

// The interaction the original bug report described: act, and have a live
// update land in the middle of it.
test("a live update arriving during a mutation leaves htmx balanced", async ({ page, request }) => {
  await request.post("/__test__/mode?delay=500");
  await page.goto("/game-requests");
  await waitForRealtime(page, request, 1);
  await instrumentHtmx(page);

  const row = page.locator("article[data-game-request-id]").first();
  await row.getByRole("button", { name: /Join|Leave/ }).click({ noWaitAfter: true });
  await request.post("/__test__/publish?type=GameRequestJoined");
  await request.post("/__test__/publish?type=GameRequestLeft");

  await expect
    .poll(async () => {
      const counters = await htmxCounters(page);
      return counters.before > 0 && counters.before === counters.after;
    }, { timeout: 15_000 })
    .toBe(true);

  await expect(page.locator("#game-request-list")).toBeVisible();
  await request.post("/__test__/mode?delay=0");
});
