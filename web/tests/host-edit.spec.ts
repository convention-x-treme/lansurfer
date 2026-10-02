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

test("Host edits open details, then only capacity and description while Playing", async ({ page }) => {
  await page.goto("/game-requests/new?game_id=game-1");
  await page.getByLabel("Max players").fill("4");
  await page.getByRole("button", { name: "Create Game Request" }).click();
  await page.waitForURL(/\/game-requests\/[0-9a-f-]+(?:\?.*)?$/);

  await page.getByRole("link", { name: "Edit" }).click();
  await page.waitForURL(/\?edit=1$/);
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

  await page.getByLabel("Maximum players").fill("6");
  await date.fill("2099-05-01");
  await time.fill("18:30");
  await page.getByLabel("Description").fill("Updated plan");
  const expectedStart = await page.evaluate(() => {
    const dateValue = document.querySelector<HTMLInputElement>("input[name='start_date']")?.value;
    const timeValue = document.querySelector<HTMLInputElement>("input[name='start_time']")?.value;
    return new Date(`${dateValue}T${timeValue}`).toISOString().replace(".000Z", "Z");
  });
  await page.getByRole("button", { name: "Save" }).click();
  await page.waitForURL(/\/game-requests\/[0-9a-f-]+(?:\?.*)?$/);
  await expect(page.locator("#game-request-detail")).toContainText("1/6");
  await expect(page.locator("#game-request-detail")).toContainText("Updated plan");
  await expect(page.locator("#game-request-detail time[data-local-time='start']")).toHaveAttribute("datetime", expectedStart);
  await expect(page.locator("#game-request-detail input[name='max_players']")).toHaveCount(0);

  page.once("dialog", (dialog) => dialog.accept());
  await page.getByRole("button", { name: "Start game" }).click();
  await expect(page.getByText("Playing", { exact: true }).first()).toBeVisible();
  await page.getByRole("link", { name: "Edit" }).click();
  await page.waitForURL(/\?edit=1$/);
  await expect(page.getByLabel("Maximum players")).toHaveValue("6");
  await expect(page.getByLabel("Description")).toHaveValue("Updated plan");
  await expect(page.locator("input[name='start_date'], input[name='start_time']")).toHaveCount(0);
  const playingStart = await page.locator("form[action$='/details'] time[data-local-time='absolute']").getAttribute("datetime");
  expect(playingStart).toBe(expectedStart);

  await page.getByLabel("Maximum players").fill("5");
  await page.getByLabel("Description").fill("Playing update");
  await page.getByRole("button", { name: "Save" }).click();
  await page.waitForURL(/\/game-requests\/[0-9a-f-]+(?:\?.*)?$/);
  await expect(page.locator("#game-request-detail")).toContainText("1/5");
  await expect(page.locator("#game-request-detail")).toContainText("Playing update");
  await expect(page.locator("#game-request-detail time[data-local-time='start']")).toHaveAttribute("datetime", expectedStart);

  await page.getByRole("link", { name: "Edit" }).click();
  await page.waitForURL(/\?edit=1$/);
  await page.getByLabel("Maximum players").fill("8");
  await page.getByLabel("Description").fill("Discard this draft");
  await page.getByRole("link", { name: "Cancel" }).click();
  await page.waitForURL(/\/game-requests\/[0-9a-f-]+(?:\?.*)?$/);
  await expect(page.locator("#game-request-detail")).toContainText("1/5");
  await expect(page.locator("#game-request-detail")).toContainText("Playing update");
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
