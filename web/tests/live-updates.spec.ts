
declare global {
  interface Window {
    __copiedSecret?: string;
  }
}

import { expect, test } from "@playwright/test";
import { htmxCounters, instrumentHtmx, resetFixture, waitForRealtime } from "./helpers";

// SSE updates arriving around an htmx swap were one of the suspected causes in
// issue #15, and Track A widened that surface: the list refresh now carries the
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
    await page.waitForURL(/\?edit=1$/);
    const capacity = page.getByLabel("Max players");
    const value = scenario === "validation error" ? "1" : "6";
    await capacity.fill(value);
    if (scenario === "validation error") {
      await page.getByRole("button", { name: "Save" }).click();
      await expect(page.locator("#max-players-error")).toBeVisible();
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
      await expect(page.locator("#max-players-error")).toBeVisible();
      await expect(capacity).toHaveAttribute("aria-invalid", "true");
    }
  });
}

for (const scenario of ["description draft", "capacity draft"] as const) {
  test(`a detail refresh keeps only the dirty field for ${scenario}`, async ({ page }) => {
    await page.goto("/game-requests/new?game_id=game-1");
    await page.locator("input[name='max_players']").fill("4");
    await page.getByRole("button", { name: "Create Game Request" }).click();
    await page.waitForURL(/\/game-requests\/[0-9a-f-]+(?:\?.*)?$/);
    await page.getByRole("link", { name: "Edit" }).click();
    await page.waitForURL(/\?edit=1$/);
    await waitForRealtime(page, page.request, 1);
    await instrumentHtmx(page);

    const detail = page.locator("#game-request-detail");
    const id = await detail.getAttribute("data-game-request-id");
    const csrf = await detail.locator("input[name='csrf_token']").first().inputValue();
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
      await expect(capacity).toHaveValue("6");
    } else {
      await expect(capacity).toHaveValue("6");
      await expect(description).toHaveValue("Server description");
    }
    await page.getByRole("button", { name: "Save" }).click();
    await page.waitForURL(/\/game-requests\/[0-9a-f-]+(?:\?.*)?$/);
    await expect(page.locator("#game-request-detail")).toContainText("1/6");
    await expect(page.locator("#game-request-detail")).toContainText(
      scenario === "description draft" ? "A😀BC" : "Server description",
    );
  });
}

test("a detail refresh preserves dirty custom start date and time", async ({ page, request }) => {
  await page.goto("/game-requests/new?game_id=game-1");
  await page.getByLabel("Max players").fill("4");
  await page.getByRole("button", { name: "Create Game Request" }).click();
  await page.waitForURL(/\/game-requests\/[0-9a-f-]+(?:\?.*)?$/);
  await page.getByRole("link", { name: "Edit" }).click();
  await page.waitForURL(/\?edit=1$/);
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
  await page.waitForURL(/\/game-requests\/[0-9a-f-]+(?:\?.*)?$/);
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
    await page.waitForURL(/\?edit=1$/);
    await page.getByText("Pick date & time").click();
    await page.locator("input[name='start_date']").fill("2099-05-02");
    await page.locator("input[name='start_time']").fill("20:30");
    await page.getByRole("button", { name: "Save" }).click();
    await page.waitForURL(/\/game-requests\/[0-9a-f-]+(?:\?.*)?$/);

    await page.getByRole("link", { name: "Edit" }).click();
    await page.waitForURL(/\?edit=1$/);
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
    await page.waitForURL(/\/game-requests\/[0-9a-f-]+(?:\?.*)?$/);
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
  await page.waitForURL(/\?edit=1$/);
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
  await page.waitForURL(/\?edit=1$/);
  await page.locator("label.start-choice").filter({ hasText: "in 15 min" }).click();
  await waitForRealtime(page, request, 1);
  await instrumentHtmx(page);
  const id = await page.locator("#game-request-detail").getAttribute("data-game-request-id");
  await request.post(`/__test__/publish?type=GameRequestJoined&request=${id}`);
  await expect.poll(async () => (await htmxCounters(page)).after).toBeGreaterThan(0);
  await expect(page.locator("input[name='start_preset'][value='15']")).toBeChecked();
  await expect(page.locator("[data-custom-start]")).toBeHidden();
  await page.getByRole("button", { name: "Save" }).click();
  await page.waitForURL(/\/game-requests\/[0-9a-f-]+(?:\?.*)?$/);
  const start = new Date((await page.locator("time[data-local-time='start']").getAttribute("datetime"))!);
  expect(start.getTime()).toBeGreaterThan(Date.now() + 14 * 60_000);
  expect(start.getTime()).toBeLessThan(Date.now() + 16 * 60_000);
});
test("a Playing refresh does not restore removed planned-start controls", async ({ page }) => {
  await page.goto("/game-requests/new?game_id=game-1");
  await page.locator("input[name='max_players']").fill("4");
  await page.getByRole("button", { name: "Create Game Request" }).click();
  await page.waitForURL(/\/game-requests\/[0-9a-f-]+(?:\?.*)?$/);
  await page.getByRole("link", { name: "Edit" }).click();
  await page.waitForURL(/\?edit=1$/);
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
  await expect(page.locator("form[action$='/details'] input[name='start_date'], form[action$='/details'] input[name='start_time']")).toHaveCount(0);
  await expect(page.getByLabel("Max players")).toHaveValue("4");
  await expect(page.getByLabel("Description")).toHaveValue("Local description");
});

test("a detail refresh updates an untouched request editor and removes a draft when cancelled", async ({ page, request }) => {
  await page.goto("/game-requests/new?game_id=game-1");
  await page.locator("input[name='max_players']").fill("4");
  await page.getByRole("button", { name: "Create Game Request" }).click();
  await page.waitForURL(/\/game-requests\/[0-9a-f-]+(?:\?.*)?$/);
  await page.getByRole("link", { name: "Edit" }).click();
  await page.waitForURL(/\?edit=1$/);
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

test("session-information and general detail refreshes preserve an active inline draft", async ({ page, request }) => {
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

  await request.post(`/__test__/publish?type=GameRequestJoined&request=${requestID}`);
  await expect(key).toHaveValue("Lobby");
  await expect(value).toHaveValue("CXT-WC3-04");
  await expect(value).toBeFocused();
});

for (const dirtyField of ["key", "value"] as const) {
  test(`session-information refresh preserves only a dirty ${dirtyField} field`, async ({ page, request }) => {
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
    const csrf = await page.locator("#session-information input[name='csrf_token']").first().inputValue();
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
    await expect(key).toHaveValue(dirtyField === "key" ? localValue : "Server changed");
    await expect(value).toHaveValue(dirtyField === "value" ? localValue : "Fresh value");
    const secret = page.getByRole("checkbox", { name: "Secret value" });
    await expect(secret).toBeChecked();
    await expect(value).toHaveAttribute("type", "password");
  });
}

test("session-information validation survives live refreshes but not terminal state", async ({ page, request }) => {
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

  page.once("dialog", dialog => dialog.accept());
  await page.getByRole("button", { name: "Cancel Game Request" }).click();
  await expect(page.locator("#session-information")).toHaveCount(0);
  await request.post(`/__test__/publish?type=GameRequestCancelled&request=${requestID}`);
  await expect(page.locator("#session-information")).toHaveCount(0);
});

test("session-information Delete removes the entry inline", async ({ page, request }) => {
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
  page.once("dialog", dialog => dialog.accept());
  await page.locator("#session-information").getByRole("button", { name: "Delete Lobby" }).click();
  await expect(page.locator("#session-information")).not.toContainText("temporary");
  await expect(page).toHaveURL(currentURL);
});

test("session-information terminal detail refresh discards draft", async ({ page, request }) => {
  await page.goto("/game-requests/new?game_id=game-1");
  await page.getByRole("button", { name: "Create Game Request" }).click();
  await page.waitForURL(/\/game-requests\/[0-9a-f-]+/);
  await waitForRealtime(page, request, 1);
  await page.getByRole("link", { name: "Add", exact: true }).click();
  await page.getByRole("textbox", { name: "Key" }).fill("Draft");
  page.once("dialog", dialog => dialog.accept());
  await page.getByRole("button", { name: "Cancel Game Request" }).click();
  await expect(page.locator("#session-information")).toHaveCount(0);
  await request.post("/__test__/publish?type=GameRequestCancelled");
  await expect(page.locator("[data-game-request-detail]")).toContainText("Cancelled");
  await expect(page.locator("#session-information")).toHaveCount(0);
});

test("session-information tiles reflow and inline editor saves locally", async ({ page }) => {
  await page.goto("/game-requests/new?game_id=game-1");
  await page.getByRole("button", { name: "Create Game Request" }).click();
  await page.waitForURL(/\/game-requests\/[0-9a-f-]+/);
  await page.getByRole("link", { name: "Add", exact: true }).click();

  for (const [keyText, valueText, secret] of [
    ["Server", "10.20.30.40:27015", false],
    ["Password", "hunter2", true],
    ["Lobby", "CXT-WC3-04", false],
    ["Long secret", "S".repeat(160), true],
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

  const tiles = page.locator(".session-information-tile");
  await expect(tiles).toHaveCount(5);
  const styleSignature = await tiles.evaluateAll((entries) => entries.map((entry) => {
    const style = getComputedStyle(entry);
    return [style.borderStyle, style.borderWidth, style.borderRadius, style.backgroundColor, style.padding].join("|");
  }));
  expect(new Set(styleSignature).size).toBe(1);
  await expect(page.locator("#session-information > .session-information-heading")).toBeVisible();
  await expect(page.locator(".session-information-tile-heading")).toHaveCount(5);
  const headingLayout = await page.locator("#session-information").evaluate((section) => {
    const outer = section.querySelector<HTMLElement>(":scope > .session-information-heading")!;
    const add = outer.querySelector<HTMLElement>("a")!;
    const title = outer.querySelector<HTMLElement>("h2")!;
    const tileHeading = section.querySelector<HTMLElement>(".session-information-tile[data-secret='true'] .session-information-tile-heading")!;
    return {
      outerDisplay: getComputedStyle(outer).display,
      addRight: Math.abs(outer.getBoundingClientRect().right - add.getBoundingClientRect().right),
      titleRight: title.getBoundingClientRect().right,
      addLeft: add.getBoundingClientRect().left,
      badgeOffset: tileHeading.querySelector(".session-information-secret")!.getBoundingClientRect().top - tileHeading.getBoundingClientRect().top,
    };
  });
  expect(headingLayout.outerDisplay).toBe("flex");
  expect(headingLayout.addRight).toBeLessThanOrEqual(1);
  expect(headingLayout.addLeft).toBeGreaterThan(headingLayout.titleRight);
  expect(headingLayout.badgeOffset).toBeLessThanOrEqual(1);
  const normal = tiles.filter({ hasText: "Server" });
  const secret = tiles.filter({ hasText: "Password" });
  await expect(normal.getByRole("button", { name: "Copy Server" })).toBeVisible();
  await expect(normal.getByRole("button", { name: /Reveal/ })).toHaveCount(0);
  await expect(secret.getByRole("button", { name: "Copy Password" })).toBeVisible();
  await expect(secret.getByRole("button", { name: "Reveal Password" })).toBeVisible();
  for (const keyText of ["Server", "Password"]) {
    await expect(page.getByRole("link", { name: `Edit ${keyText}` })).toBeVisible();
    await expect(page.getByRole("button", { name: `Delete ${keyText}` })).toBeVisible();
  }
  await expect(tiles.nth(3).locator("[data-secret-value]")).toHaveText("••••••••");
  await expect(tiles.nth(0)).toContainText("Server");
  await expect(tiles.nth(4)).toContainText("Long value");

  const requestID = await page.locator("[data-game-request-detail]").getAttribute("data-game-request-id");
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

  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto(`/game-requests/${requestID}`);
  await expect(page.locator("#session-information").getByRole("link", { name: "Edit Server" })).toBeVisible();
  await expect(page.locator("#session-information").getByRole("button", { name: "Delete Server" })).toBeVisible();
  await page.locator("#session-information").getByRole("link", { name: "Edit Server" }).click();
  await expect(page.locator("#session-information .session-information-form")).toHaveCount(1);
  const editorWidth = await page.locator("#session-information").evaluate((section) => {
    const sectionRect = section.getBoundingClientRect();
    const editorRect = section.querySelector<HTMLFormElement>("form.session-information-form")!.getBoundingClientRect();
    return { section: sectionRect.width, editor: editorRect.width, offset: editorRect.left - sectionRect.left };
  });
  expect(editorWidth.editor).toBeGreaterThanOrEqual(editorWidth.section - 1);
  expect(editorWidth.offset).toBeLessThanOrEqual(1);
  await expect(page.locator("#session-information form.session-information-form")).toBeVisible();
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

  const tile = page.locator(".session-information-tile");
  const value = tile.locator("[data-secret-value]");
  await expect(tile).toContainText("Secret");
  await expect(value).toHaveText("••••••••");
  await tile.getByRole("button", { name: "Copy Password" }).click();
  await expect.poll(() => page.evaluate(() => window.__copiedSecret)).toBe("shoulder-secret");
  const notices = page.locator("#live-notices");
  await expect(notices).toContainText("Secret copied.");
  await expect(notices).not.toContainText("shoulder-secret");
  await expect(value).toHaveText("••••••••");

  await tile.getByRole("button", { name: "Reveal Password" }).click();
  const hide = tile.getByRole("button", { name: "Hide Password" });
  await expect(hide).toHaveAttribute("aria-pressed", "true");
  await hide.click();
  await expect(value).toHaveText("••••••••");

  await page.clock.install();
  await tile.getByRole("button", { name: "Reveal Password" }).click();
  await page.clock.fastForward(10_000);
  await tile.getByRole("button", { name: "Copy Password" }).click();
  await expect(notices).toContainText("Secret copied.");
  await expect(value).toHaveText("shoulder-secret");
  await page.clock.fastForward(10_000);
  await expect(value).toHaveText("••••••••");
  await tile.getByRole("button", { name: "Reveal Password" }).click();
  await expect(value).toHaveText("shoulder-secret");
  const requestID = await page.locator("[data-game-request-detail]").getAttribute("data-game-request-id");
  await request.post(`/__test__/publish?type=GameRequestSessionInformationChanged&request=${requestID}`);
  await expect(value).toHaveText("••••••••");
  await tile.getByRole("button", { name: "Reveal Password" }).click();
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

  const tile = page.locator(".session-information-tile");
  const value = tile.locator("[data-secret-value]");
  await tile.getByRole("button", { name: "Copy Password" }).click();
  const notices = page.locator("#live-notices");
  await expect(notices).toContainText("Could not copy secret. Please try again.");
  await expect(notices).not.toContainText("shoulder-secret");
  await expect(value).toHaveText("••••••••");

  await page.evaluate(() => {
    Object.defineProperty(Navigator.prototype, "clipboard", { configurable: true, get: () => undefined });
  });
  await tile.getByRole("button", { name: "Copy Password" }).click();
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
    await expect(page.locator("#session-information [data-session-information-form]")).toHaveAttribute("data-server-is-secret", "false");

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
    await expect(page.locator("#session-information [data-session-information-form]")).toHaveAttribute("data-server-is-secret", "false");

    await key.fill("Password");
    await page.getByRole("button", { name: "Save" }).click();
    await expect(page.locator(".session-information-tile[data-secret='true']")).toContainText("Secret");
    await expect(page.locator(".session-information-tile[data-secret='true'] [data-secret-value]")).toHaveAttribute("data-revealed", "false");
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

  const tile = page.locator(".session-information-tile");
  await tile.getByRole("button", { name: "Copy Password" }).click();
  await expect.poll(() => page.evaluate(() => window.__copiedSecret)).toBe("shoulder-secret");
  await expect(tile.locator("[data-secret-value]")).toHaveText("••••••••");
  await expect(page.locator("#live-notices")).toContainText("Secret copied.");
  await expect(page.locator("textarea")).toHaveCount(0);
  await page.evaluate(() => {
    Object.defineProperty(Navigator.prototype, "clipboard", { configurable: true, get: () => undefined });
  });
  await tile.getByRole("button", { name: "Copy Password" }).click();
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

  const tiles = page.locator(".session-information-tile");
  const lobby = tiles.filter({ hasText: "Lobby password" });
  const voice = tiles.filter({ hasText: "Voice password" });
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
