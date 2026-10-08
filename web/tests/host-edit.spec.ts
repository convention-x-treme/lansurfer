import { expect, test } from "@playwright/test";
import { resetFixture } from "./helpers";

const otherViewerState = {
  cookies: [{
    name: "lansurfer_session",
    value: "browser-test-other",
    domain: "127.0.0.1",
    path: "/",
    expires: -1,
    httpOnly: true,
    secure: false,
    sameSite: "Lax" as const,
  }],
  origins: [],
};

test.beforeEach(async ({ request }) => {
  await resetFixture(request);
});

test("detail layout spans hero and stacks lower content without overflow", async ({ page }) => {
  await page.goto("/game-requests/new?game_id=game-1");
  await page.getByLabel("Max players").fill("4");
  await page.getByRole("button", { name: "Create Game Request" }).click();
  await page.waitForURL(/\/game-requests\/[0-9a-f-]+(?:\?.*)?$/);

  for (const width of [1440, 900]) {
    await page.setViewportSize({ width, height: 900 });
    const layout = await page.evaluate(() => {
      const hero = document.querySelector(".detail-hero")!.getBoundingClientRect();
      const grid = document.querySelector(".detail-lower-grid")!.getBoundingClientRect();
      const players = document.querySelector("#players-title")!.closest("section")!.getBoundingClientRect();
      const lobby = document.querySelector("#main-lobby")!.getBoundingClientRect();
      return {
        heroLeft: hero.left, gridLeft: grid.left, heroRight: hero.right, gridRight: grid.right,
        playersLeft: players.left, lobbyLeft: lobby.left, playersWidth: players.width, lobbyWidth: lobby.width,
        scrollWidth: document.documentElement.scrollWidth, clientWidth: document.documentElement.clientWidth,
      };
    });
    expect(Math.abs(layout.heroLeft - layout.gridLeft)).toBeLessThanOrEqual(1);
    expect(Math.abs(layout.heroRight - layout.gridRight)).toBeLessThanOrEqual(1);
    expect(layout.lobbyLeft).toBeGreaterThan(layout.playersLeft);
    expect(layout.lobbyWidth).toBeGreaterThan(layout.playersWidth);
    expect(layout.scrollWidth).toBeLessThanOrEqual(layout.clientWidth + 1);
  }

  await page.setViewportSize({ width: 390, height: 844 });
  const mobile = await page.evaluate(() => {
    const top = (selector: string) => document.querySelector(selector)!.getBoundingClientRect().top;
    const buttons = Array.from(document.querySelectorAll(".detail-actions button"));
    const title = document.querySelector(".detail-title")!;
    const titleLines = title.getBoundingClientRect().height / parseFloat(getComputedStyle(title).lineHeight);
    return {
      hero: top(".detail-hero"), players: top("#players-title"), lobby: top("#main-lobby"),
      titleLines,
      scrollWidth: document.documentElement.scrollWidth, clientWidth: document.documentElement.clientWidth,
      touchTargets: buttons.map((button) => {
        const rect = button.getBoundingClientRect();
        return { width: rect.width, height: rect.height };
      }),
    };
  });
  expect(mobile.titleLines).toBeLessThanOrEqual(1.05);
  expect(mobile.players).toBeGreaterThan(mobile.hero);
  expect(mobile.lobby).toBeGreaterThan(mobile.players);
  expect(mobile.scrollWidth).toBeLessThanOrEqual(mobile.clientWidth + 1);
  expect(mobile.touchTargets.every(({ width, height }) => width >= 44 && height >= 44)).toBe(true);
  await expect(page.getByRole("button", { name: "Start game" })).toBeVisible();
  await expect(page.getByRole("link", { name: "Edit" })).toBeVisible();
  await expect(page.getByRole("button", { name: "Cancel Game Request" })).toBeVisible();
  await expect(page.getByText("Host actions", { exact: true })).toHaveCount(0);
  await expect(page.getByText("Follow Game", { exact: true })).toHaveCount(0);
});
test("Host edits open details, then only capacity and description while Playing", async ({ page }) => {
  await page.goto("/game-requests/new?game_id=game-1");
  await page.getByLabel("Max players").fill("4");
  await page.getByRole("button", { name: "Create Game Request" }).click();
  await page.waitForURL(/\/game-requests\/[0-9a-f-]+(?:\?.*)?$/);

  await page.getByRole("link", { name: "Edit" }).click();
  const dialog = page.locator("#game-request-edit-dialog");
  await expect(dialog).toBeVisible();
  await expect(page).not.toHaveURL(/edit=1/);
  await expect(dialog.getByRole("heading", { name: "Edit Game Request" })).toBeVisible();
  await expect(dialog.locator("[name='game_id'], [name='game']")).toHaveCount(0);
  const date = page.locator("input[name='start_date']");
  const time = page.locator("input[name='start_time']");
  const storedStart = await page.locator("#game-request-detail time[data-local-time='start']").getAttribute("datetime");
  const localized = await page.evaluate((value) => {
    const start = new Date(value ?? "");
    const pad = (part: number) => String(part).padStart(2, "0");
    return {
      date: `${start.getFullYear()}-${pad(start.getMonth() + 1)}-${pad(start.getDate())}`,
      time: `${pad(start.getHours())}:${pad(start.getMinutes())}`,
    };
  }, storedStart);
  await expect(date).toHaveValue(localized.date);
  await expect(time).toHaveValue(localized.time);

  await page.getByLabel("Max players").fill("6");
  await date.fill("2099-05-01");
  await time.fill("18:30");
  await page.getByLabel("Description").fill("Updated plan");
  const expectedStart = await page.evaluate(() => {
    const dateValue = document.querySelector<HTMLInputElement>("input[name='start_date']")?.value;
    const timeValue = document.querySelector<HTMLInputElement>("input[name='start_time']")?.value;
    return new Date(`${dateValue}T${timeValue}`).toISOString().replace(".000Z", "Z");
  });
  await page.getByRole("button", { name: "Save" }).click();
  await expect(page.locator("#game-request-edit-dialog")).toBeHidden();
  await expect(page.locator("#game-request-detail")).toContainText("1/6");
  await expect(page.locator("#game-request-detail")).toContainText("Updated plan");
  await expect(page.locator("#game-request-detail time[data-local-time='start']")).toHaveAttribute("datetime", expectedStart);
  await expect(page.locator("#game-request-detail input[name='max_players']")).toHaveCount(0);

  page.once("dialog", (dialog) => dialog.accept());
  await page.getByRole("button", { name: "Start game" }).click();
  await expect(page.getByText("Playing", { exact: true }).first()).toBeVisible();
  await expect(page.getByRole("button", { name: "Close game" })).toBeVisible();
  await expect(page.getByRole("button", { name: "Cancel Game Request" })).toHaveCount(0);
  await page.getByRole("link", { name: "Edit" }).click();
  await expect(page.locator("#game-request-edit-dialog")).toBeVisible();
  await expect(page.getByLabel("Max players")).toHaveValue("6");
  await expect(page.getByLabel("Description")).toHaveValue("Updated plan");
  await expect(page.locator("input[name='start_date'], input[name='start_time']")).toHaveCount(0);
  const playingStart = await page.locator("form[action$='/details'] time[data-local-time='absolute']").getAttribute("datetime");
  expect(playingStart).toBe(expectedStart);

  await page.getByLabel("Max players").fill("5");
  await page.getByLabel("Description").fill("Playing update");
  await page.getByRole("button", { name: "Save" }).click();
  await expect(page.locator("#game-request-edit-dialog")).toBeHidden();
  await expect(page.locator("#game-request-detail")).toContainText("1/5");
  await expect(page.locator("#game-request-detail")).toContainText("Playing update");
  await expect(page.locator("#game-request-detail time[data-local-time='start']")).toHaveAttribute("datetime", expectedStart);

  await page.getByRole("link", { name: "Edit" }).click();
  await expect(page.locator("#game-request-edit-dialog")).toBeVisible();
  await page.getByLabel("Max players").fill("8");
  await page.getByLabel("Description").fill("Discard this draft");
  await page.getByRole("button", { name: "Cancel" }).click();
  await expect(page.locator("#game-request-edit-dialog")).toBeHidden();
  await expect(page.locator("#game-request-detail")).toContainText("1/5");
  await expect(page.locator("#game-request-detail")).toContainText("Playing update");
});
test("Host saves a relative start shortcut", async ({ page }) => {
  await page.goto("/game-requests/new?game_id=game-1");
  await page.getByLabel("Max players").fill("4");
  await page.getByRole("button", { name: "Create Game Request" }).click();
  await page.waitForURL(/\/game-requests\/[0-9a-f-]+(?:\?.*)?$/);
  const detailURL = page.url();
  const before = new Date((await page.request.get(detailURL)).headers()["date"]!);
  await page.getByRole("link", { name: "Edit" }).click();
  await expect(page.locator("#game-request-edit-dialog")).toBeVisible();
  await expect(page.locator("input[name='start_preset'][value='custom']")).toBeChecked();
  await page.locator("label.start-choice").filter({ hasText: "in 15 min" }).click();
  await expect(page.locator("[data-custom-start]")).toBeHidden();
  await page.getByRole("button", { name: "Save" }).click();
  await expect(page.locator("#game-request-edit-dialog")).toBeHidden();
  const after = new Date((await page.request.get(page.url())).headers()["date"]!);
  const stored = new Date((await page.locator("#game-request-detail time[data-local-time='start']").getAttribute("datetime"))!);
  expect(stored.getTime()).toBeGreaterThanOrEqual(before.getTime() + 15 * 60_000 - 1_000);
  expect(stored.getTime()).toBeLessThanOrEqual(after.getTime() + 15 * 60_000 + 1_000);
});
test("planned-start activity stays inside a narrow rail", async ({ page }) => {
  await page.goto("/game-requests/new?game_id=game-1");
  await page.getByLabel("Max players").fill("4");
  await page.locator("label.start-choice").filter({ hasText: "Pick date & time" }).click();
  await page.locator("input[name='start_date']").fill("2099-05-01");
  await page.locator("input[name='start_time']").fill("18:30");
  await page.getByRole("button", { name: "Create Game Request" }).click();
  await page.waitForURL(/\/game-requests\/[0-9a-f-]+(?:\?.*)?$/);
  const requestID = await page.locator("#game-request-detail").getAttribute("data-game-request-id");
  await page.getByRole("link", { name: "Edit" }).click();
  await expect(page.locator("#game-request-edit-dialog")).toBeVisible();
  const form = page.locator("form[action$='/details']");
  const csrf = await form.locator("input[name='csrf_token']").inputValue();
  const timezone = await form.locator("input[name='timezone']").inputValue();
  const date = await form.locator("input[name='start_date']").inputValue();
  const description = await form.locator("textarea[name='description']").inputValue();
  for (const [capacity, startTime] of [["4", "19:00"], ["6", "20:00"]]) {
    const response = await page.request.post(`/game-requests/${requestID}/details`, {
      form: { csrf_token: csrf, max_players: capacity, start_date: date, start_time: startTime, timezone, description },
      maxRedirects: 0,
    });
    expect(response.status()).toBe(303);
  }
  for (const width of [390, 1440]) {
    await page.setViewportSize({ width, height: 900 });
    await page.goto(`/game-requests/${requestID}`);
    for (const label of ["Ada Player changed the planned start", "Ada Player updated capacity and planned start"]) {
      const row = page.locator(".activity-row").filter({ hasText: label });
      const content = row.locator(".activity-content");
      const detail = content.locator(".activity-detail");
      await expect(content.locator("p").first()).toContainText(label);
      await expect(detail.locator("time[data-local-time='absolute']")).toHaveCount(2);
      const layout = await row.evaluate((element) => {
        const content = element.querySelector(".activity-content")!;
        const card = element.closest(".detail-card")!;
        const bounds = (item: Element) => {
          const rect = item.getBoundingClientRect();
          return { left: rect.left, right: rect.right, scroll: item.scrollWidth, client: item.clientWidth };
        };
        return {
          row: bounds(element),
          content: bounds(content),
          card: bounds(card),
          times: [...content.querySelectorAll("time")].map((time) => bounds(time)),
          document: document.documentElement.scrollWidth - document.documentElement.clientWidth,
        };
      });
      expect(layout.row.left).toBeGreaterThanOrEqual(layout.card.left - 1);
      expect(layout.row.right).toBeLessThanOrEqual(layout.card.right + 1);
      expect(layout.content.scroll).toBeLessThanOrEqual(layout.content.client + 1);
      expect(layout.times.every((time) => time.left >= layout.card.left - 1 && time.right <= layout.card.right + 1)).toBe(true);
      expect(layout.document).toBeLessThanOrEqual(1);
    }
  }
});

test("Playing request still supports Join, Leave, and Full states", async ({ page, browser }) => {
  await page.goto("/game-requests?q=Age%20of%20Empires%20II");
  const row = page.locator("article[data-game-request-id]").filter({ hasText: "Age of Empires II" });
  await row.getByRole("button", { name: "Join" }).click();
  await expect(row.getByRole("button", { name: "Leave" })).toBeVisible();

  const otherContext = await browser.newContext({ baseURL: "http://127.0.0.1:8123", storageState: otherViewerState });
  const otherPage = await otherContext.newPage();
  await otherPage.goto("/game-requests?q=Age%20of%20Empires%20II");
  const otherRow = otherPage.locator("article[data-game-request-id]").filter({ hasText: "Age of Empires II" });
  await expect(otherRow.getByText("Full", { exact: true }).last()).toBeVisible();

  await row.getByRole("button", { name: "Leave" }).click();
  await expect(otherRow.getByRole("button", { name: "Join" })).toBeVisible();
  await otherContext.close();
});
test("no-JavaScript edit URL keeps inline Save and Cancel behavior", async ({ page, browser }) => {
  await page.goto("/game-requests/new?game_id=game-1");
  await page.getByLabel("Max players").fill("4");
  await page.getByRole("button", { name: "Create Game Request" }).click();
  await page.waitForURL(/\/game-requests\/[0-9a-f-]+(?:\?.*)?$/);
  const detailURL = new URL(page.url());
  const editURL = `${detailURL.pathname}?edit=1`;
  const context = await browser.newContext({
    baseURL: "http://127.0.0.1:8123",
    storageState: await page.context().storageState(),
    javaScriptEnabled: false,
  });
  const noJS = await context.newPage();
  await noJS.goto(editURL);
  await expect(noJS.getByLabel("Max players")).toHaveValue("4");
  await noJS.getByLabel("Max players").fill("6");
  await noJS.getByLabel("Description").fill("No-JS update");
  await noJS.getByRole("button", { name: "Save" }).click();
  await noJS.waitForURL(editURL.replace("?edit=1", ""));
  await expect(noJS.locator("#game-request-detail")).toContainText("1/6");
  await expect(noJS.locator("#game-request-detail")).toContainText("No-JS update");
  await noJS.goto(editURL);
  await noJS.getByLabel("Max players").fill("8");
  await noJS.getByRole("link", { name: "Cancel" }).click();
  await noJS.waitForURL(editURL.replace("?edit=1", ""));
  await expect(noJS.locator("#game-request-detail")).toContainText("1/6");
  await context.close();
});
test("dialog close controls discard draft and return focus to Edit", async ({ page }) => {
  await page.goto("/game-requests/new?game_id=game-1");
  await page.getByLabel("Max players").fill("4");
  await page.getByRole("button", { name: "Create Game Request" }).click();
  await page.waitForURL(/\/game-requests\/[0-9a-f-]+(?:\?.*)?$/);

  for (const close of ["Cancel", "Close dialog", "Escape", "Backdrop"]) {
    const edit = page.getByRole("link", { name: "Edit" });
    await edit.click();
    const dialog = page.locator("#game-request-edit-dialog");
    await expect(dialog).toBeVisible();
    await page.getByLabel("Max players").fill("8");
    if (close === "Cancel") await dialog.getByRole("button", { name: close, exact: true }).click();
    else if (close === "Close dialog") await page.getByRole("button", { name: close }).click();
    else if (close === "Escape") await page.keyboard.press("Escape");
    else await page.mouse.click(1, 1);
    await expect(dialog).toBeHidden();
    await expect(edit).toBeFocused();
    await edit.click();
    await expect(page.getByLabel("Max players")).toHaveValue("4");
    await page.keyboard.press("Escape");
  }
});
