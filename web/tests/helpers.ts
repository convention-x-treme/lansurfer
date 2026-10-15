// Helpers for LANsurfer's browser regression tests.
//
// Which of these tests are proven regression guards was established by removing
// each behavior and re-running the suite. Four tests fail when their behavior is
// deleted, and those are the ones whose failure means something:
//
//   - "a hidden page releases its stream and reclaims it when visible again"
//   - "six open pages do not exhaust the connection pool"
//       both fail without the visibilitychange/pagehide handlers in app.ts,
//       which are the fix for the hang in issue #15
//   - "a live update keeps the active discovery filter"
//       fails without data-filter-query being carried into the refresh
//   - "a server error surfaces a dismissible notice instead of freezing"
//       fails without the htmx:responseError handler
//
// The rest are backstops. They document invariants that currently hold for a
// reason below the application — Chromium refuses to submit a form twice while
// one submission is in flight, htmx queues requests per element, and the
// browser closes a stream's socket when its page goes away. They are cheap and
// they would catch a change that removes those protections, but a green run of
// them is not evidence that the application's own guards work.

import { expect, type APIRequestContext, type Page } from "@playwright/test";

export type FixtureState = { creates: number; joins: number; streams: number; requests: number };

export async function fixtureState(request: APIRequestContext): Promise<FixtureState> {
  const response = await request.get("/__test__/state");
  return (await response.json()) as FixtureState;
}

export async function resetFixture(request: APIRequestContext): Promise<void> {
  await request.post("/__test__/reset");
}

export async function expectStreams(request: APIRequestContext, count: number): Promise<void> {
  await expect
    .poll(async () => (await fixtureState(request)).streams, { timeout: 10_000 })
    .toBe(count);
}

// waitForRealtime resolves once the page has actually opened its stream, so a
// test never measures the moment before the browser connected.
export async function waitForRealtime(page: Page, request: APIRequestContext, expected: number): Promise<void> {
  await page.waitForSelector("[data-realtime-url]", { state: "attached" });
  await expectStreams(request, expected);
}

// Fire EventSource lifecycle callbacks to test recovery without a network fault.
export async function simulateRealtimeReconnect(page: Page): Promise<void> {
  await page.evaluate(() => {
    const browserWindow = window as Window & { __eventSource?: EventSource };
    const stream = browserWindow.__eventSource;
    if (!stream) throw new Error("EventSource was not recorded");
    stream.dispatchEvent(new Event("error"));
    stream.dispatchEvent(new Event("open"));
  });
}

// countHtmxRequests instruments htmx's own lifecycle events. An unbalanced
// count is the failure that matters: app.ts defers live refreshes while a
// mutation is in flight, so a request that starts and never finishes freezes
// every later refresh.
export async function instrumentHtmx(page: Page): Promise<void> {
  await page.evaluate(() => {
    const counters = { before: 0, after: 0 };
    (window as unknown as { __htmx: typeof counters }).__htmx = counters;
    document.addEventListener("htmx:beforeRequest", () => (counters.before += 1));
    document.addEventListener("htmx:afterRequest", () => (counters.after += 1));
  });
}

export async function htmxCounters(page: Page): Promise<{ before: number; after: number }> {
  return await page.evaluate(() => (window as unknown as { __htmx: { before: number; after: number } }).__htmx);
}
