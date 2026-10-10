import { defineConfig } from "@playwright/test";

export default defineConfig({
  testDir: "./browser",
  workers: 1,
  fullyParallel: false,
  reporter: "list",
  use: {
    headless: true,
    trace: "off",
    video: "off",
    screenshot: "off",
    storageState: undefined,
  },
});
