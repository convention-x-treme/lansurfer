import { spawn } from "node:child_process";
import { execFileSync } from "node:child_process";
import { mkdir, readFile, writeFile } from "node:fs/promises";
import { homedir } from "node:os";
import { join, resolve } from "node:path";
import { chromium } from "@playwright/test";

process.umask(0o077);

const root = resolve(import.meta.dirname, "../..");
const baseURL = process.env.BASE_URL || "http://127.0.0.1:18080";
const requestID = process.env.REQUEST || "";
const simulatedUsers = integerEnv("SIMULATED_USERS", 120, 1, 400);
const browserClients = integerEnv("BROWSER_CLIENTS", 5, 5, 10);
const duration = process.env.DURATION || "75s";
const triggerCount = integerEnv("TRIGGER", 20, 0, 100);
const triggerInterval = process.env.TRIGGER_INTERVAL || "500ms";
const triggerDelay = process.env.TRIGGER_DELAY || "0s";
const allowReconnect = process.env.ALLOW_RECONNECT === "1";
const allowMissingEvents = process.env.ALLOW_MISSING_EVENTS === "1";
const viewMode = process.env.VIEW || "mixed";
const filterGame = process.env.FILTER_GAME || "Warcraft III";
const sessionsPath = process.env.LANSURFER_LOADTEST_SESSIONS_FILE || join(homedir(), "lansurfer-loadtest-sessions.jsonl");
const reportDir = process.env.LANSURFER_LOADTEST_REPORT_DIR || join(homedir(), "lansurfer-loadtest-reports");

if (!/^http:\/\/127\.0\.0\.1:(18080|18081)$/.test(baseURL)) throw new Error("BASE_URL must target the isolated load-test stack");
if (!/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(requestID)) throw new Error("REQUEST must be an isolated Game Request UUID");
if (!/^[1-9]\d*(ms|s)$/.test(triggerInterval)) throw new Error("TRIGGER_INTERVAL must use positive milliseconds or seconds");
if (!/^(0s|[1-9]\d*(ms|s|m))$/.test(triggerDelay)) throw new Error("TRIGGER_DELAY must use non-negative milliseconds, seconds, or minutes");
if (!["discovery", "detail", "mixed"].includes(viewMode)) throw new Error("VIEW must be discovery, detail or mixed");

const sessions = (await readFile(sessionsPath, "utf8"))
  .trim()
  .split("\n")
  .map((line) => JSON.parse(line));
if (simulatedUsers + (simulatedUsers === 400 ? 0 : 5) > sessions.length) throw new Error("session file has too few seeded users");

const sessionOffset = simulatedUsers === 400 ? 0 : 5;
const browserOffset = 0;
const browsers = [];
const contexts = [];
let runner;
let runnerExitCode = null;
let runnerError = "";

try {
  const players = [];
  for (let i = 0; i < browserClients; i++) {
    const browser = await chromium.launch({ headless: true });
    browsers.push(browser);
    const view = process.env.LOBBY_BROWSER === "1" && i === 0 ? "lobby" : viewMode === "mixed" ? (i % 2 === 0 ? "discovery" : "detail") : viewMode;
    const filterQuery = new URLSearchParams({ q: filterGame }).toString();
    const refreshURL = view === "lobby" ? `/game-requests/${requestID}/lobby` : view === "detail" ? `/game-requests/${requestID}/refresh` : `/game-requests/refresh?${filterQuery}`;
    const pageURL = view === "lobby" || view === "detail" ? `/game-requests/${requestID}` : `/game-requests?${filterQuery}`;
    const context = await browser.newContext();
    contexts.push(context);
    await context.addCookies([{
      name: "lansurfer_session",
      value: (view === "lobby" ? sessions[sessions.length - 2] : sessions[browserOffset + i]).session_token,
      url: baseURL,
      httpOnly: true,
      sameSite: "Lax",
    }]);
    await context.addInitScript(() => {
      window.__loadtest = { events: 0, eventTypes: {}, streamOpen: false, opens: 0, eventTimeline: [], swapTimeline: [], errors: 0 };
      const NativeEventSource = window.EventSource;
      window.EventSource = class extends NativeEventSource {
        constructor(...args) {
          super(...args);
          this.addEventListener("open", () => { window.__loadtest.streamOpen = true; window.__loadtest.opens++; });
          this.addEventListener("error", () => { window.__loadtest.sseErrors = (window.__loadtest.sseErrors || 0) + 1; });
          for (const type of ["game-request", "game-request-chat", "game-request-activity"]) this.addEventListener(type, () => { window.__loadtest.events++; window.__loadtest.eventTypes[type] = (window.__loadtest.eventTypes[type] || 0) + 1; window.__loadtest.eventTimeline.push({ type, at: performance.now() }); });
        }
      };
      window.addEventListener("htmx:beforeRequest", () => { window.__loadtest.htmxBefore = (window.__loadtest.htmxBefore || 0) + 1; });
      window.addEventListener("htmx:afterRequest", () => { window.__loadtest.htmxAfter = (window.__loadtest.htmxAfter || 0) + 1; });
      window.addEventListener("htmx:afterSwap", () => { window.__loadtest.htmxSwap = (window.__loadtest.htmxSwap || 0) + 1; window.__loadtest.swapTimeline.push(performance.now()); });
      window.addEventListener("error", () => { window.__loadtest.errors++; });
      window.addEventListener("unhandledrejection", () => { window.__loadtest.errors++; });
    });
    const page = await context.newPage();
    let abortedRefresh = false;
    await page.route("**/game-requests/**/refresh*", async (route) => {
      if (process.env.ABORT_REFRESH === "1" && !abortedRefresh && isRefresh(route.request())) {
        abortedRefresh = true;
        await route.abort();
      } else {
        await route.continue();
      }
    });
    const stats = { started: 0, completed: 0, failed: 0, active: 0, peakConcurrent: 0, outOfOrder: 0, lastCompletedSequence: 0, durationsMs: [], statuses: [] };
    const pending = new Map();
    const responseStatuses = new Map();
    page.on("request", (request) => {
      if (isRefresh(request)) {
        const record = { sequence: ++stats.started, startedAt: performance.now() };
        pending.set(request, record);
        stats.active++;
        stats.peakConcurrent = Math.max(stats.peakConcurrent, stats.active);
      }
    });
    page.on("response", (response) => {
      const request = response.request();
      if (pending.has(request)) responseStatuses.set(request, response.status());
    });
    page.on("requestfinished", (request) => {
      const record = pending.get(request);
      if (!record) return;
      finishRefresh(request, record, responseStatuses.get(request) ?? 0);
    });
    page.on("requestfailed", (request) => {
      const record = pending.get(request);
      if (!record) return;
      stats.failed++;
      finishRefresh(request, record, 0);
    });
    await page.goto(`${baseURL}${pageURL}`, { waitUntil: "domcontentloaded" });
    await page.waitForSelector(view === "lobby" ? "#game-request-lobby li, #game-request-lobby" : view === "detail" ? "[data-game-request-detail]" : "[data-game-request-list]");
    await page.waitForFunction(() => window.__loadtest.streamOpen, { timeout: 20_000 });
    const initialState = await page.evaluate((currentView) => currentView === "lobby" ? { messages: document.querySelectorAll("#game-request-lobby li").length } : currentView === "detail" ? { count: document.querySelector(".detail-shell .place-counter")?.textContent?.trim() || "" } : { rows: document.querySelectorAll("#game-request-list [data-game-request-id]").length }, view);
    players.push({ page, stats, view, initialState, refreshURL });

    function isRefresh(request) {
      const url = new URL(request.url());
      return request.method() === "GET" && url.pathname === new URL(refreshURL, baseURL).pathname && url.searchParams.get("__loadtest_diagnostic") !== "1";
    }
    function finishRefresh(request, record, status) {
      pending.delete(request);
      responseStatuses.delete(request);
      stats.active--;
      stats.completed++;
      stats.statuses.push(status);
      stats.durationsMs.push(performance.now() - record.startedAt);
      if (record.sequence < stats.lastCompletedSequence) stats.outOfOrder++;
      stats.lastCompletedSequence = Math.max(stats.lastCompletedSequence, record.sequence);
    }
  }

  let stdout = "";
  if (process.env.BROWSER_ONLY === "1") {
    // Soak identity mode: hold real browser pages for the whole run while the soak owns SSE/write load.
    const unit = { s: 1000, m: 60_000, h: 3_600_000 }[duration.at(-1)];
    console.error("loadtest-ready");
    await new Promise((resolveWait) => setTimeout(resolveWait, Number(duration.slice(0, -1)) * unit));
    runnerExitCode = 0;
  } else {
    runner = spawn("go", [
      "run", "./tools/loadtest/sse",
      "-clients", String(simulatedUsers),
      "-duration", duration,
      "-request", requestID,
      "-trigger-mode", process.env.TRIGGER_MODE || "join-leave",
      "-interval",
      triggerInterval,
      "-trigger", String(triggerCount),
      "-trigger-delay", triggerDelay,
      ...(allowReconnect ? ["-allow-reconnect"] : []),
      ...(allowMissingEvents ? ["-allow-missing-events"] : []),
      "-refresh-mode", process.env.REFRESH_MODE || "off",
      "-refresh-query", new URLSearchParams({ q: filterGame }).toString(),
      "-connect-mode", process.env.CONNECT_MODE || "staggered",
      "-base-url", baseURL,
      "-session-offset", String(sessionOffset),
      "-sessions", sessionsPath,
    ], { cwd: root, stdio: ["ignore", "pipe", "pipe"] });
    console.error("loadtest-ready");
    const runnerStdout = collect(runner.stdout);
    const runnerStderr = collect(runner.stderr);
    runner.once("error", (error) => { runnerError = error.message; });
    const runnerExit = new Promise((resolveExit) => runner.once("close", (code) => resolveExit(code)));
    runnerExitCode = await runnerExit;
    const [capturedStdout, stderr] = await Promise.all([runnerStdout, runnerStderr]);
    stdout = capturedStdout;
    runner = undefined;
    if (runnerExitCode !== 0) runnerError = stderr.trim() || stdout.trim() || "SSE runner stopped before completion";
  }

  if (!allowMissingEvents && runnerExitCode === 0) {
    await Promise.all(players.map(({ page }) => page.waitForFunction(
      (count) => window.__loadtest.events >= count,
      triggerCount,
      { timeout: 30_000 },
    )));
  }
  let idleError = "";
  try {
    await waitForIdle(players.map((player) => player.stats), 30_000);
  } catch (error) {
    idleError = error.message;
  }
  const allLatenciesMs = [];

  const results = await Promise.all(players.map(async ({ page, stats, view, initialState, refreshURL }, index) => {
    const measured = {
      started: stats.started,
      completed: stats.completed,
      failed: stats.failed,
      peakConcurrent: stats.peakConcurrent,
      outOfOrder: stats.outOfOrder,
      durationsMs: stats.durationsMs.slice(),
      statuses: stats.statuses.slice(),
    };
    allLatenciesMs.push(...measured.durationsMs);
    const state = await page.evaluate(async ({ url, currentView }) => {
      let response;
      let html = "";
      let fetchError = "";
      for (let attempt = 0; attempt < 3; attempt++) {
        try {
          response = await fetch(`${url}${url.includes("?") ? "&" : "?"}__loadtest_diagnostic=1`, { headers: { "HX-Request": "true" } });
          html = await response.text();
          fetchError = "";
          break;
        } catch (error) {
          fetchError = String(error);
          await new Promise((resolveWait) => setTimeout(resolveWait, 1000));
        }
      }
      if (!response) {
        return { status: 0, fetchError, stateMatches: false, mismatchedEntries: 1, visibleState: {}, authoritativeState: {}, events: window.__loadtest.events, eventTypes: window.__loadtest.eventTypes, eventTimeline: window.__loadtest.eventTimeline, opens: window.__loadtest.opens, sseErrors: window.__loadtest.sseErrors || 0, htmx: { before: window.__loadtest.htmxBefore || 0, after: window.__loadtest.htmxAfter || 0, swaps: window.__loadtest.htmxSwap || 0, errors: window.__loadtest.errors || 0 }, swapTimeline: window.__loadtest.swapTimeline, busy: 0, disabledActions: 0 };
      }
      const fragmentDocument = new DOMParser().parseFromString(html, "text/html");
      const viewState = (root) => currentView === "lobby" ? root.querySelectorAll("#game-request-lobby li").length : currentView === "detail" ? root.querySelector(".detail-shell .place-counter")?.textContent?.trim() || "" : [...root.querySelectorAll("#game-request-list [data-game-request-id]")].map((row) => [row.dataset.gameRequestId, row.querySelector(".request-places .place-counter")?.textContent?.trim() || ""]);
      const visibleState = viewState(window.document);
      const authoritativeState = viewState(fragmentDocument);
      return {
        status: response.status,
        stateMatches: JSON.stringify(visibleState) === JSON.stringify(authoritativeState),
        mismatchedEntries: currentView === "detail" || currentView === "lobby" ? Number(visibleState !== authoritativeState) : Math.max(visibleState.length, authoritativeState.length) - visibleState.filter((entry, index) => JSON.stringify(entry) === JSON.stringify(authoritativeState[index])).length,
        visibleState: currentView === "lobby" ? { messages: visibleState } : currentView === "detail" ? { count: visibleState } : { rows: visibleState.length },
        authoritativeState: currentView === "lobby" ? { messages: authoritativeState } : currentView === "detail" ? { count: authoritativeState } : { rows: authoritativeState.length },
        fetchError,
        events: window.__loadtest.events,
        eventTypes: window.__loadtest.eventTypes,
        eventTimeline: window.__loadtest.eventTimeline,
        opens: window.__loadtest.opens,
        sseErrors: window.__loadtest.sseErrors || 0,
        htmx: { before: window.__loadtest.htmxBefore || 0, after: window.__loadtest.htmxAfter || 0, swaps: window.__loadtest.htmxSwap || 0, errors: window.__loadtest.errors || 0 },
        swapTimeline: window.__loadtest.swapTimeline,
        busy: document.querySelectorAll("[aria-busy='true'], .htmx-request").length,
        disabledActions: document.querySelectorAll("form button:disabled, form input:disabled").length,
      };
    }, { url: refreshURL, currentView: view });
    const visibilityRecovery = process.env.VISIBILITY_RECOVERY === "1" ? await recoverVisibility(page, refreshURL, view) : null;
    return {
      view,
      events: state.events,
      eventTypes: state.eventTypes,
      eventTimeline: state.eventTimeline,
      opens: state.opens,
      sseErrors: state.sseErrors,
      htmx: state.htmx,
      swapTimeline: state.swapTimeline,
      busy: state.busy,
      disabledActions: state.disabledActions,
      visibilityRecovery,
      mismatchedEntries: state.mismatchedEntries,
      refreshes: measured.started,
      completed: measured.completed,
      failed: measured.failed,
      peakConcurrent: measured.peakConcurrent,
      outOfOrderCompletions: measured.outOfOrder,
      refreshLatencyMs: { p50: percentile(measured.durationsMs, 50), p95: percentile(measured.durationsMs, 95), p99: percentile(measured.durationsMs, 99), max: percentile(measured.durationsMs, 100) },
      statuses: statusCounts(measured.statuses),
      initialState,
      visibleState: state.visibleState,
      authoritativeState: state.authoritativeState,
      converged: state.status === 200 && state.stateMatches,
    };
  }));
  const totalEvents = results.reduce((sum, item) => sum + item.events, 0);
  const totalRefreshes = results.reduce((sum, item) => sum + item.refreshes, 0);

  const summary = {
    schemaVersion: 1,
    gitRevision: execFileSync("git", ["rev-parse", "HEAD"], { cwd: root, encoding: "utf8" }).trim(),
    baseURL,
    requestID,
    simulatedUsers,
    browserClients,
    duration,
    triggerCount,
    triggerInterval,
    triggerDelay,
    allowReconnect,
    allowMissingEvents,
    viewMode,
    filterGame: viewMode === "detail" ? null : filterGame,
    refreshLatencyMs: { p50: percentile(allLatenciesMs, 50), p95: percentile(allLatenciesMs, 95), p99: percentile(allLatenciesMs, 99), max: percentile(allLatenciesMs, 100) },
    runner: stdout.trim(),
    runnerExitCode,
    runnerError,
    idleError,
    totals: {
      events: totalEvents,
      opens: results.reduce((sum, item) => sum + item.opens, 0),
      mismatchedEntries: results.reduce((sum, item) => sum + item.mismatchedEntries, 0),
      refreshes: totalRefreshes,
      refreshesPerEvent: totalEvents === 0 ? null : Number((totalRefreshes / totalEvents).toFixed(2)),
      failed: results.reduce((sum, item) => sum + item.failed, 0),
      peakConcurrent: Math.max(...results.map((item) => item.peakConcurrent)),
      outOfOrderCompletions: results.reduce((sum, item) => sum + item.outOfOrderCompletions, 0),
      convergedBrowsers: results.filter((item) => item.converged).length,
    },
    browsers: results,
  };
  await mkdir(reportDir, { recursive: true, mode: 0o700 });
  const reportPath = join(reportDir, `browser-calibration-${new Date().toISOString().replaceAll(/[-:]/g, "").replace(/\.\d+Z$/, "Z")}.json`);
  await writeFile(reportPath, `${JSON.stringify(summary, null, 2)}\n`, { mode: 0o600, flag: "wx" });
  console.log(JSON.stringify({ ...summary, reportPath }, null, 2));
  if (runnerExitCode !== 0 || summary.totals.failed > 0 || results.some((item) => Object.keys(item.statuses).some((status) => Number(status) !== 200)) || summary.totals.convergedBrowsers !== browserClients) process.exitCode = 1;
} finally {
  if (runner) runner.kill("SIGTERM");
  await Promise.all(contexts.map((context) => context.close()));
  await Promise.all(browsers.map((browser) => browser.close()));
}

function integerEnv(name, fallback, min, max) {
  const value = Number(process.env[name] || fallback);
  if (!Number.isInteger(value) || value < min || value > max) throw new Error(`${name} must be an integer in ${min}..${max}`);
  return value;
}

function percentile(values, quantile) {
  if (values.length === 0) return null;
  const sorted = [...values].sort((a, b) => a - b);
  return Math.round(sorted[Math.ceil(quantile / 100 * sorted.length) - 1] * 100) / 100;
}

function statusCounts(statuses) {
  return statuses.reduce((counts, status) => {
    counts[status] = (counts[status] || 0) + 1;
    return counts;
  }, {});
}

async function collect(stream) {
  let value = "";
  for await (const chunk of stream) value += chunk;
  return value;
}

async function waitForIdle(statsList, timeoutMs) {
  const deadline = Date.now() + timeoutMs;
  while (statsList.some((stats) => stats.active > 0)) {
    if (Date.now() >= deadline) throw new Error("browser refresh requests did not settle");
    await new Promise((resolveWait) => setTimeout(resolveWait, 50));
  }
}

async function recoverVisibility(page, refreshURL, view) {
  const setVisibility = (hidden) => page.evaluate((nextHidden) => {
    Object.defineProperty(document, "hidden", { configurable: true, get: () => nextHidden });
    Object.defineProperty(document, "visibilityState", { configurable: true, get: () => nextHidden ? "hidden" : "visible" });
    document.dispatchEvent(new Event("visibilitychange"));
  }, hidden);
  await setVisibility(true);
  await setVisibility(false);
  await page.waitForTimeout(2000);
  return page.evaluate(async ({ url, currentView }) => {
    const response = await fetch(`${url}${url.includes("?") ? "&" : "?"}__loadtest_diagnostic=1`, { headers: { "HX-Request": "true" } });
    const fragment = new DOMParser().parseFromString(await response.text(), "text/html");
    const read = (root) => currentView === "lobby" ? root.querySelectorAll("#game-request-lobby li").length : currentView === "detail" ? root.querySelector(".detail-shell .place-counter")?.textContent?.trim() || "" : [...root.querySelectorAll("#game-request-list [data-game-request-id]")].map((row) => [row.dataset.gameRequestId, row.querySelector(".request-places .place-counter")?.textContent?.trim() || ""]);
    const visible = read(document);
    return { converged: response.status === 200 && JSON.stringify(visible) === JSON.stringify(read(fragment)), visibleState: visible, busy: document.querySelectorAll("[aria-busy='true'], .htmx-request").length };
  }, { url: refreshURL, currentView: view });
}
