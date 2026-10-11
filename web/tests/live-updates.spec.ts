
declare global {
  interface Window {
    __copiedSecret?: string;
  }
}

import { expect, test } from "@playwright/test";
import { htmxCounters, instrumentHtmx, resetFixture, waitForRealtime } from "./helpers";

// SSE updates arriving around an htmx swap were one of the suspected causes in
// issue #15, and discovery filtering widened that surface: the list refresh now carries the
// active discovery filters, and the Trending rail is swapped out of band during
// the same refresh.


test.beforeEach(async ({ request }) => {
  await resetFixture(request);
});
test("normal session information copy writes its exact value and gives generic feedback", async ({ page }) => {
  const pageErrors: string[] = [];
  page.on("pageerror", error => pageErrors.push(error.message));
  await page.addInitScript(() => {
    Object.defineProperty(Navigator.prototype, "clipboard", {
      configurable: true,
      get: () => ({ writeText: (text: string) => { window.__copiedSecret = text; return Promise.resolve(); } }),
    });
  });
  await page.goto("/game-requests/new?game_id=game-1");
  await page.getByRole("button", { name: "Create Game Request" }).click();
  await page.waitForURL(/\/game-requests\/[0-9a-f-]+/);
  await page.getByRole("link", { name: "Add", exact: true }).click();
  await page.getByRole("textbox", { name: "Key" }).fill("Server");
  await page.getByRole("textbox", { name: "Value" }).fill("10.20.30.40:27015");
  await page.getByRole("button", { name: "Save" }).click();
  const copy = page.locator("#session-information").getByRole("button", { name: "Copy Server" });
  await expect(copy.locator("i")).toHaveClass(/ph-copy/);
  await copy.click();
  await expect.poll(() => page.evaluate(() => window.__copiedSecret)).toBe("10.20.30.40:27015");
  await expect(page.locator("#live-notices")).toContainText("Value copied.");
  await expect(page.locator("#live-notices")).not.toContainText("10.20.30.40:27015");
  expect(pageErrors).toEqual([]);
});

// Detail refreshes retain edits and validation messages in the shared Host editor.
for (const scenario of ["draft", "validation error", "tab return"] as const) {
  test(`a detail refresh preserves the request edit ${scenario}`, async ({ page, request }) => {
    await page.goto("/game-requests/new?game_id=game-1");
    await page.locator("input[name='max_players']").fill("4");
    await page.getByRole("button", { name: "Create Game Request" }).click();
    await page.waitForURL(/\/game-requests\/[0-9a-f-]+(?:\?.*)?$/);
    await page.getByRole("link", { name: "Edit" }).click();
    await expect(page.locator("#game-request-edit-dialog")).toBeVisible();
    const capacity = page.getByLabel("Max players");
    const value = scenario === "validation error" ? "1" : "6";
    await capacity.fill(value);
    if (scenario === "validation error") {
      await page.getByRole("button", { name: "Save" }).click();
      await expect(page.locator("#max-players-error")).toBeVisible();
      await expect(capacity).toHaveValue("1");
      await expect(capacity).toBeFocused();
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
    await expect(page.locator("#game-request-edit-dialog")).toHaveCount(1);
    await expect(capacity).toHaveValue(value);
    await expect(capacity).toBeFocused();
    if (scenario === "validation error") {
      await expect(page.locator("#max-players-error")).toBeVisible();
      await expect(capacity).toHaveAttribute("aria-invalid", "true");
    }
    const firstRefreshCount = (await htmxCounters(page)).after;
    const id = await page.locator("#game-request-detail").getAttribute("data-game-request-id");
    await request.post(`/__test__/publish?type=GameRequestJoined&request=${id}`);
    await expect.poll(async () => (await htmxCounters(page)).after).toBeGreaterThan(firstRefreshCount);
    await expect(page.locator("#game-request-edit-dialog")).toHaveCount(1);
    await expect(capacity).toHaveValue(value);
    await page.keyboard.press("Escape");
    await page.getByRole("link", { name: "Edit" }).click();
    await expect(page.locator("#game-request-edit-dialog")).toBeVisible();
  });
}

for (const scenario of ["description draft", "capacity draft"] as const) {
  test(`a detail refresh keeps only the dirty field for ${scenario}`, async ({ page }) => {
    await page.goto("/game-requests/new?game_id=game-1");
    await page.locator("input[name='max_players']").fill("4");
    await page.getByRole("button", { name: "Create Game Request" }).click();
    await page.waitForURL(/\/game-requests\/[0-9a-f-]+(?:\?.*)?$/);
    await page.getByRole("link", { name: "Edit" }).click();
    await expect(page.locator("#game-request-edit-dialog")).toBeVisible();
    await waitForRealtime(page, page.request, 1);
    await instrumentHtmx(page);

    const detail = page.locator("#game-request-detail");
    const id = await detail.getAttribute("data-game-request-id");
    const csrf = await page.locator("#game-request-edit-dialog form[action$='/details'] input[name='csrf_token']").inputValue();
    const capacity = page.getByLabel("Max players");
    const description = page.getByLabel("Description");
    if (scenario === "description draft") await description.fill("A😀B");
    else await capacity.fill("6");

    const serverUpdate = await page.request.post(`/game-requests/${id}/details`, {
      form: {
        max_players: scenario === "description draft" ? "6" : "4",
        start_date: await page.locator("input[name='start_date']").inputValue(),
        start_time: await page.locator("input[name='start_time']").inputValue(),
        timezone: await page.locator("input[name='timezone']").inputValue(),
        description: scenario === "description draft" ? "" : "Server description",
        csrf_token: csrf,
      },
    });
    expect(serverUpdate.ok()).toBe(true);
    await expect.poll(async () => (await htmxCounters(page)).after).toBeGreaterThan(0);

    if (scenario === "description draft") {
      await expect(description).toHaveValue("A😀B");
      await expect(description.locator("xpath=..").locator("[data-description-count]")).toHaveText("3 / 1000");
      await description.fill("A😀BC");
      await expect(description.locator("xpath=..").locator("[data-description-count]")).toHaveText("4 / 1000");
      await expect(capacity).toHaveValue("4");
    } else {
      await expect(capacity).toHaveValue("6");
      await expect(description).toHaveValue("");
    }
    await page.getByRole("button", { name: "Save" }).click();
    await expect(page.locator("#game-request-edit-dialog")).toBeHidden();
    await expect(page.locator("#game-request-detail")).toContainText(scenario === "description draft" ? "1/4" : "1/6");
    await expect(page.locator("#game-request-detail")).toContainText(scenario === "description draft" ? "A😀BC" : "No description.");
  });
}

test("a detail refresh preserves dirty custom start date and time", async ({ page, request }) => {
  await page.goto("/game-requests/new?game_id=game-1");
  await page.getByLabel("Max players").fill("4");
  await page.getByRole("button", { name: "Create Game Request" }).click();
  await page.waitForURL(/\/game-requests\/[0-9a-f-]+(?:\?.*)?$/);
  await page.getByRole("link", { name: "Edit" }).click();
  await expect(page.locator("#game-request-edit-dialog")).toBeVisible();
  await page.getByText("Pick date & time").click();
  const date = page.locator("input[name='start_date']");
  const time = page.locator("input[name='start_time']");
  await date.fill("2099-05-02");
  await time.fill("20:15");
  await waitForRealtime(page, request, 1);
  await instrumentHtmx(page);
  const id = await page.locator("#game-request-detail").getAttribute("data-game-request-id");
  await request.post(`/__test__/publish?type=GameRequestJoined&request=${id}`);
  await expect.poll(async () => (await htmxCounters(page)).after).toBeGreaterThan(0);
  await expect(page.locator("input[name='start_preset'][value='custom']")).toBeChecked();
  await expect(date).toHaveValue("2099-05-02");
  await expect(time).toHaveValue("20:15");
  const expectedStart = await page.evaluate(() => {
    const [year, month, day] = (document.querySelector<HTMLInputElement>("input[name='start_date']")!.value).split("-").map(Number);
    const [hour, minute] = (document.querySelector<HTMLInputElement>("input[name='start_time']")!.value).split(":").map(Number);
    return new Date(year, month - 1, day, hour, minute).toISOString().replace(".000Z", "Z");
  });
  await page.getByRole("button", { name: "Save" }).click();
  await expect(page.locator("#game-request-edit-dialog")).toBeHidden();
  await expect(page.locator("#game-request-detail time[data-local-time='start']")).toHaveAttribute("datetime", expectedStart);
});

test.describe("Berlin custom-start refresh", () => {
  test.use({ timezoneId: "Europe/Berlin" });

  test("preserves a dirty time that matches the raw UTC default", async ({ page, request }) => {
    await page.goto("/game-requests/new?game_id=game-1");
    await page.getByLabel("Max players").fill("4");
    await page.getByRole("button", { name: "Create Game Request" }).click();
    await page.waitForURL(/\/game-requests\/[0-9a-f-]+(?:\?.*)?$/);
    await page.getByRole("link", { name: "Edit" }).click();
    await expect(page.locator("#game-request-edit-dialog")).toBeVisible();
    await page.getByText("Pick date & time").click();
    await page.locator("input[name='start_date']").fill("2099-05-02");
    await page.locator("input[name='start_time']").fill("20:30");
    await page.getByRole("button", { name: "Save" }).click();
    await expect(page.locator("#game-request-edit-dialog")).toBeHidden();

    await page.getByRole("link", { name: "Edit" }).click();
    await expect(page.locator("#game-request-edit-dialog")).toBeVisible();
    const time = page.locator("input[name='start_time']");
    await expect(time).toHaveValue("20:30");
    await time.fill("18:30");
    await waitForRealtime(page, request, 1);
    await instrumentHtmx(page);
    const id = await page.locator("#game-request-detail").getAttribute("data-game-request-id");
    await request.post(`/__test__/publish?type=GameRequestJoined&request=${id}`);
    await expect.poll(async () => (await htmxCounters(page)).after).toBeGreaterThan(0);
    await expect(time).toHaveValue("18:30");
    await page.getByRole("button", { name: "Save" }).click();
    await expect(page.locator("#game-request-edit-dialog")).toBeHidden();
    await expect(page.locator("#game-request-detail time[data-local-time='start']")).toHaveAttribute(
      "datetime",
      "2099-05-02T16:30:00Z",
    );
  });
});
test("number inputs inserted by a detail refresh clamp immediately", async ({ page, request }) => {
  await page.goto("/game-requests/new?game_id=game-1");
  await page.getByLabel("Max players").fill("4");
  await page.getByRole("button", { name: "Create Game Request" }).click();
  await page.waitForURL(/\/game-requests\/[0-9a-f-]+(?:\?.*)?$/);
  await page.getByRole("link", { name: "Edit" }).click();
  await expect(page.locator("#game-request-edit-dialog")).toBeVisible();
  await waitForRealtime(page, request, 1);
  await instrumentHtmx(page);
  const id = await page.locator("#game-request-detail").getAttribute("data-game-request-id");
  await request.post(`/__test__/publish?type=GameRequestJoined&request=${id}`);
  await expect.poll(async () => (await htmxCounters(page)).after).toBeGreaterThan(0);
  const capacity = page.getByLabel("Max players");
  await capacity.fill("1000");
  await expect(capacity).toHaveValue("999");
});


test("a detail refresh preserves a selected relative start shortcut", async ({ page, request }) => {
  await page.goto("/game-requests/new?game_id=game-1");
  await page.getByLabel("Max players").fill("4");
  await page.getByRole("button", { name: "Create Game Request" }).click();
  await page.waitForURL(/\/game-requests\/[0-9a-f-]+(?:\?.*)?$/);
  await page.getByRole("link", { name: "Edit" }).click();
  await expect(page.locator("#game-request-edit-dialog")).toBeVisible();
  await page.locator("label.start-choice").filter({ hasText: "in 15 min" }).click();
  await waitForRealtime(page, request, 1);
  await instrumentHtmx(page);
  const id = await page.locator("#game-request-detail").getAttribute("data-game-request-id");
  await request.post(`/__test__/publish?type=GameRequestJoined&request=${id}`);
  await expect.poll(async () => (await htmxCounters(page)).after).toBeGreaterThan(0);
  await expect(page.locator("input[name='start_preset'][value='15']")).toBeChecked();
  await expect(page.locator("[data-custom-start]")).toBeHidden();
  await page.getByRole("button", { name: "Save" }).click();
  await expect(page.locator("#game-request-edit-dialog")).toBeHidden();
  const start = new Date((await page.locator("#game-request-detail time[data-local-time='start']").getAttribute("datetime"))!);
  expect(start.getTime()).toBeGreaterThan(Date.now() + 14 * 60_000);
  expect(start.getTime()).toBeLessThan(Date.now() + 16 * 60_000);
});
test("a playing refresh leaves the open edit draft untouched", async ({ page }) => {
  await page.goto("/game-requests/new?game_id=game-1");
  await page.locator("input[name='max_players']").fill("4");
  await page.getByRole("button", { name: "Create Game Request" }).click();
  await page.waitForURL(/\/game-requests\/[0-9a-f-]+(?:\?.*)?$/);
  await page.getByRole("link", { name: "Edit" }).click();
  await expect(page.locator("#game-request-edit-dialog")).toBeVisible();
  await waitForRealtime(page, page.request, 1);
  await instrumentHtmx(page);

  const detail = page.locator("#game-request-detail");
  const id = await detail.getAttribute("data-game-request-id");
  const csrf = await detail.locator("input[name='csrf_token']").first().inputValue();
  await page.getByLabel("Description").fill("Local description");
  const started = await page.request.post(`/game-requests/${id}/start`, { form: { csrf_token: csrf } });
  expect(started.ok()).toBe(true);
  await expect.poll(async () => (await htmxCounters(page)).after).toBeGreaterThan(0);

  await expect(page.getByText("Playing", { exact: true }).first()).toBeVisible();
  await expect(page.locator("form[action$='/details'] input[name='start_date'], form[action$='/details'] input[name='start_time']")).toHaveCount(2);
  await expect(page.getByLabel("Max players")).toHaveValue("4");
  await expect(page.getByLabel("Description")).toHaveValue("Local description");
});

test("a detail refresh does not merge server edits into an open editor draft", async ({ page, request }) => {
  await page.goto("/game-requests/new?game_id=game-1");
  await page.locator("input[name='max_players']").fill("4");
  await page.getByRole("button", { name: "Create Game Request" }).click();
  await page.waitForURL(/\/game-requests\/[0-9a-f-]+(?:\?.*)?$/);
  await page.getByRole("link", { name: "Edit" }).click();
  await expect(page.locator("#game-request-edit-dialog")).toBeVisible();
  await instrumentHtmx(page);
  const capacity = page.getByLabel("Max players");

  const detail = page.locator("#game-request-detail");
  const id = await detail.getAttribute("data-game-request-id");
  const csrf = await detail.locator("input[name='csrf_token']").first().inputValue();
  const startDate = await page.locator("input[name='start_date']").inputValue();
  const startTime = await page.locator("input[name='start_time']").inputValue();
  const timezone = await page.locator("input[name='timezone']").inputValue();
  const description = await page.locator("textarea[name='description']").inputValue();
  const changed = await page.request.post(`/game-requests/${id}/details`, {
    form: {
      max_players: "5", start_date: startDate, start_time: startTime,
      timezone, description, csrf_token: csrf,
    },
  });
  expect(changed.ok()).toBe(true);
  expect(new URL(changed.url()).pathname).toBe(`/game-requests/${id}`);
  await expect.poll(async () => (await htmxCounters(page)).after).toBeGreaterThan(0);

  await expect(capacity).toHaveValue("4");
  await expect(page.locator("#game-request-detail")).toContainText("1/5");

  await capacity.fill("6");
  const cancelled = await page.request.post(`/game-requests/${id}/cancel`, {
    form: { csrf_token: csrf },
  });
  expect(cancelled.ok()).toBe(true);
  await expect(capacity).toHaveValue("6");
  await expect(page.getByText("Cancelled", { exact: true }).first()).toBeVisible();
  await page.getByRole("button", { name: "Save" }).click();
  const alert = page.locator("#game-request-edit-dialog [role='alert']");
  await expect(alert).toContainText("This Game Request is no longer active.");
  await expect(alert).toBeFocused();
  await expect(page.locator("#game-request-edit-dialog form[action$='/details']")).toHaveCount(0);
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

test("a live detail refresh preserves an unsent chat draft", async ({ page, request }) => {
  await page.goto("/game-requests/new?game_id=game-1");
  await page.getByRole("button", { name: "Create Game Request" }).click();
  await page.waitForURL(/\/game-requests\/[0-9a-f-]+/);
  await waitForRealtime(page, request, 1);
  await instrumentHtmx(page);

  const draft = page.getByRole("textbox", { name: "Message the lobby" });
  await draft.fill("I am still typing");
  const requestID = await page.locator("[data-game-request-detail]").getAttribute("data-game-request-id");
  await request.post(`/__test__/publish?type=GameRequestJoined&request=${requestID}`);

  await expect.poll(async () => (await htmxCounters(page)).after).toBeGreaterThan(0);
  await expect(draft).toHaveValue("I am still typing");
});

test("the Lobby combines activity and chat and refreshes from a content-free chat signal", async ({ page, request }) => {
  await page.goto("/game-requests/new?game_id=game-1");
  await page.getByRole("button", { name: "Create Game Request" }).click();
  await page.waitForURL(/\/game-requests\/[0-9a-f-]+/);
  const lobby = page.locator("#game-request-lobby");
  await expect(lobby.locator(".activity-row")).toContainText("Ada Player created the request");
  await page.getByRole("textbox", { name: "Message the lobby" }).fill("Ready for game");
  await page.getByRole("button", { name: "Send" }).click();
  await expect(lobby.locator(".chat-message")).toContainText("Ready for game");
  await expect(lobby.locator(".activity-row")).toContainText("Ada Player created the request");

  await waitForRealtime(page, request, 1);
  await instrumentHtmx(page);
  const requestID = await page.locator("[data-game-request-detail]").getAttribute("data-game-request-id");
  await request.post(`/__test__/publish?type=GameRequestChatMessage&request=${requestID}`);
  await expect.poll(async () => (await htmxCounters(page)).after).toBeGreaterThan(0);
  await expect(lobby.locator(".chat-message")).toContainText("Ready for game");
});
test("a chat signal during a mutation refreshes only the Lobby", async ({ page, request }) => {
  await page.goto("/game-requests/new?game_id=game-1");
  await page.getByRole("button", { name: "Create Game Request" }).click();
  await page.waitForURL(/\/game-requests\/[0-9a-f-]+/);
  await waitForRealtime(page, request, 1);

  const requestID = await page.locator("[data-game-request-detail]").getAttribute("data-game-request-id");
  const requestedURLs: string[] = [];
  page.on("request", (outgoing) => requestedURLs.push(outgoing.url()));
  await request.post("/__test__/mode?delay=500");
  await page.getByRole("textbox", { name: "Message the lobby" }).fill("Sending while signal arrives");
  await page.getByRole("button", { name: "Send" }).click({ noWaitAfter: true });
  await request.post(`/__test__/publish?type=GameRequestChatMessage&request=${requestID}`);

  await expect(page.locator("#game-request-lobby .chat-message")).toContainText("Sending while signal arrives");
  await expect.poll(() => requestedURLs.some((url) => url.endsWith("/lobby"))).toBe(true);
  expect(requestedURLs.some((url) => url.endsWith("/refresh"))).toBe(false);
  await request.post("/__test__/mode?delay=0");
});

test("session-information and general detail refreshes preserve an active dialog draft", async ({ page, request }) => {
  await page.goto("/game-requests/new?game_id=game-1");
  await page.getByRole("button", { name: "Create Game Request" }).click();
  await page.waitForURL(/\/game-requests\/[0-9a-f-]+/);
  await waitForRealtime(page, request, 1);

  await page.getByRole("link", { name: "Add", exact: true }).click();
  const key = page.getByRole("textbox", { name: "Key" });
  const value = page.getByRole("textbox", { name: "Value" });
  await key.fill("Lobby");
  await value.fill("CXT-WC3-04");
  await value.focus();
  const requestID = await page.locator("[data-game-request-detail]").getAttribute("data-game-request-id");
  await request.post(`/__test__/publish?type=GameRequestSessionInformationChanged&request=${requestID}`);
  await expect(key).toHaveValue("Lobby");
  await expect(value).toHaveValue("CXT-WC3-04");
  await expect(value).toBeFocused();
  await instrumentHtmx(page);

  await request.post(`/__test__/publish?type=GameRequestJoined&request=${requestID}`);
  await expect.poll(async () => (await htmxCounters(page)).after).toBeGreaterThan(0);
  await expect(page.locator("#session-information-dialog")).toHaveCount(1);
  await expect(page.locator("#session-information-delete-dialog")).toHaveCount(1);
  await expect(key).toHaveValue("Lobby");
  await expect(value).toHaveValue("CXT-WC3-04");
  await expect(value).toBeFocused();
  await page.keyboard.press("Escape");
  await expect(page.getByRole("dialog")).not.toBeVisible();
  await expect(page.getByRole("link", { name: "Add", exact: true })).toBeFocused();
  await page.getByRole("link", { name: "Add", exact: true }).click();
  await expect(page.getByRole("dialog", { name: "Add session information" })).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(page.getByRole("link", { name: "Add", exact: true })).toBeFocused();
});

for (const dirtyField of ["key", "value"] as const) {
  test(`session-information refresh leaves the open dialog's ${dirtyField} draft untouched`, async ({ page, request }) => {
    await page.goto("/game-requests/new?game_id=game-1");
    await page.getByRole("button", { name: "Create Game Request" }).click();
    await page.waitForURL(/\/game-requests\/[0-9a-f-]+/);
    await waitForRealtime(page, request, 1);
    await page.getByRole("link", { name: "Add", exact: true }).click();
    await page.getByRole("textbox", { name: "Key" }).fill("Lobby");
    await page.getByRole("textbox", { name: "Value" }).fill("old value");
    await page.getByRole("button", { name: "Save" }).click();
    await expect(page.locator("#session-information")).toContainText("old value");

    const edit = page.locator("#session-information").getByRole("link", { name: "Edit Lobby" });
    const editURL = await edit.getAttribute("href");
    const entryID = new URL(editURL!, "http://localhost").searchParams.get("session_edit")!;
    await edit.click();
    const key = page.getByRole("textbox", { name: "Key" });
    const value = page.getByRole("textbox", { name: "Value" });
    const localValue = dirtyField === "key" ? "Local key" : "Local value";
    await (dirtyField === "key" ? key : value).fill(localValue);
    const csrf = await page.locator("#session-information-dialog input[name='csrf_token']").first().inputValue();
    const requestID = await page.locator("[data-game-request-detail]").getAttribute("data-game-request-id");
    const serverKey = dirtyField === "key" ? "Lobby" : "Server changed";
    const serverValue = dirtyField === "key" ? "Fresh value" : "old value";
    const result = await page.evaluate(async ({ requestID, entryID, key, value, csrf }) => {
      const response = await fetch(`/game-requests/${requestID}/session-information/${entryID}`, {
        method: "POST",
        headers: { "HX-Request": "true", "Content-Type": "application/x-www-form-urlencoded" },
        body: new URLSearchParams({ key, value, csrf_token: csrf, is_secret: "true" }),
      });
      return response.status;
    }, { requestID, entryID, key: serverKey, value: serverValue, csrf });
    expect(result).toBe(200);
    await expect(key).toHaveValue(dirtyField === "key" ? localValue : "Lobby");
    await expect(value).toHaveValue(dirtyField === "value" ? localValue : "old value");
    await expect(page.getByRole("checkbox", { name: "Secret value" })).not.toBeChecked();
    await expect(value).toHaveAttribute("type", "text");
  });
}

test("Session Information validation stays in the open dialog across live refreshes", async ({ page, request }) => {
  await page.goto("/game-requests/new?game_id=game-1");
  await page.getByRole("button", { name: "Create Game Request" }).click();
  await page.waitForURL(/\/game-requests\/[0-9a-f-]+/);
  await waitForRealtime(page, request, 1);
  await page.getByRole("link", { name: "Add", exact: true }).click();
  const rejectedKey = "K".repeat(41);
  const key = page.getByRole("textbox", { name: "Key" });
  const value = page.getByRole("textbox", { name: "Value" });
  await key.fill(rejectedKey);
  await value.fill("secret");
  await page.getByRole("button", { name: "Save" }).click();
  await expect(page.locator("#session-information-error")).toBeVisible();

  const requestID = await page.locator("[data-game-request-detail]").getAttribute("data-game-request-id");
  await request.post(`/__test__/publish?type=GameRequestSessionInformationChanged&request=${requestID}`);
  await expect(key).toHaveValue(rejectedKey);
  await expect(page.locator("#session-information-error")).toContainText("Key must contain");
  await request.post(`/__test__/publish?type=GameRequestJoined&request=${requestID}`);
  await expect(key).toHaveValue(rejectedKey);
  await expect(page.locator("#session-information-error")).toContainText("Key must contain");
});


test("Session Information Delete confirms and refreshes the fragment", async ({ page, request }) => {
  await page.goto("/game-requests/new?game_id=game-1");
  await page.getByRole("button", { name: "Create Game Request" }).click();
  await page.waitForURL(/\/game-requests\/[0-9a-f-]+/);
  await waitForRealtime(page, request, 1);
  await page.getByRole("link", { name: "Add", exact: true }).click();
  await page.getByRole("textbox", { name: "Key" }).fill("Lobby");
  await page.getByRole("textbox", { name: "Value" }).fill("temporary");
  await page.getByRole("button", { name: "Save" }).click();
  await expect(page.locator("#session-information")).toContainText("temporary");
  // htmx applies HX-Replace-Url after the swap settles, so the text can appear while the URL still has ?session_add=1.
  await expect(page).not.toHaveURL(/session_add/);
  const currentURL = page.url();
  await page.locator("#session-information").getByRole("button", { name: "Delete Lobby" }).click();
  const dialog = page.getByRole("dialog", { name: "Delete Lobby?" });
  await expect(dialog).toContainText("removed for everyone in this Game Request");
  await dialog.getByRole("button", { name: "Delete" }).click();
  await expect(dialog).not.toBeVisible();
  await expect(page.locator("#session-information")).not.toContainText("temporary");
  await expect(page).toHaveURL(currentURL);
});

test("session-information facts render side by side and edit in dialog", async ({ page }) => {
  await page.goto("/game-requests/new?game_id=game-1");
  await page.getByRole("button", { name: "Create Game Request" }).click();
  await page.waitForURL(/\/game-requests\/[0-9a-f-]+/);
  await page.getByRole("link", { name: "Add", exact: true }).click();

  for (const [keyText, valueText, secret] of [
    ["Server", "10.20.30.40:27015", false],
    ["Password", "hunter2", true],
    ["Lobby", "CXT-WC3-04", false],
    ["L".repeat(40), "S".repeat(160), true],
    ["Long value", "A".repeat(90), false],
  ] as const) {
    await page.getByRole("textbox", { name: "Key" }).fill(keyText);
    await page.getByRole("textbox", { name: "Value" }).fill(valueText);
    if (secret) await page.getByRole("checkbox", { name: "Secret value" }).check();
    await page.getByRole("button", { name: "Save" }).click();
    if (secret) await expect(page.locator("#session-information")).toContainText("Secret");
    else await expect(page.locator("#session-information")).toContainText(valueText);
    if (keyText !== "Long value") await page.getByRole("link", { name: "Add", exact: true }).click();
  }

  const requestID = await page.locator("[data-game-request-detail]").getAttribute("data-game-request-id");
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto(`/game-requests/${requestID}`);
  const facts = page.locator(".session-information-fact");
  await expect(facts).toHaveCount(5);
  await expect(page.locator(".session-information-grid")).toBeVisible();
  const desktop = await facts.first().evaluate((fact) => {
    const style = getComputedStyle(fact);
    const key = fact.querySelector<HTMLElement>(".session-information-key")!;
    const value = fact.querySelector<HTMLElement>(".session-information-value")!;
    const actions = fact.querySelector<HTMLElement>(".session-information-copy-actions")!;
    return {
      display: style.display,
      keyAboveValue: key.getBoundingClientRect().bottom <= value.getBoundingClientRect().top,
      actionsBesideValue: Math.abs(
        (actions.getBoundingClientRect().top + actions.getBoundingClientRect().bottom) / 2 -
        (value.getBoundingClientRect().top + value.getBoundingClientRect().bottom) / 2,
      ) < 12,
      factWidth: fact.getBoundingClientRect().width,
      sameRow: Math.abs(fact.getBoundingClientRect().top - fact.parentElement!.children[1].getBoundingClientRect().top) < 1,
    };
  });
  expect(desktop).toMatchObject({ display: "flex", keyAboveValue: true, actionsBesideValue: true, sameRow: true });
  expect(desktop.factWidth).toBeGreaterThanOrEqual(220);
  expect(desktop.factWidth).toBeLessThanOrEqual(320);
  const secret = facts.filter({ hasText: "Password" });
  await expect(secret.getByRole("button", { name: "Copy Password" })).toBeVisible();
  await expect(secret.getByRole("button", { name: "Reveal Password" })).toBeVisible();
  await expect(secret.locator("[data-secret-value]")).toHaveText("••••••••");
  const actionOrder = await secret.locator(".session-information-value-row").evaluate((actions) =>
    Array.from(actions.querySelectorAll<HTMLElement>("button, a")).map((action) => action.getAttribute("aria-label")));
  expect(actionOrder).toEqual(["Copy Password", "Reveal Password", "Edit Password", "Delete Password"]);
  const normalOrder = await facts.filter({ hasText: "Server" }).locator(".session-information-value-row").evaluate((actions) =>
    Array.from(actions.querySelectorAll<HTMLElement>("button, a")).map((action) => action.getAttribute("aria-label")));
  expect(normalOrder).toEqual(["Copy Server", "Edit Server", "Delete Server"]);

  for (const width of [1440, 900, 390]) {
    await page.setViewportSize({ width, height: 900 });
    await page.goto(`/game-requests/${requestID}`);
    const layout = await page.locator(".detail-coordination").evaluate((element) => {
      const description = element.querySelector<HTMLElement>(".detail-coordination-description")!;
      const session = element.querySelector<HTMLElement>("#session-information")!;
      return {
        description: description.getBoundingClientRect().toJSON(),
        session: session.getBoundingClientRect().toJSON(),
        document: document.documentElement.scrollWidth - document.documentElement.clientWidth,
      };
    });
    expect(layout.document).toBeLessThanOrEqual(1);
    if (width === 390) expect(layout.session.y).toBeGreaterThan(layout.description.y);
    else expect(layout.session.width).toBeGreaterThan(layout.description.width);
  }

  await page.setViewportSize({ width: 390, height: 900 });
  await page.goto(`/game-requests/${requestID}`);
  const mobileStacked = await facts.first().evaluate((first) => {
    const second = first.parentElement!.children[1].getBoundingClientRect();
    const firstRect = first.getBoundingClientRect();
    return Math.abs(firstRect.left - second.left) < 1 && second.top >= firstRect.bottom;
  });
  expect(mobileStacked).toBe(true);
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
  expect(overflow).toBeLessThanOrEqual(1);
  await expect(page.locator("#session-information").getByRole("link", { name: "Edit Server" })).toBeVisible();
  await page.locator("#session-information").getByRole("link", { name: "Edit Server" }).click();
  const dialog = page.getByRole("dialog", { name: "Edit session information" });
  await expect(dialog).toBeVisible();
  await page.getByRole("textbox", { name: "Value" }).fill("Saved value");
  await page.getByRole("button", { name: "Save" }).click();
  await expect(page.locator("#session-information")).toContainText("Saved value");
});

test("secret copy stays masked and reveal resets on fragment and full-detail refresh", async ({ page, request }) => {
  await page.addInitScript(() => {
    Object.defineProperty(Navigator.prototype, "clipboard", {
      configurable: true,
      get: () => ({ writeText: (text: string) => { window.__copiedSecret = text; return Promise.resolve(); } }),
    });
  });
  await page.goto("/game-requests/new?game_id=game-1");
  await page.getByRole("button", { name: "Create Game Request" }).click();
  await page.waitForURL(/\/game-requests\/[0-9a-f-]+/);
  await waitForRealtime(page, request, 1);
  await page.getByRole("link", { name: "Add", exact: true }).click();
  await page.getByRole("textbox", { name: "Key" }).fill("Password");
  await page.getByRole("textbox", { name: "Value" }).fill("shoulder-secret");
  await page.getByRole("checkbox", { name: "Secret value" }).check();
  await page.getByRole("button", { name: "Save" }).click();

  const fact = page.locator(".session-information-fact");
  const value = fact.locator("[data-secret-value]");
  await expect(fact).toContainText("Secret");
  await expect(value).toHaveText("••••••••");
  await fact.getByRole("button", { name: "Copy Password" }).click();
  await expect.poll(() => page.evaluate(() => window.__copiedSecret)).toBe("shoulder-secret");
  const notices = page.locator("#live-notices");
  await expect(notices).toContainText("Secret copied.");
  await expect(notices).not.toContainText("shoulder-secret");
  await expect(value).toHaveText("••••••••");

  await fact.getByRole("button", { name: "Reveal Password" }).click();
  const hide = fact.getByRole("button", { name: "Hide Password" });
  await expect(hide).toHaveAttribute("aria-pressed", "true");
  await hide.click();
  await expect(value).toHaveText("••••••••");

  await page.clock.install();
  await fact.getByRole("button", { name: "Reveal Password" }).click();
  await page.clock.fastForward(10_000);
  await fact.getByRole("button", { name: "Copy Password" }).click();
  await expect(notices).toContainText("Secret copied.");
  await expect(value).toHaveText("shoulder-secret");
  await page.clock.fastForward(10_000);
  await expect(value).toHaveText("••••••••");
  await fact.getByRole("button", { name: "Reveal Password" }).click();
  await expect(value).toHaveText("shoulder-secret");
  const requestID = await page.locator("[data-game-request-detail]").getAttribute("data-game-request-id");
  await request.post(`/__test__/publish?type=GameRequestSessionInformationChanged&request=${requestID}`);
  await expect(value).toHaveText("••••••••");
  await fact.getByRole("button", { name: "Reveal Password" }).click();
  await expect(value).toHaveText("shoulder-secret");
  const previousDetail = await page.locator("#game-request-detail").elementHandle();
  expect(previousDetail).not.toBeNull();
  await request.post(`/__test__/publish?type=GameRequestJoined&request=${requestID}`);
  await expect.poll(() => previousDetail!.evaluate((element) => element.isConnected)).toBe(false);
  const refreshedValue = page.locator("#session-information [data-secret-value]");
  await expect(refreshedValue).toHaveText("••••••••");
  await page.locator("#session-information").getByRole("button", { name: "Reveal Password" }).click();
  await expect(refreshedValue).toHaveText("shoulder-secret");
  await page.clock.fastForward(20_000);
  await expect(refreshedValue).toHaveText("••••••••");
});
test("secret copy reports clipboard rejection and unsupported API", async ({ page }) => {
  await page.addInitScript(() => {
    Object.defineProperty(Navigator.prototype, "clipboard", {
      configurable: true,
      get: () => ({ writeText: () => Promise.reject(new Error("permission denied")) }),
    });
    document.execCommand = () => false;
  });
  await page.goto("/game-requests/new?game_id=game-1");
  await page.getByRole("button", { name: "Create Game Request" }).click();
  await page.waitForURL(/\/game-requests\/[0-9a-f-]+/);
  await page.getByRole("link", { name: "Add", exact: true }).click();
  await page.getByRole("textbox", { name: "Key" }).fill("Password");
  await page.getByRole("textbox", { name: "Value" }).fill("shoulder-secret");
  await page.getByRole("checkbox", { name: "Secret value" }).check();
  await page.getByRole("button", { name: "Save" }).click();

  const fact = page.locator(".session-information-fact");
  const value = fact.locator("[data-secret-value]");
  await fact.getByRole("button", { name: "Copy Password" }).click();
  const notices = page.locator("#live-notices");
  await expect(notices).toContainText("Could not copy secret. Please try again.");
  await expect(notices).not.toContainText("shoulder-secret");
  await expect(value).toHaveText("••••••••");

  await page.evaluate(() => {
    Object.defineProperty(Navigator.prototype, "clipboard", { configurable: true, get: () => undefined });
  });
  await fact.getByRole("button", { name: "Copy Password" }).click();
  await expect(notices.locator('[role="alert"]', { hasText: "Could not copy secret" })).toHaveCount(2);
  await expect(value).toHaveText("••••••••");
});


test("secret session edit masks value and permits toggling presentation", async ({ page }) => {
  await page.goto("/game-requests/new?game_id=game-1");
  await page.getByRole("button", { name: "Create Game Request" }).click();
  await page.waitForURL(/\/game-requests\/[0-9a-f-]+/);
  await page.getByRole("link", { name: "Add", exact: true }).click();
  await page.getByRole("textbox", { name: "Key" }).fill("Password");
  await page.getByRole("textbox", { name: "Value" }).fill("shoulder-secret");
  await page.getByRole("checkbox", { name: "Secret value" }).check();
  await page.getByRole("button", { name: "Save" }).click();
  await page.getByRole("link", { name: "Edit Password" }).click();
  const value = page.getByRole("textbox", { name: "Value" });
  await expect(value).toHaveAttribute("type", "password");
  await expect(value).toHaveValue("shoulder-secret");
  await page.getByRole("checkbox", { name: "Secret value" }).uncheck();
  await expect(value).toHaveAttribute("type", "text");
});


for (const refresh of ["session-information fragment", "full detail"] as const) {
  test(`a rejected edit preserves its dirty secret toggle through ${refresh} refresh`, async ({ page, request }) => {
    const browserConsole: string[] = [];
    const pageErrors: string[] = [];
    page.on("console", message => browserConsole.push(message.text()));
    page.on("pageerror", error => pageErrors.push(error.message));
    await page.goto("/game-requests/new?game_id=game-1");
    await page.getByRole("button", { name: "Create Game Request" }).click();
    await page.waitForURL(/\/game-requests\/[0-9a-f-]+/);
    await waitForRealtime(page, request, 1);
    await page.getByRole("link", { name: "Add", exact: true }).click();
    await page.getByRole("textbox", { name: "Key" }).fill("Password");
    await page.getByRole("textbox", { name: "Value" }).fill("hunter2");
    await page.getByRole("button", { name: "Save" }).click();
    await expect(page.locator("#session-information")).toContainText("hunter2");

    await page.getByRole("link", { name: "Edit Password" }).click();
    const key = page.getByRole("textbox", { name: "Key" });
    const value = page.getByRole("textbox", { name: "Value" });
    const toggle = page.getByRole("checkbox", { name: "Secret value" });
    await toggle.check();
    await key.evaluate((input: HTMLInputElement) => { input.value = "K".repeat(41); });
    await page.getByRole("button", { name: "Save" }).click();
    await expect(page.locator("#session-information-error")).toContainText("Key must contain");
    await expect(toggle).toBeChecked();
    await expect(value).toHaveAttribute("type", "password");
    await expect(page.locator("#session-information-dialog [data-session-information-form]")).toHaveAttribute("data-server-is-secret", "false");

    const requestID = await page.locator("[data-game-request-detail]").getAttribute("data-game-request-id");
    if (refresh === "session-information fragment") {
      await request.post(`/__test__/publish?type=GameRequestSessionInformationChanged&request=${requestID}`);
    } else {
      await request.post(`/__test__/publish?type=GameRequestJoined&request=${requestID}`);
    }
    await expect(key).toHaveValue("K".repeat(41));
    await expect(page.locator("#session-information-error")).toContainText("Key must contain");
    await expect(toggle).toBeChecked();
    await expect(value).toHaveAttribute("type", "password");
    await expect(page.locator("#session-information-dialog [data-session-information-form]")).toHaveAttribute("data-server-is-secret", "false");

    await key.fill("Password");
    await page.getByRole("button", { name: "Save" }).click();
    await expect(page.locator(".session-information-fact[data-secret='true']")).toContainText("Secret");
    await expect(page.locator(".session-information-fact[data-secret='true'] [data-secret-value]")).toHaveAttribute("data-revealed", "false");
    expect(browserConsole.filter(message => /session-information/i.test(message))).toEqual([]);
    expect(pageErrors).toEqual([]);
  });
}
test("session-information refresh preserves a dirty secret toggle field", async ({ page, request }) => {
  await page.goto("/game-requests/new?game_id=game-1");
  await page.getByRole("button", { name: "Create Game Request" }).click();
  await page.waitForURL(/\/game-requests\/[0-9a-f-]+/);
  await waitForRealtime(page, request, 1);
  await page.getByRole("link", { name: "Add", exact: true }).click();
  const toggle = page.getByRole("checkbox", { name: "Secret value" });
  await toggle.focus();
  await toggle.check();
  const requestID = await page.locator("[data-game-request-detail]").getAttribute("data-game-request-id");
  await request.post(`/__test__/publish?type=GameRequestSessionInformationChanged&request=${requestID}`);
  await expect(toggle).toBeFocused();
  await expect(toggle).toBeChecked();
  await expect(page.getByRole("textbox", { name: "Value" })).toHaveAttribute("type", "password");
});

test("session-information outsider sees join hint without entry data", async ({ page, browser }) => {
  await page.goto("/game-requests/new?game_id=game-1");
  await page.getByRole("button", { name: "Create Game Request" }).click();
  await page.waitForURL(/\/game-requests\/[0-9a-f-]+/);
  await page.getByRole("link", { name: "Add", exact: true }).click();
  await page.getByRole("textbox", { name: "Key" }).fill("Private key");
  await page.getByRole("textbox", { name: "Value" }).fill("private value");
  await page.getByRole("checkbox", { name: "Secret value" }).check();
  await page.getByRole("button", { name: "Save" }).click();
  const requestID = await page.locator("[data-game-request-detail]").getAttribute("data-game-request-id");

  const outsider = await browser.newContext({
    baseURL: "http://127.0.0.1:8123",
    storageState: {
      cookies: [{
        name: "lansurfer_session",
        value: "browser-test-other",
        domain: "127.0.0.1",
        path: "/",
        expires: -1,
        httpOnly: true,
        secure: false,
        sameSite: "Lax",
      }],
      origins: [],
    },
  });
  const outsiderPage = await outsider.newPage();
  await outsiderPage.goto(`/game-requests/${requestID}`);
  const sessionInformation = outsiderPage.locator("#session-information");
  await expect(sessionInformation).toContainText("Join this Game Request to see session information.");
  await expect(sessionInformation).not.toContainText("Private key");
  await expect(sessionInformation).not.toContainText("private value");
  await outsider.close();
});
test("secret copy falls back to a temporary textarea when clipboard API is unavailable", async ({ page }) => {
  await page.addInitScript(() => {
    Object.defineProperty(Navigator.prototype, "clipboard", {
      configurable: true,
      get: () => ({ writeText: () => Promise.reject(new Error("permission denied")) }),
    });
    document.execCommand = (command) => {
      if (command !== "copy") return false;
      const textarea = document.activeElement;
      window.__copiedSecret = textarea instanceof HTMLTextAreaElement ? textarea.value : "";
      return textarea instanceof HTMLTextAreaElement;
    };
  });
  await page.goto("/game-requests/new?game_id=game-1");
  await page.getByRole("button", { name: "Create Game Request" }).click();
  await page.waitForURL(/\/game-requests\/[0-9a-f-]+/);
  await page.getByRole("link", { name: "Add", exact: true }).click();
  await page.getByRole("textbox", { name: "Key" }).fill("Password");
  await page.getByRole("textbox", { name: "Value" }).fill("shoulder-secret");
  await page.getByRole("checkbox", { name: "Secret value" }).check();
  await page.getByRole("button", { name: "Save" }).click();

  const fact = page.locator(".session-information-fact");
  await fact.getByRole("button", { name: "Copy Password" }).click();
  await expect.poll(() => page.evaluate(() => window.__copiedSecret)).toBe("shoulder-secret");
  await expect(fact.locator("[data-secret-value]")).toHaveText("••••••••");
  await expect(page.locator("#live-notices")).toContainText("Secret copied.");
  await expect(page.locator("textarea")).toHaveCount(0);
  await page.evaluate(() => {
    Object.defineProperty(Navigator.prototype, "clipboard", { configurable: true, get: () => undefined });
  });
  await fact.getByRole("button", { name: "Copy Password" }).click();
  await expect.poll(() => page.evaluate(() => window.__copiedSecret)).toBe("shoulder-secret");
  await expect(page.locator("#live-notices [role='status']")).toHaveCount(2);
});

test("secret reveal controls are per entry and remask on visibility change", async ({ page }) => {
  await page.goto("/game-requests/new?game_id=game-1");
  await page.getByRole("button", { name: "Create Game Request" }).click();
  await page.waitForURL(/\/game-requests\/[0-9a-f-]+/);
  for (const [key, value] of [["Lobby password", "lobby-secret"], ["Voice password", "voice-secret"]]) {
    await page.getByRole("link", { name: "Add", exact: true }).click();
    await page.getByRole("textbox", { name: "Key" }).fill(key);
    await page.getByRole("textbox", { name: "Value" }).fill(value);
    await page.getByRole("checkbox", { name: "Secret value" }).check();
    await page.getByRole("button", { name: "Save" }).click();
  }

  const facts = page.locator(".session-information-fact");
  const lobby = facts.filter({ hasText: "Lobby password" });
  const voice = facts.filter({ hasText: "Voice password" });
  const lobbyValue = lobby.locator("[data-secret-value]");
  const voiceValue = voice.locator("[data-secret-value]");
  const revealLobby = lobby.getByRole("button", { name: "Reveal Lobby password" });
  await expect(revealLobby.locator("i")).toHaveClass(/ph-eye/);
  await revealLobby.click();
  const hideLobby = lobby.getByRole("button", { name: "Hide Lobby password" });
  await expect(hideLobby).toHaveAttribute("aria-pressed", "true");
  await expect(hideLobby.locator("i")).toHaveClass(/ph-eye-slash/);
  await expect(lobbyValue).toHaveText("lobby-secret");
  await expect(voiceValue).toHaveText("••••••••");
  await voice.getByRole("button", { name: "Reveal Voice password" }).click();
  await expect(voiceValue).toHaveText("voice-secret");
  await page.evaluate(() => {
    Object.defineProperty(document, "hidden", { configurable: true, get: () => true });
    document.dispatchEvent(new Event("visibilitychange"));
  });
  await expect(lobbyValue).toHaveText("••••••••");
  await expect(voiceValue).toHaveText("••••••••");
  await expect(lobby.getByRole("button", { name: "Reveal Lobby password" })).toHaveAttribute("aria-pressed", "false");
  await expect(voice.getByRole("button", { name: "Reveal Voice password" })).toHaveAttribute("aria-pressed", "false");
});
test("Session Information dialogs keep drafts across realtime refreshes and close after save", async ({ page, request }) => {
  await page.goto("/game-requests/new?game_id=game-1");
  await page.getByRole("button", { name: "Create Game Request" }).click();
  await page.waitForURL(/\/game-requests\/[0-9a-f-]+/);
  await waitForRealtime(page, request, 1);

  const add = page.getByRole("link", { name: "Add", exact: true });
  await add.click();
  const dialog = page.getByRole("dialog", { name: "Add session information" });
  await expect(dialog).toBeVisible();
  const key = dialog.getByRole("textbox", { name: "Key" });
  const value = dialog.getByRole("textbox", { name: "Value" });
  await key.fill("Lobby");
  await value.fill("draft");

  const requestID = await page.locator("[data-game-request-detail]").getAttribute("data-game-request-id");
  await request.post(`/__test__/publish?type=GameRequestSessionInformationChanged&request=${requestID}`);
  await expect(key).toHaveValue("Lobby");
  await expect(value).toHaveValue("draft");
  await expect(dialog).toBeVisible();

  await dialog.getByRole("button", { name: "Save" }).click();
  await expect(dialog).not.toBeVisible();
  await expect(page.locator("#session-information")).toContainText("draft");
  await expect(page.locator("#session-information").getByRole("link", { name: "Add", exact: true })).toBeFocused();
});

test("Session Information dialog supports Escape, backdrop, validation, edit, and delete confirmation", async ({ page }) => {
  await page.goto("/game-requests/new?game_id=game-1");
  await page.getByRole("button", { name: "Create Game Request" }).click();
  await page.waitForURL(/\/game-requests\/[0-9a-f-]+/);

  const add = page.getByRole("link", { name: "Add", exact: true });
  await add.click();
  const dialog = page.getByRole("dialog", { name: "Add session information" });
  const key = dialog.getByRole("textbox", { name: "Key" });
  await key.fill("X".repeat(41));
  await dialog.getByRole("textbox", { name: "Value" }).fill("kept");
  await dialog.getByRole("button", { name: "Save" }).click();
  await expect(dialog.getByRole("alert")).toContainText("Key must contain");
  await expect(key).toHaveValue("X".repeat(41));
  await expect(key).toHaveAttribute("aria-invalid", "true");
  await expect(key).toBeFocused();
  await page.keyboard.press("Escape");
  await expect(dialog).not.toBeVisible();
  await expect(add).toBeFocused();

  await add.click();
  await expect(dialog).toBeVisible();
  const panel = await dialog.locator(".dialog-panel").boundingBox();
  const viewport = page.viewportSize();
  if (!panel || !viewport) throw new Error("Dialog geometry is unavailable");
  await page.mouse.click(panel.x + panel.width / 2, panel.y / 2);
  await expect(dialog).not.toBeVisible();
  await expect(add).toBeFocused();

  await add.click();
  await expect(dialog).toBeVisible();
  await dialog.getByRole("button", { name: "Close dialog" }).click();
  await expect(dialog).not.toBeVisible();
  await expect(add).toBeFocused();

  await add.click();
  await dialog.getByRole("textbox", { name: "Key" }).fill("Password");
  const value = dialog.getByRole("textbox", { name: "Value" });
  await value.fill("private");
  await dialog.getByRole("checkbox", { name: "Secret value" }).check();
  await expect(value).toHaveAttribute("type", "password");
  await dialog.getByRole("button", { name: "Save" }).click();
  await expect(page.locator("#session-information")).toContainText("Secret");

  await page.getByRole("link", { name: "Edit Password" }).click();
  const editDialog = page.getByRole("dialog", { name: "Edit session information" });
  await expect(editDialog.getByRole("textbox", { name: "Key" })).toHaveValue("Password");
  await expect(editDialog.getByRole("textbox", { name: "Value" })).toHaveValue("private");
  await expect(editDialog.getByRole("textbox", { name: "Value" })).toHaveAttribute("type", "password");
  await editDialog.getByRole("button", { name: "Cancel" }).click();
  await expect(editDialog).not.toBeVisible();
  await expect(page.getByRole("link", { name: "Edit Password" })).toBeFocused();

  await page.getByRole("button", { name: "Delete Password" }).click();
  const deleteDialog = page.getByRole("dialog", { name: "Delete Password?" });
  await expect(deleteDialog).toContainText("removed for everyone in this Game Request");
  await deleteDialog.getByRole("button", { name: "Cancel" }).click();
  await expect(deleteDialog).not.toBeVisible();
  await expect(page.getByRole("button", { name: "Delete Password" })).toBeFocused();
});
test("Session Information no-JavaScript fallback supports add, edit, and delete", async ({ page }) => {
  const browser = page.context().browser();
  if (!browser) throw new Error("Playwright browser is unavailable");
  const context = await browser.newContext({
    baseURL: "http://127.0.0.1:8123",
    storageState: await page.context().storageState(),
    javaScriptEnabled: false,
  });
  const noJS = await context.newPage();
  try {
    await noJS.goto("/game-requests/new?game_id=game-1");
    await noJS.getByRole("button", { name: "Create Game Request" }).click();
    await noJS.waitForURL(/\/game-requests\/[0-9a-f-]+/);
    await noJS.getByRole("link", { name: "Add", exact: true }).click();
    await expect(noJS.locator("#session-information .session-information-form")).toBeVisible();
    await noJS.getByRole("textbox", { name: "Key" }).fill("Server");
    await noJS.getByRole("textbox", { name: "Value" }).fill("no-js-value");
    await noJS.getByRole("button", { name: "Save" }).click();
    await expect(noJS.locator("#session-information")).toContainText("no-js-value");

    await noJS.getByRole("link", { name: "Edit Server" }).click();
    await expect(noJS.locator("#session-information .session-information-form")).toBeVisible();
    await expect(noJS.getByRole("textbox", { name: "Value" })).toHaveValue("no-js-value");
    await noJS.getByRole("textbox", { name: "Value" }).fill("edited-without-js");
    await noJS.getByRole("button", { name: "Save" }).click();
    await expect(noJS.locator("#session-information")).toContainText("edited-without-js");

    await noJS.locator("#session-information").getByRole("button", { name: "Delete Server" }).click();
    await expect(noJS.locator("#session-information")).not.toContainText("edited-without-js");
  } finally {
    await context.close();
  }
});
