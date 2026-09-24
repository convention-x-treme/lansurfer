import { expect, test } from "@playwright/test";

const settings = (current: boolean, devices: number, enabled: boolean) => `<!doctype html><html><body>
<section data-vapid-public-key="AQID" data-current-device-id="${current ? 12 : 0}" data-device-count="${devices}" data-push-enabled="${enabled}">
<ul>
<li data-current-device="true"><form method="post" action="/account/notifications/devices/12/remove" data-current-device-remove="true"><input name="csrf_token" value="test"><button type="submit">Remove current</button></form></li>
<li data-current-device="false"><form method="post" action="/account/notifications/devices/13/remove"><input name="csrf_token" value="test"><button type="submit">Remove other</button></form></li>
</ul>
<form method="post" action="/account/notifications/subscribe" data-subscribe-form>
<input name="csrf_token" value="test"><input name="endpoint" data-subscribe-endpoint><input name="p256dh" data-subscribe-p256dh><input name="auth" data-subscribe-auth>
<button type="button" data-subscribe-button>Enable on this device</button><p role="status" data-push-status hidden></p></form></section>
<script defer src="/assets/app.js"></script></body></html>`;

for (const state of [
  { name: "revoked permission", permission: "denied", subscription: true },
  { name: "missing browser subscription", permission: "granted", subscription: false },
]) {
  test(`${state.name} reconciles only known current device without prompting`, async ({ page }) => {
    let reconciles = 0;
    let current = true;
    await page.route("**/account/notifications/reconcile", async (route) => {
      reconciles++;
      current = false;
      expect(route.request().postData()).toContain("csrf_token=test");
      expect(route.request().postData()).not.toContain("deviceID");
      await route.fulfill({ status: 200, body: "ok" });
    });
    await page.route("**/account/notifications", (route) => route.fulfill({ contentType: "text/html", body: settings(current, current ? 2 : 1, true) }));
    await page.addInitScript(({ permission, subscription }) => {
      Object.defineProperty(window, "Notification", { configurable: true, value: { permission, requestPermission: () => { throw Error("unexpected prompt"); } } });
      Object.defineProperty(window, "PushManager", { configurable: true, value: function PushManager() {} });
      Object.defineProperty(navigator, "serviceWorker", { configurable: true, value: { register: async () => ({ pushManager: { getSubscription: async () => subscription ? { endpoint: "https://push.example" } : null } }) } });
    }, state);
    await page.goto("/account/notifications");
    await expect.poll(() => reconciles).toBe(1);
  });
}

test("other device without local subscription is not reconciled", async ({ page }) => {
  let reconciles = 0;
  await page.route("**/account/notifications/reconcile", (route) => { reconciles++; return route.fulfill({ status: 200 }); });
  await page.route("**/account/notifications", (route) => route.fulfill({ contentType: "text/html", body: settings(false, 1, true) }));
  await page.addInitScript(() => {
    Object.defineProperty(window, "Notification", { configurable: true, value: { permission: "granted", requestPermission: () => { throw Error("unexpected prompt"); } } });
    Object.defineProperty(window, "PushManager", { configurable: true, value: function PushManager() {} });
    Object.defineProperty(navigator, "serviceWorker", { configurable: true, value: { register: async () => ({ pushManager: { getSubscription: async () => null } }) } });
  });
  await page.goto("/account/notifications");
  await expect(page.locator("[data-push-status]")).toHaveAttribute("data-push-state", "other-devices");
  expect(reconciles).toBe(0);
});
for (const state of [
  { name: "browser blocked", permission: "denied", enabled: true, devices: 1, current: false, expected: "blocked" },
  { name: "globally disabled", permission: "granted", enabled: false, devices: 1, current: false, expected: "disabled" },
  { name: "enabled without devices", permission: "granted", enabled: true, devices: 0, current: false, expected: "no-devices" },
  { name: "current browser registered", permission: "granted", enabled: true, devices: 1, current: true, expected: "current-device" },
]) {
  test(`${state.name} has a distinct browser state`, async ({ page }) => {
    await page.route("**/account/notifications", (route) => route.fulfill({ contentType: "text/html", body: settings(state.current, state.devices, state.enabled) }));
    await page.addInitScript(({ permission }) => {
      Object.defineProperty(window, "Notification", { configurable: true, value: { permission, requestPermission: () => { throw Error("unexpected prompt"); } } });
      Object.defineProperty(window, "PushManager", { configurable: true, value: function PushManager() {} });
      Object.defineProperty(navigator, "serviceWorker", { configurable: true, value: { register: async () => ({ pushManager: { getSubscription: async () => ({ endpoint: "https://push.example" }) } }) } });
    }, state);
    await page.goto("/account/notifications");
    await expect(page.locator("[data-push-status]")).toHaveAttribute("data-push-state", state.expected);
  });
}

test("worker registration unavailable has a distinct browser state", async ({ page }) => {
  await page.route("**/account/notifications", (route) => route.fulfill({ contentType: "text/html", body: settings(false, 0, true) }));
  await page.addInitScript(() => {
    Object.defineProperty(window, "Notification", { configurable: true, value: { permission: "default", requestPermission: () => { throw Error("unexpected prompt"); } } });
    Object.defineProperty(window, "PushManager", { configurable: true, value: function PushManager() {} });
    Object.defineProperty(navigator, "serviceWorker", { configurable: true, value: { register: async () => { throw Error("worker unavailable"); } } });
  });
  await page.goto("/account/notifications");
  await expect(page.locator("[data-push-status]")).toHaveAttribute("data-push-state", "unavailable");
  await expect(page.locator("[data-subscribe-button]")).toBeDisabled();
});
test("denied permission reconciles current device even when registration fails", async ({ page }) => {
  let reconciles = 0;
  let current = true;
  await page.route("**/account/notifications/reconcile", (route) => { reconciles++; current = false; return route.fulfill({ status: 200 }); });
  await page.route("**/account/notifications", (route) => route.fulfill({ contentType: "text/html", body: settings(current, current ? 1 : 0, true) }));
  await page.addInitScript(() => {
    Object.defineProperty(window, "Notification", { configurable: true, value: { permission: "denied", requestPermission: () => { throw Error("unexpected prompt"); } } });
    Object.defineProperty(window, "PushManager", { configurable: true, value: function PushManager() {} });
    Object.defineProperty(navigator, "serviceWorker", { configurable: true, value: { register: async () => { throw Error("worker unavailable"); } } });
  });
  await page.goto("/account/notifications");
  await expect.poll(() => reconciles).toBe(1);
});
for (const state of [
  { name: "current device removal", selector: '[data-current-device-remove]', fail: false, expectedUnsubscribes: 1 },
  { name: "other device removal", selector: 'li[data-current-device="false"] form', fail: false, expectedUnsubscribes: 0 },
  { name: "current device removal after unsubscribe failure", selector: '[data-current-device-remove]', fail: true, expectedUnsubscribes: 1 },
]) {
  test(`${state.name} preserves normal server removal`, async ({ page }) => {
    let removals = 0;
    await page.route("**/account/notifications/devices/**", async (route) => {
      removals++;
      expect(route.request().method()).toBe("POST");
      await route.fulfill({ status: 200, body: "removed" });
    });
    await page.route("**/account/notifications", (route) => route.fulfill({ contentType: "text/html", body: settings(true, 2, true) }));
    await page.addInitScript((fail) => {
      Object.defineProperty(window, "Notification", { configurable: true, value: { permission: "granted" } });
      Object.defineProperty(window, "PushManager", { configurable: true, value: function PushManager() {} });
      Object.defineProperty(navigator, "serviceWorker", { configurable: true, value: { register: async () => ({ pushManager: { getSubscription: async () => ({ unsubscribe: async () => { localStorage.setItem("unsubscribe-count", "1"); if (fail) throw Error("unsubscribe failed"); return true; } }) } }) } });
    }, state.fail);
    await page.goto("/account/notifications");
    await page.locator(state.selector).getByRole("button", { name: /Remove/ }).click();
    await expect.poll(() => removals).toBe(1);
    expect(await page.evaluate(() => localStorage.getItem("unsubscribe-count"))).toBe(state.expectedUnsubscribes ? "1" : null);
  });
}
