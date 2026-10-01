import { expect, test } from "@playwright/test";
import { resetFixture } from "./helpers";

// The server renders UTC fallbacks; app.ts rewrites them into the browser's
// zone. Go tests cannot observe that, and a server in UTC against a Berlin
// browser is exactly the mismatch of issue #114.

test.use({ timezoneId: "Europe/Berlin" });

test.beforeEach(async ({ request }) => {
  await resetFixture(request);
});

test("absolute times follow the browser zone, not the server's", async ({ page }) => {
  await page.goto("/game-requests");

  const start = page.locator("time[data-local-time='start']").first();
  const iso = (await start.getAttribute("datetime"))!;
  const berlin = new Intl.DateTimeFormat("sv-SE", {
    timeZone: "Europe/Berlin", year: "numeric", month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit",
  }).format(new Date(iso));
  expect(berlin).toMatch(/^\d{4}-\d{2}-\d{2} \d{2}:\d{2}$/);
  await expect(start).toHaveAttribute("title", berlin);
});

test("the composer posts the browser's IANA zone, not a fixed offset", async ({ page }) => {
  await page.goto("/game-requests/new?game_id=game-1");
  await expect(page.locator("input[name='timezone']")).toHaveValue("Europe/Berlin");
});
