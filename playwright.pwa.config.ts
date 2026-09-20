import { defineConfig, devices } from "@playwright/test";

// Track D's own focused PWA browser suite: service-worker registration,
// lifecycle, scope, controller behavior, failure degradation, and the bfcache
// authenticated-reload guard. Deliberately separate from playwright.config.ts
// (task test:browser) rather than folded into it, and on its own port so both
// suites can run independently — task test:browser stays interaction-hardening
// only, per AGENTS.md. The spec lives in web/tests-pwa/, not web/tests/,
// specifically so playwright.config.ts's default testDir never picks it up.
const port = 8124;

export default defineConfig({
  testDir: "web/tests-pwa",
  fullyParallel: false,
  workers: 1,
  forbidOnly: !!process.env.CI,
  retries: 0,
  reporter: process.env.CI ? "list" : [["list"]],
  timeout: 30_000,
  expect: { timeout: 10_000 },
  use: {
    baseURL: `http://127.0.0.1:${port}`,
    trace: "retain-on-failure",
    storageState: {
      cookies: [
        {
          name: "lansurfer_session",
          value: "browser-test-session",
          domain: "127.0.0.1",
          path: "/",
          expires: -1,
          httpOnly: true,
          secure: false,
          sameSite: "Lax",
        },
      ],
      origins: [],
    },
  },
  projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"], channel: "chromium" } }],
  webServer: {
    command: `go run ./internal/web/browsertest -address 127.0.0.1:${port}`,
    url: `http://127.0.0.1:${port}/__test__/state`,
    reuseExistingServer: false,
    timeout: 120_000,
    stdout: "pipe",
    stderr: "pipe",
  },
});
