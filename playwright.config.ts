import { defineConfig, devices } from "@playwright/test";

// Browser tests verify the Host edit flow, playing participation, and client
// behavior that Go tests cannot observe: rapid clicks, stream lifecycle,
// recovery, and SSE-versus-swap handling. Domain and persistence rules remain
// covered by Go unit and PostgreSQL integration tests.
//
// The fixture server runs LANsurfer's real router, templates, assets and SSE
// handler against in-memory stores, so no database is required.
const port = 8123;

export default defineConfig({
  testDir: "web/tests",
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
    // Any session cookie resolves to the fixture's player. The product's own
    // authentication and CSRF middleware still run.
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
  // The full Chromium build rather than the reduced headless shell: these
  // tests depend on real browser connection-pool behavior, which is the thing
  // the original hang in issue #15 was caused by.
  projects: [
    { name: "chromium", use: { ...devices["Desktop Chrome"], channel: "chromium" } },
    {
      name: "webkit",
      grep: /detail layout spans hero and contains Player rows/,
      use: { ...devices["Desktop Safari"] },
    },
  ],
  webServer: {
    command: `go run ./internal/web/browsertest -address 127.0.0.1:${port}`,
    url: `http://127.0.0.1:${port}/__test__/state`,
    // Never reuse a running fixture. The Go binary embeds the generated CSS and
    // JS at compile time, so a server left over from an earlier run would serve
    // the previous bundle — and every assertion about web/src/app.ts would pass
    // against code that is no longer there.
    reuseExistingServer: false,
    timeout: 120_000,
    stdout: "pipe",
    stderr: "pipe",
  },
});
