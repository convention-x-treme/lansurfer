import { expect, test } from "@playwright/test";
import { resetFixture } from "./helpers";

// The delete button is gated from app.ts, which Go tests cannot observe. The
// server compares the nick again, so this only covers the browser-side gate.

test.beforeEach(async ({ request }) => {
  await resetFixture(request);
});

test("account deletion stays disabled until the Game Nick matches exactly", async ({ page }) => {
  await page.goto("/account");

  const form = page.locator("form[data-delete-account]");
  const nick = (await form.getAttribute("data-delete-account"))!;
  const input = form.locator("input[name='confirm_game_nick']");
  const button = form.getByRole("button", { name: "Delete account" });

  await expect(button).toBeDisabled();
  await input.fill(nick.toLowerCase() === nick ? nick.toUpperCase() : nick.toLowerCase());
  await expect(button).toBeDisabled();
  await input.fill(nick + " ");
  await expect(button).toBeDisabled();
  await input.fill(nick);
  await expect(button).toBeEnabled();
});
