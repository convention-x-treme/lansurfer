import { expect, test } from "@playwright/test";
import { resetFixture } from "./helpers";

// Issue #105: while the mobile drawer is open, scrolling moves the menu and
// never the page behind it; closing the drawer gives scrolling back. Scroll
// chaining and the :has() page lock are browser behaviour Go tests cannot see.

test.beforeEach(async ({ request }) => {
  await resetFixture(request);
});

test("the open mobile navigation scrolls on its own and freezes the page", async ({ page }) => {
  // Short enough that both the menu and the page overflow.
  await page.setViewportSize({ width: 390, height: 320 });
  await page.goto("/game-requests");
  const panel = page.locator(".mobile-drawer-panel");

  await page.getByLabel("Open navigation").click();
  await expect(panel).toBeInViewport();
  const box = (await panel.boundingBox())!;
  await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);

  // Far past the end of the menu: the menu scrolls, the excess must not chain.
  await page.mouse.wheel(0, 5000);
  await expect.poll(() => panel.evaluate((element) => element.scrollTop)).toBeGreaterThan(0);
  expect(await page.evaluate(() => window.scrollY)).toBe(0);

  await page.getByLabel("Close navigation").click();
  await page.mouse.wheel(0, 400);
  await expect.poll(() => page.evaluate(() => window.scrollY)).toBeGreaterThan(0);
});
