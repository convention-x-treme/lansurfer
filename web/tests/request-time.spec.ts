import { expect, test } from "@playwright/test";
import { resetFixture } from "./helpers";

const requestID = "00000000-0000-0000-0000-000000000003";
const iso = (date: Date): string => date.toISOString().replace(/\.\d{3}Z$/, "Z");
const actual = iso(new Date(Date.now() - 3 * 60_000));

async function setLifecycle(
  request: Parameters<typeof resetFixture>[0],
  status: string,
  plan: Date,
  startedAt?: string,
): Promise<void> {
  const query = new URLSearchParams({ status, starts_at: plan.toISOString() });
  query.set("joined", "1");
  if (startedAt) query.set("started_at", startedAt);
  const response = await request.get(`/__test__/lifecycle?${query}`);
  expect(response.ok()).toBeTruthy();
}

test.beforeEach(async ({ request }) => {
  await resetFixture(request);
});

test("playing time uses actual start across list, detail, and active-games navigation", async ({ page, request }) => {
  await setLifecycle(request, "playing", new Date(Date.now() - 60 * 60_000), actual);
  await page.goto("/game-requests");
  await expect(page.getByRole("heading", { name: "Your active games" })).toBeVisible();

  for (const surface of [
    page.locator(`#game-request-${requestID} time[data-local-time='start']`),
    page.locator(`a.nav-upcoming-item[href='/game-requests/${requestID}'] time[data-local-time='start']`).first(),
  ]) {
    await expect(surface).toContainText(/^started \d+ min ago$/);
    await expect(surface).toHaveAttribute("datetime", actual);
    await expect(surface).toHaveAttribute("title", /\d{4}-\d{2}-\d{2}/);
    await expect(surface).not.toContainText(/starts now|starts in/);
    await expect(surface).not.toHaveClass(/count-(hot|last)|nav-upcoming-item-(now|soon)/);
  }
  await expect(page.locator(`a.nav-upcoming-item[href='/game-requests/${requestID}']`).first()).not.toHaveClass(/nav-upcoming-item-(now|soon)/);

  await page.goto(`/game-requests/${requestID}`);
  const detailTime = page.locator("#game-request-detail time[data-local-time='start']");
  await expect(detailTime).toContainText(/^started \d+ min ago$/);
  await expect(detailTime).toHaveAttribute("datetime", actual);
  await expect(detailTime).not.toContainText(/starts now|starts in/);
  await expect(detailTime).not.toHaveClass(/count-(hot|last)/);
});

test("playing with future plan and missing actual start never shows planned countdown", async ({ page, request }) => {
  await setLifecycle(request, "playing", new Date(Date.now() + 60 * 60_000));
  await page.goto("/game-requests");

  for (const surface of [
    page.locator(`#game-request-${requestID} span`).filter({ hasText: /^playing$/ }).first(),
    page.locator(`a.nav-upcoming-item[href='/game-requests/${requestID}'] span`).filter({ hasText: /^playing$/ }).first(),
  ]) {
    await expect(surface).toBeVisible();
    await expect(surface).not.toHaveClass(/count-(hot|last)|nav-upcoming-item-(now|soon)/);
  }

  await page.goto(`/game-requests/${requestID}`);
  await expect(page.locator("time[data-local-time='start']")).toHaveCount(0);
  await expect(page.locator("main")).toContainText("playing");
  await expect(page.locator("main")).not.toContainText(/starts now|starts in/);
});

test("playing with future plan and actual start still uses actual time", async ({ page, request }) => {
  await setLifecycle(request, "playing", new Date(Date.now() + 60 * 60_000), actual);
  await page.goto("/game-requests");
  const listTime = page.locator(`#game-request-${requestID} time[data-local-time='start']`);
  const upcomingTime = page.locator(`a.nav-upcoming-item[href='/game-requests/${requestID}'] time[data-local-time='start']`).first();
  for (const surface of [listTime, upcomingTime]) {
    await expect(surface).toContainText(/^started \d+ min ago$/);
    await expect(surface).toHaveAttribute("datetime", actual);
    await expect(surface).not.toContainText(/starts now|starts in/);
  }
  await page.goto(`/game-requests/${requestID}`);
  const detailTime = page.locator("#game-request-detail time[data-local-time='start']");
  await expect(detailTime).toContainText(/^started \d+ min ago$/);
  await expect(detailTime).toHaveAttribute("datetime", actual);
  await expect(detailTime).not.toContainText(/starts now|starts in/);
});

test("closed actual start uses historical time and legacy closed stays planned", async ({ page, request }) => {
  const plan = new Date(Date.now() + 60 * 60_000);
  await setLifecycle(request, "closed", plan, actual);
  await page.goto(`/game-requests/${requestID}`);
  let time = page.locator("time[data-local-time='start']");
  await expect(time).toContainText(/^started \d+ min ago$/);
  await expect(time).toHaveAttribute("datetime", actual);
  await expect(time).not.toHaveClass(/count-(hot|last)/);

  await setLifecycle(request, "closed", plan);
  await page.reload();
  time = page.locator("time[data-local-time='start']");
  await expect(time).toContainText(/^planned /);
  await expect(time).toHaveAttribute("datetime", iso(plan));
});
test.describe("future planned start localization", () => {
  test.use({ timezoneId: "Europe/Berlin" });

  test("shows local date and clock across list, detail, and navigation beyond 24 hours", async ({ page, request }) => {
    const plan = new Date(Date.now() + 48 * 60 * 60_000 + 90 * 60_000);
    await setLifecycle(request, "open", plan);
    await page.goto("/game-requests");
    const local = new Intl.DateTimeFormat("en-US", {
      timeZone: "Europe/Berlin", month: "short", day: "numeric", hour: "2-digit", minute: "2-digit", hourCycle: "h23",
    }).format(plan).replace(",", " at");
    for (const surface of [
      page.locator(`#game-request-${requestID} time[data-local-time='start']`),
      page.locator(`a.nav-upcoming-item[href='/game-requests/${requestID}'] time[data-local-time='start']`).first(),
    ]) {
      await expect(surface).toHaveText(`starts ${local}`);
      await expect(surface).toHaveAttribute("datetime", iso(plan));
      await expect(surface).toHaveAttribute("title", /\d{4}-\d{2}-\d{2} \d{2}:\d{2}/);
    }
    await page.goto(`/game-requests/${requestID}`);
    const detail = page.locator("#game-request-detail time[data-local-time='start']");
    await expect(detail).toHaveText(`starts ${local}`);
    await expect(detail).toHaveAttribute("datetime", iso(plan));
  });
});
