import { expect, test } from "@playwright/test";
import { resetFixture } from "./helpers";

// The theme toggle swaps its Phosphor glyph by class from app.ts, which Go
// rendering tests cannot observe. The accessible name must follow the theme.

test.beforeEach(async ({ request }) => {
  await resetFixture(request);
});

test("the theme toggle swaps its icon and keeps its accessible name", async ({ page }) => {
  await page.emulateMedia({ colorScheme: "dark" });
  await page.goto("/game-requests");

  const toggle = page.locator("[data-theme-toggle]:visible").first();
  const icon = toggle.locator("[data-theme-icon]");
  await expect(toggle).toHaveAttribute("aria-label", "Use light theme");
  await expect(icon).toHaveClass(/\bph-moon\b/);

  await toggle.click();

  await expect(page.locator("html")).toHaveAttribute("data-theme", "lansurfer-light");
  await expect(toggle).toHaveAttribute("aria-label", "Use dark theme");
  await expect(toggle).toHaveAttribute("title", "Use dark theme");
  await expect(icon).toHaveClass(/\bph-sun\b/);
  await expect(icon).not.toHaveClass(/\bph-moon\b/);

  // The server stores the choice as an HttpOnly cookie (Twitch Developer
  // Services Agreement cookie rules), so the next document renders it.
  await expect.poll(async () => (await page.context().cookies()).find((cookie) => cookie.name === "lansurfer_theme")).toMatchObject({
    value: "lansurfer-light",
    httpOnly: true,
  });
  await page.reload();
  await expect(page.locator("html")).toHaveAttribute("data-theme", "lansurfer-light");
});
