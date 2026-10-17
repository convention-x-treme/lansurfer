import { spawn } from "node:child_process";
import { execFileSync } from "node:child_process";
import { mkdir, readFile, writeFile } from "node:fs/promises";
import { homedir } from "node:os";
import { join, resolve } from "node:path";
import { chromium } from "@playwright/test";
import { calibrationFailures, waitForIdle } from "./outcomes.mjs";

process.umask(0o077);

const root = resolve(import.meta.dirname, "../..");
const baseURL = process.env.BASE_URL || "http://127.0.0.1:18080";
const requestID = process.env.REQUEST || "";
const simulatedUsers = integerEnv("SIMULATED_USERS", 120, 1, 400);
const browserClients = integerEnv("BROWSER_CLIENTS", 5, 5, 10);
const duration = process.env.DURATION || "75s";
const triggerCount = integerEnv("TRIGGER", 20, 0, 100);
const triggerInterval = process.env.TRIGGER_INTERVAL || "500ms";
const allowReconnect = process.env.ALLOW_RECONNECT === "1";
const allowMissingEvents = process.env.ALLOW_MISSING_EVENTS === "1";
const viewMode = process.env.VIEW || "mixed";
const filterGame = process.env.FILTER_GAME || "Warcraft III";
const sessionsPath = process.env.LANSURFER_LOADTEST_SESSIONS_FILE || join(homedir(), "lansurfer-loadtest-sessions.jsonl");
const reportDir = process.env.LANSURFER_LOADTEST_REPORT_DIR || join(homedir(), "lansurfer-loadtest-reports");
const coalescing = process.env.COALESCING || "0";
const probe = process.env.PROBE || "0";
const refreshMode = process.env.REFRESH_MODE || (coalescing === "1" ? "coalesced" : "off");
const triggerDelay = process.env.TRIGGER_DELAY || (coalescing === "1" ? "10s" : "0s");
const simulatedSessionOffset = integerEnv("SIMULATED_SESSION_OFFSET", simulatedUsers === 400 ? 0 : 5, 0, 507);
const writerUsers = integerEnv("WRITERS", 0, 0, 100);
const writerSessionOffset = integerEnv("WRITER_SESSION_OFFSET", simulatedSessionOffset + simulatedUsers, 0, 507);
const writerDuration = process.env.WRITER_DURATION || "90s";
const profileDuration = process.env.PROFILE_DURATION || "90s";
if (!["0", "1"].includes(coalescing) || !["0", "1"].includes(probe)) throw new Error("COALESCING and PROBE must be 0 or 1");

if (!/^http:\/\/127\.0\.0\.1:(18080|18081)$/.test(baseURL)) throw new Error("BASE_URL must target the isolated load-test stack");
if (!/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(requestID)) throw new Error("REQUEST must be an isolated Game Request UUID");
if (!/^[1-9]\d*(ms|s)$/.test(triggerInterval)) throw new Error("TRIGGER_INTERVAL must use positive milliseconds or seconds");
if (!/^(0s|[1-9]\d*(ms|s|m))$/.test(triggerDelay)) throw new Error("TRIGGER_DELAY must use non-negative milliseconds, seconds, or minutes");
if (coalescing === "1" && triggerCount > 0 && durationMilliseconds(triggerDelay) < 10_000) {
  throw new Error("TRIGGER_DELAY must be at least 10s in coalescing mode so writers and profiler start before mutations");
}
if (coalescing === "1" && triggerCount === 0) throw new Error("TRIGGER must be positive in coalescing mode");
if (probe === "1" && triggerCount !== 1) throw new Error("PROBE=1 requires TRIGGER=1");
if (!["discovery", "detail", "mixed"].includes(viewMode)) throw new Error("VIEW must be discovery, detail or mixed");
const browserOffset = 0;

const sessions = (await readFile(sessionsPath, "utf8"))
  .trim()
  .split("\n")
  .map((line) => JSON.parse(line));
if (simulatedSessionOffset + simulatedUsers > sessions.length) throw new Error("session file has too few sessions for simulated streams");
if (writerUsers > 0 && writerSessionOffset + writerUsers > sessions.length) throw new Error("session file has too few sessions for writers");
const rangesOverlap = (startA, endA, startB, endB) => startA < endB && startB < endA;
const reservedStart = sessions.length - 2;
const sharesBrowserSessions = simulatedUsers === 400 && simulatedSessionOffset === 0;
if (!sharesBrowserSessions && rangesOverlap(simulatedSessionOffset, simulatedSessionOffset + simulatedUsers, browserOffset, browserOffset + browserClients)) {
  throw new Error("simulated stream sessions overlap browser sessions");
}
if (coalescing === "1" && simulatedSessionOffset + simulatedUsers > reservedStart) throw new Error("simulated streams overlap reserved control sessions");
if (writerUsers > 0 && (
  rangesOverlap(writerSessionOffset, writerSessionOffset + writerUsers, browserOffset, browserOffset + browserClients) ||
  rangesOverlap(writerSessionOffset, writerSessionOffset + writerUsers, simulatedSessionOffset, simulatedSessionOffset + simulatedUsers) ||
  writerSessionOffset + writerUsers > reservedStart
)) throw new Error("writer sessions overlap browsers, simulated streams, or reserved control sessions");

const sessionOffset = simulatedSessionOffset;
const browsers = [];
const contexts = [];
let writerRunner;
let profileRunner;
let writerExit;
let profileExit;
let writerOutput;
let writerErrorOutput;
let profileOutput;
let profileErrorOutput;
let writerExitCode = null;
let profileExitCode = null;
let writerTimeouts = null;
let writerHTTP502s = null;
let writerReadyAtMs = 0;
let writerReadyAt = "";
let profileStartedAt = "";
let profilePath = "";
let profileInvalidWindows = null;
let profileSamples = null;
let profileSampleErrors = null;
let sseReadyAt = "";
let triggerDeadlineMs = 0;
let triggerStartMs = 0;
let runner;
let stdout = "";
let runnerExitCode = null;
let runnerError = "";
let signalExitCode = 0;
const handleSignal = (exitCode) => {
  signalExitCode = exitCode;
  for (const child of [runner, writerRunner, profileRunner]) if (child) stopProcessGroup(child);
  for (const context of contexts) void context.close().catch(() => {});
  for (const browser of browsers) void browser.close().catch(() => {});
  process.exitCode = exitCode;
};
process.once("SIGINT", () => handleSignal(130));
process.once("SIGTERM", () => handleSignal(143));

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
    await context.addInitScript(({ view, requestID, probeEnabled }) => {
      window.__loadtest = { events: 0, relevantGameRequestEvents: 0, eventTypes: {}, streamOpen: false, opens: 0, eventTimeline: [], swapTimeline: [], errors: 0, probeSamples: [] };
      const NativeEventSource = window.EventSource;
      window.EventSource = class extends NativeEventSource {
        constructor(...args) {
          super(...args);
          this.addEventListener("open", () => { window.__loadtest.streamOpen = true; window.__loadtest.opens++; });
          this.addEventListener("error", () => { window.__loadtest.sseErrors = (window.__loadtest.sseErrors || 0) + 1; });
          for (const type of ["game-request", "game-request-chat", "game-request-activity"]) this.addEventListener(type, (event) => {
            window.__loadtest.events++;
            window.__loadtest.eventTypes[type] = (window.__loadtest.eventTypes[type] || 0) + 1;
            let payload = null;
            try { payload = JSON.parse(event.data); } catch {}
            if (type === "game-request" && (view !== "detail" || payload?.requestId === requestID)) {
              window.__loadtest.relevantGameRequestEvents++;
            }
            if (probeEnabled && type === "game-request" && payload?.requestId === requestID && payload?.type === "GameRequestJoined") {
              const startedAt = performance.now();
              const observer = new MutationObserver(() => {
                const selector = view === "detail" ? ".detail-shell .place-counter" : `[data-game-request-id="${CSS.escape(requestID)}"] .request-places .place-counter`;
                const counter = document.querySelector(selector)?.textContent?.trim();
                if (counter === "2/8") {
                  window.__loadtest.probeSamples.push(performance.now() - startedAt);
                  observer.disconnect();
                }
              });
              observer.observe(document.body, { childList: true, subtree: true, characterData: true });
            }
            window.__loadtest.eventTimeline.push({ type, requestId: payload?.requestId || "", eventType: payload?.type || "", at: performance.now() });
            window.__loadtestActivity?.();
          });
        }
      };
      window.addEventListener("htmx:beforeRequest", () => { window.__loadtest.htmxBefore = (window.__loadtest.htmxBefore || 0) + 1; });
      window.addEventListener("htmx:afterRequest", () => { window.__loadtest.htmxAfter = (window.__loadtest.htmxAfter || 0) + 1; });
      window.addEventListener("htmx:afterSwap", () => { window.__loadtest.htmxSwap = (window.__loadtest.htmxSwap || 0) + 1; window.__loadtest.swapTimeline.push(performance.now()); });
      window.addEventListener("error", () => { window.__loadtest.errors++; });
      window.addEventListener("unhandledrejection", () => { window.__loadtest.errors++; });
    }, { view, requestID, probeEnabled: probe === "1" });
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
    const stats = { started: 0, completed: 0, failed: 0, active: 0, peakConcurrent: 0, outOfOrder: 0, lastCompletedSequence: 0, lastActivityAt: Date.now(), durationsMs: [], statuses: [], requestTimeline: [] };
    await page.exposeBinding("__loadtestActivity", () => { stats.lastActivityAt = Date.now(); });
    const pending = new Map();
    const responseStatuses = new Map();
    page.on("request", (request) => {
      if (isRefresh(request)) {
        stats.lastActivityAt = Date.now();
        const record = { sequence: ++stats.started, startedAt: performance.now() };
        pending.set(request, record);
        stats.active++;
        stats.requestTimeline.push({ type: "start", sequence: record.sequence, at: record.startedAt, active: stats.active });
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
      stats.lastActivityAt = Date.now();
      stats.active--;
      stats.completed++;
      stats.statuses.push(status);
      const durationMs = performance.now() - record.startedAt;
      stats.durationsMs.push(durationMs);
      stats.requestTimeline.push({ type: "finish", sequence: record.sequence, at: performance.now(), active: stats.active, status, durationMs });
      if (record.sequence < stats.lastCompletedSequence) stats.outOfOrder++;
      stats.lastCompletedSequence = Math.max(stats.lastCompletedSequence, record.sequence);
    }
  }

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
      ...(coalescing === "1" ? ["-game-request-refresh-only"] : []),
      "-refresh-mode", refreshMode,
      "-refresh-query", new URLSearchParams({ q: filterGame }).toString(),
      "-connect-mode", process.env.CONNECT_MODE || "staggered",
      "-base-url", baseURL,
      "-session-offset", String(sessionOffset),
      "-sessions", sessionsPath,
    ], { cwd: root, stdio: ["ignore", "pipe", "pipe"], detached: true });
    console.error("loadtest-ready");
    const runnerStdout = collect(runner.stdout);
    let stderr = "";
    let readyResolve;
    const ready = new Promise((resolveReady) => { readyResolve = resolveReady; });
    runner.stderr.on("data", (chunk) => {
      stderr += chunk;
      if (/sse-ready clients=\d+/.test(stderr) && !sseReadyAt) sseReadyAt = new Date().toISOString();
      if (/trigger-start clients=\d+ delay=\S+/.test(stderr) && !triggerStartMs) {
        triggerStartMs = Date.now();
        readyResolve(triggerStartMs);
      }
    });
    const runnerExit = new Promise((resolveExit) => runner.once("close", (code) => resolveExit(code)));
    runner.once("error", (error) => { runnerError = error.message; });
    if (coalescing === "1") {
      let triggerTimeout;
      const timeout = new Promise((resolveTimeout) => { triggerTimeout = setTimeout(() => resolveTimeout(new Error("SSE trigger did not become ready within 20 seconds")), 20_000); });
      const earlyExit = runnerExit.then((code) => new Error(`SSE runner exited before trigger-start (${code}): ${stderr.trim() || "no stderr output"}`));
      const readyResult = await Promise.race([ready, timeout, earlyExit]);
      clearTimeout(triggerTimeout);
      if (readyResult instanceof Error) throw readyResult;
      triggerStartMs = readyResult;
      triggerDeadlineMs = readyResult + durationMilliseconds(triggerDelay);
      if (writerUsers > 0) {
        writerRunner = spawn("bash", ["tools/loadtest/run-http.sh", String(writerUsers), writerDuration, "writes"], {
          cwd: root,
          env: { ...process.env, BASE_URL: "http://proxy:8080", SESSION_OFFSET: String(writerSessionOffset), LANSURFER_LOADTEST_SESSIONS_FILE: sessionsPath },
          stdio: ["ignore", "pipe", "pipe"],
          detached: true,
        });
        const writerReady = waitForStreamLine([writerRunner.stdout, writerRunner.stderr], /loadtest-writers-ready/, 20_000);
        writerOutput = collect(writerRunner.stdout);
        writerErrorOutput = collect(writerRunner.stderr);
        writerExit = new Promise((resolveExit) => writerRunner.once("close", resolveExit));
        const writerStatus = await writerReady;
        if (!writerStatus) {
          stopProcessGroup(writerRunner);
          await writerExit;
          const [output, errorOutput] = await Promise.all([writerOutput, writerErrorOutput]);
          throw new Error(`k6 writers did not become ready within 20 seconds: ${errorOutput.trim() || output.trim() || "no output"}`);
        }
        writerReadyAtMs = Date.now();
        writerReadyAt = new Date(writerReadyAtMs).toISOString();
        if (writerReadyAtMs >= triggerDeadlineMs) runnerError ||= "k6 writers became ready after the first mutation";
        const metricsKeyFile = process.env.LANSURFER_LOADTEST_METRICS_KEY_FILE || join(homedir(), "lansurfer-loadtest-metrics-key");
        profilePath = join(reportDir, `profile-${new Date().toISOString().replaceAll(/[-:]/g, "").replace(/\.\d+Z$/, "Z")}.jsonl`);
        await mkdir(reportDir, { recursive: true, mode: 0o700 });
        profileRunner = spawn("go", [
          "run", "./tools/loadtest/profile",
          "-duration", profileDuration,
          "-interval", process.env.PROFILE_INTERVAL || "5s",
          "-metrics-key-file", metricsKeyFile,
          "-output", profilePath,
        ], {
          cwd: root,
          env: { ...process.env, LANSURFER_LOADTEST_METRICS_KEY_FILE: metricsKeyFile },
          stdio: ["ignore", "pipe", "pipe"],
          detached: true,
        });
        profileStartedAt = new Date().toISOString();
        if (Date.now() >= triggerDeadlineMs) runnerError ||= "profiler started after the first mutation";
        profileOutput = collect(profileRunner.stdout);
        profileErrorOutput = collect(profileRunner.stderr);
        profileExit = new Promise((resolveExit) => profileRunner.once("close", resolveExit));
      }
    }
    runnerExitCode = await runnerExit;
    stdout = await runnerStdout;
    runner = undefined;
    if (writerRunner) writerExitCode = await writerExit;
    if (profileRunner) profileExitCode = await profileExit;
    if (writerRunner) {
      const [output, errorOutput] = await Promise.all([writerOutput, writerErrorOutput]);
      const reportPath = /^Result:\s*(.+)$/m.exec(output)?.[1]?.trim();
      if (reportPath) {
        try {
          const report = JSON.parse(await readFile(reportPath, "utf8"));
          writerTimeouts = report.metrics?.loadtest_write_timeouts?.count ?? null;
          writerHTTP502s = report.metrics?.loadtest_write_http_502s?.count ?? null;
        } catch (error) {
          runnerError ||= `cannot read k6 writer report: ${String(error)}`;
        }
      } else if (writerExitCode === 0) {
        runnerError ||= "k6 writer report path was missing from output";
      }
      if (writerExitCode !== 0) runnerError ||= `k6 writers exited with status ${writerExitCode}: ${errorOutput.trim() || output.trim()}`;
    }
    if (profileExitCode !== null && profileExitCode !== 0) runnerError ||= `profiler exited with status ${profileExitCode}: ${(await Promise.all([profileOutput, profileErrorOutput])).join("\n").trim()}`;
    if (profileRunner) {
      try {
        const records = (await readFile(profilePath, "utf8")).trim().split("\n").map((line) => JSON.parse(line));
        const profileSummary = records.find((record) => record.kind === "summary");
        if (!profileSummary) throw new Error("profile summary is missing");
        profileInvalidWindows = profileSummary.invalid_windows;
        profileSamples = profileSummary.samples;
        profileSampleErrors = profileSummary.sample_errors;
        if (profileInvalidWindows > 0 || profileSampleErrors > 0) {
          runnerError ||= `profiler recorded ${profileInvalidWindows} invalid windows and ${profileSampleErrors} sample errors`;
        }
      } catch (error) {
        runnerError ||= `cannot read profiler output: ${String(error)}`;
      }
    }
    if (runnerExitCode !== 0) runnerError ||= stderr.trim() || stdout.trim() || "SSE runner stopped before completion";
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
      active: stats.active,
      started: stats.started,
      completed: stats.completed,
      failed: stats.failed,
      peakConcurrent: stats.peakConcurrent,
      outOfOrder: stats.outOfOrder,
      durationsMs: stats.durationsMs.slice(),
      statuses: stats.statuses.slice(),
      requestTimeline: stats.requestTimeline,
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
        return { status: 0, fetchError, stateMatches: false, mismatchedEntries: 1, visibleState: {}, authoritativeState: {}, events: window.__loadtest.events, relevantGameRequestEvents: window.__loadtest.relevantGameRequestEvents, probeSamples: window.__loadtest.probeSamples, eventTypes: window.__loadtest.eventTypes, eventTimeline: window.__loadtest.eventTimeline, opens: window.__loadtest.opens, sseErrors: window.__loadtest.sseErrors || 0, htmx: { before: window.__loadtest.htmxBefore || 0, after: window.__loadtest.htmxAfter || 0, swaps: window.__loadtest.htmxSwap || 0, errors: window.__loadtest.errors || 0 }, swapTimeline: window.__loadtest.swapTimeline, busy: 0, disabledActions: 0 };
      }
      // An in-flight request is not a stuck page: give loading state a bounded chance to clear.
      const residual = () => document.querySelectorAll("[aria-busy='true'], .htmx-request, form button:disabled, form input:disabled").length;
      for (let waited = 0; waited < 5000 && residual() > 0; waited += 100) await new Promise((resolveWait) => setTimeout(resolveWait, 100));
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
        relevantGameRequestEvents: window.__loadtest.relevantGameRequestEvents,
        probeSamples: window.__loadtest.probeSamples,
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
      relevantGameRequestEvents: state.relevantGameRequestEvents,
      probeSamples: state.probeSamples,
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
      active: measured.active,
      completed: measured.completed,
      failed: measured.failed,
      peakConcurrent: measured.peakConcurrent,
      outOfOrderCompletions: measured.outOfOrder,
      refreshLatencyMs: { p50: percentile(measured.durationsMs, 50), p95: percentile(measured.durationsMs, 95), p99: percentile(measured.durationsMs, 99), max: percentile(measured.durationsMs, 100) },
      statuses: statusCounts(measured.statuses),
      requestTimeline: measured.requestTimeline,
      initialState,
      visibleState: state.visibleState,
      authoritativeState: state.authoritativeState,
      converged: state.status === 200 && state.stateMatches,
    };
  }));
  const totalEvents = results.reduce((sum, item) => sum + item.events, 0);
  const relevantGameRequestEvents = results.reduce((sum, item) => sum + item.relevantGameRequestEvents, 0);
  const totalRefreshes = results.reduce((sum, item) => sum + item.refreshes, 0);
  const totalRefreshLatencyMs = {
    p50: percentile(allLatenciesMs, 50),
    p95: percentile(allLatenciesMs, 95),
    p99: percentile(allLatenciesMs, 99),
    max: percentile(allLatenciesMs, 100),
  };
  const probeSamples = results.flatMap((item) => item.probeSamples);

  const summary = {
    schemaVersion: 2,
    gitRevision: execFileSync("git", ["rev-parse", "HEAD"], { cwd: root, encoding: "utf8" }).trim(),
    productRevision: process.env.PRODUCT_REVISION || "",
    harnessRevision: process.env.HARNESS_REVISION || "",
    bundleSha256: process.env.BUNDLE_SHA256 || "",
    baseURL,
    requestID,
    refreshMode,
    simulatedUsers,
    browserClients,
    duration,
    triggerCount,
    triggerDelay,
    allowReconnect,
    allowMissingEvents,
    viewMode,
    filterGame: viewMode === "detail" ? null : filterGame,
    triggerInterval,
    sseReadyAt,
    runner: stdout.trim(),
    coalescing: coalescing === "1",
    probe: probe === "1",
    writerUsers,
    writerTimeouts,
    writerHTTP502s,
    writerReadyAt,
    writerExitCode,
    profileExitCode,
    signalExitCode,
    profileStartedAt,
    profilePath,
    profileSamples,
    profileInvalidWindows,
    profileSampleErrors,
    runnerExitCode,
    runnerError,
    idleError,
    failures: [],
    refreshLatencyMs: totalRefreshLatencyMs,
    totals: {
      events: totalEvents,
      relevantGameRequestEvents,
      opens: results.reduce((sum, item) => sum + item.opens, 0),
      mismatchedEntries: results.reduce((sum, item) => sum + item.mismatchedEntries, 0),
      refreshes: totalRefreshes,
      refreshesPerEvent: totalEvents === 0 ? null : Number((totalRefreshes / totalEvents).toFixed(2)),
      refreshesPerRelevantGameRequestEvent: relevantGameRequestEvents === 0 ? null : Number((totalRefreshes / relevantGameRequestEvents).toFixed(2)),
      refreshLatencyMs: totalRefreshLatencyMs,
      eventToVisibleMs: {
        samples: probeSamples.length,
        missing: probe === "1" ? Math.max(0, browserClients - probeSamples.length) : 0,
        p50: percentile(probeSamples, 50),
        p95: percentile(probeSamples, 95),
        p99: percentile(probeSamples, 99),
        max: percentile(probeSamples, 100),
      },
      failed: results.reduce((sum, item) => sum + item.failed, 0),
      peakConcurrent: Math.max(...results.map((item) => item.peakConcurrent)),
      outOfOrderCompletions: results.reduce((sum, item) => sum + item.outOfOrderCompletions, 0),
      convergedBrowsers: results.filter((item) => item.converged).length,
    },
    browsers: results,
  };
  summary.failures = calibrationFailures(summary, browserClients);
  if (runnerError) summary.failures.push(`load-test process failed: ${runnerError}`);
  if (writerUsers > 0 && (writerExitCode !== 0 || writerTimeouts === null || writerHTTP502s === null)) summary.failures.push("writer process or HTTP failure metrics are missing");
  if (writerUsers > 0 && profileExitCode !== 0) summary.failures.push("profiler process failed");
  await mkdir(reportDir, { recursive: true, mode: 0o700 });
  const reportPath = join(reportDir, `browser-calibration-${new Date().toISOString().replaceAll(/[-:]/g, "").replace(/\.\d+Z$/, "Z")}.json`);
  await writeFile(reportPath, `${JSON.stringify(summary, null, 2)}\n`, { mode: 0o600, flag: "wx" });
  console.log(JSON.stringify({ ...summary, reportPath }, null, 2));
  if (summary.failures.length > 0) {
    for (const failure of summary.failures) console.error(`browser calibration failed: ${failure}`);
    if (!signalExitCode) process.exitCode = 1;
  }
} catch (error) {
  if (!signalExitCode) throw error;
} finally {
  for (const child of [runner, writerRunner, profileRunner]) if (child) stopProcessGroup(child);
  await Promise.all(contexts.map((context) => context.close()));
  await Promise.all(browsers.map((browser) => browser.close()));
}

function integerEnv(name, fallback, min, max) {
  const value = Number(process.env[name] || fallback);
  if (!Number.isInteger(value) || value < min || value > max) throw new Error(`${name} must be an integer in ${min}..${max}`);
  return value;
}
function durationMilliseconds(value) {
  const match = /^(\d+)(ms|s|m)$/.exec(value);
  if (!match) return 0;
  return Number(match[1]) * ({ ms: 1, s: 1000, m: 60_000 }[match[2]]);
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

function stopProcessGroup(child) {
  if (!child.pid) return;
  try {
    process.kill(-child.pid, "SIGTERM");
  } catch {
    child.kill("SIGTERM");
  }
}
function waitForStreamLine(streams, pattern, timeoutMs) {
  return new Promise((resolveWait) => {
    const inputs = Array.isArray(streams) ? streams : [streams];
    const listeners = new Map();
    let value = "";
    const finish = (ready) => {
      clearTimeout(timer);
      for (const [stream, listener] of listeners) stream.off("data", listener);
      resolveWait(ready);
    };
    const timer = setTimeout(() => finish(false), timeoutMs);
    for (const stream of inputs) {
      const listener = (chunk) => {
        value += chunk;
        if (pattern.test(value)) finish(true);
      };
      listeners.set(stream, listener);
      stream.on("data", listener);
    }
  });
}
async function collect(stream) {
  let value = "";
  for await (const chunk of stream) value += chunk;
  return value;
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
