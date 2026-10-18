import { expect, test, type Page } from "@playwright/test";
import { fixtureState, resetFixture, waitForRealtime } from "./helpers";

// Issue #147: a stream whose server-side buffer overflows must not stay open
// and silently stale. The server ends that one subscription; the browser's own
// EventSource reconnect then revalidates the page. Nothing here dispatches
// synthetic events or replaces the SSE response.

type OverflowProbe = { instances: number; states: string[]; gameRequestEvents: number; stream?: EventSource };
type OverflowProbeWindow = Window & { __overflowProbe?: OverflowProbe };

async function probe(page: Page): Promise<{ instances: number; states: string[]; gameRequestEvents: number; readyState?: number; open: number }> {
  return await page.evaluate(() => {
    const state = (window as OverflowProbeWindow).__overflowProbe!;
    return {
      instances: state.instances,
      states: [...state.states],
      gameRequestEvents: state.gameRequestEvents,
      readyState: state.stream?.readyState,
      open: EventSource.OPEN,
    };
  });
}

test.beforeEach(async ({ request }) => {
  await resetFixture(request);
});

test("overflow reconnects and revalidates detail without another domain event", async ({ page, request }) => {
  await page.addInitScript(() => {
    const state: OverflowProbe = { instances: 0, states: [], gameRequestEvents: 0 };
    (window as OverflowProbeWindow).__overflowProbe = state;
    const NativeEventSource = window.EventSource;
    window.EventSource = class extends NativeEventSource {
      constructor(url: string | URL, eventSourceInitDict?: EventSourceInit) {
        super(url, eventSourceInitDict);
        state.instances += 1;
        state.stream = this;
        this.addEventListener("open", () => state.states.push("open"));
        this.addEventListener("error", () => state.states.push("error"));
        this.addEventListener("game-request", () => (state.gameRequestEvents += 1));
      }
    };
  });
  const requestID = "00000000-0000-0000-0000-000000000003";

  await page.goto(`/game-requests/${requestID}`);
  await waitForRealtime(page, request, 1);
  await expect.poll(async () => (await probe(page)).states).toEqual(["open"]);
  expect((await probe(page)).instances).toBe(1);
  await expect(page.locator("#game-request-detail")).toContainText("3/4");
  await expect(page.locator("#session-information")).toContainText("Join this Game Request to see session information.");

  const change = await request.get("/__test__/lifecycle?status=open&starts_at=2026-10-20T18:00:00Z&joined=1");
  expect(change.status()).toBe(204);
  await expect(page.locator("#game-request-detail")).toContainText("3/4");

  const overflow = await request.post(`/__test__/sse-overflow?request=${requestID}`);
  expect(overflow.status()).toBe(204);

  await expect.poll(async () => (await probe(page)).states, { timeout: 10_000 }).toEqual(["open", "error", "open"]);
  await expect(page.locator("#game-request-detail")).toContainText("4/4");
  await expect(page.locator("#session-information")).toContainText("No session information yet.");
  await expect(page.locator("#game-request-lobby")).toContainText("Host created the request");
  expect((await probe(page)).gameRequestEvents).toBe(0);

  // Bounded negative check: a reconnect loop would add states or instances.
  await page.waitForTimeout(6_500);
  const settled = await probe(page);
  expect(settled.states).toEqual(["open", "error", "open"]);
  expect(settled.instances).toBe(1);
  expect(settled.readyState).toBe(settled.open);
  expect(settled.gameRequestEvents).toBe(0);
  expect((await fixtureState(request)).streams).toBe(1);
});
