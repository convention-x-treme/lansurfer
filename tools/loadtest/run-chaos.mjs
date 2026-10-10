// Chaos coordinator: healthy baseline, one fault window via the isolated proxy,
// recovery, serial control mutations through the direct app port, then audit.
// Env: REQUEST (control Game Request ID), USERS=120|400, BROWSERS=5|10,
// SCENARIO=cut|offline|delay|loss, DURATION=120s..30m (default 180s), DELAY_MS=200|500.
import { spawn, execFileSync } from "node:child_process";
import { mkdir, readFile, writeFile } from "node:fs/promises";
import { homedir } from "node:os";
import { join, resolve } from "node:path";

process.umask(0o077);
const root = resolve(import.meta.dirname, "../..");
const request = process.env.REQUEST ?? "";
const users = Number(process.env.USERS ?? 120);
const browsers = Number(process.env.BROWSERS ?? 5);
const scenario = process.env.SCENARIO ?? "";
const durationText = process.env.DURATION ?? "180s";
const delayMs = Number(process.env.DELAY_MS ?? 200);
const direct = "http://127.0.0.1:18080";
const sessionsPath = process.env.LANSURFER_LOADTEST_SESSIONS_FILE || join(homedir(), "lansurfer-loadtest-sessions.jsonl");
const reportRoot = process.env.LANSURFER_LOADTEST_REPORT_DIR || join(homedir(), "lansurfer-loadtest-reports");
const compose = ["compose", "-p", "lansurfer-loadtest", "-f", "tools/loadtest/compose.yaml", "-f", "tools/loadtest/compose.chaos.yaml"];

if (!/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(request)) throw new Error("REQUEST must be the control Game Request UUID");
if (![120, 400].includes(users)) throw new Error("USERS must be 120 or 400");
if (![5, 10].includes(browsers)) throw new Error("BROWSERS must be 5 or 10");
if (!["cut", "offline", "delay", "loss"].includes(scenario)) throw new Error("SCENARIO must be cut, offline, delay or loss");
if (![200, 500].includes(delayMs)) throw new Error("DELAY_MS must be 200 or 500");
const durationMatch = /^([1-9]\d*)(s|m)$/.exec(durationText);
const durationSeconds = durationMatch ? Number(durationMatch[1]) * (durationMatch[2] === "m" ? 60 : 1) : 0;
if (durationSeconds < 120 || durationSeconds > 1800) throw new Error("DURATION must be between 120s and 30m");

const sessions = (await readFile(sessionsPath, "utf8")).trim().split("\n").map((line) => JSON.parse(line));
const offset = users === 400 ? 0 : 5;
const checked = [...new Set([...Array(browsers).keys(), ...Array.from({ length: users }, (_, i) => offset + i)])];
const triggerSession = sessions[sessions.length - 1];
const fault = {
  cut: { at: 30, body: { mode: "cut-streams", duration_ms: 1000 }, end: 31 },
  offline: { at: 30, body: { mode: "offline", duration_ms: 60000 }, end: 90 },
  delay: { at: 30, body: { mode: "delay", duration_ms: 60000, delay_ms: delayMs }, end: 90 },
  loss: { at: 30, body: { mode: "loss", duration_ms: 10000 }, end: 40 },
}[scenario];

const sleep = (ms) => new Promise((done) => setTimeout(done, ms));
const report = { schemaVersion: 1, scenario, users, browsers, duration: durationText, delayMs: scenario === "delay" ? delayMs : null, request, timeline: [], mutations: { ok: 0, failedInFault: 0, failedOutsideFault: 0 }, errors: [] };
let t0 = 0;
const mark = (event, extra = {}) => report.timeline.push({ tSeconds: Number(((Date.now() - t0) / 1000).toFixed(1)), event, ...extra });

async function control(body) {
  return fetch("http://127.0.0.1:18082/control", { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify(body), signal: AbortSignal.timeout(10_000) });
}
function qdisc() {
  try {
    return execFileSync("docker", [...compose, "exec", "-T", "proxy", "tc", "-s", "qdisc", "show", "dev", "eth0"], { cwd: root, encoding: "utf8", timeout: 15_000 }).trim().slice(0, 2000);
  } catch (error) {
    return `unavailable: ${error.message.split("\n")[0]}`;
  }
}
async function sessionsValid() {
  let bad = 0;
  for (let i = 0; i < checked.length; i += 20) {
    const results = await Promise.all(checked.slice(i, i + 20).map(async (row) => {
      try {
        const response = await fetch(`${direct}/game-requests/`, { redirect: "manual", headers: { cookie: `lansurfer_session=${sessions[row].session_token}` }, signal: AbortSignal.timeout(20_000) });
        await response.arrayBuffer();
        return response.status === 200;
      } catch {
        return false;
      }
    }));
    bad += results.filter((ok) => !ok).length;
  }
  return { checked: checked.length, invalid: bad };
}
function goState(mode) {
  const args = ["run", "./tools/loadtest/state", "-mode", mode, "-request", request];
  if (mode === "snapshot") args.push("-viewer-id", triggerSession.user_id);
  return execFileSync("go", args, { cwd: root, encoding: "utf8", timeout: 120_000 });
}

// Control mutations: one real non-member player over the direct port, serial.
async function mutate(stopAt) {
  const headers = { cookie: `lansurfer_session=${triggerSession.session_token}` };
  const page = await fetch(`${direct}/game-requests/`, { redirect: "manual", headers });
  await page.arrayBuffer();
  const csrf = page.headers.getSetCookie().map((cookie) => /^lansurfer_csrf=([^;]+)/.exec(cookie)?.[1]).find(Boolean);
  if (page.status !== 200 || !csrf) throw new Error(`control player cannot get a CSRF token (status ${page.status})`);
  let joined = false;
  while (Date.now() < stopAt) {
    const action = joined ? "leave" : "join";
    try {
      const response = await fetch(`${direct}/game-requests/${request}/${action}`, {
        method: "POST", redirect: "manual", signal: AbortSignal.timeout(30_000),
        headers: { ...headers, cookie: `${headers.cookie}; lansurfer_csrf=${csrf}`, "content-type": "application/x-www-form-urlencoded", "hx-request": "true" },
        body: new URLSearchParams({ csrf_token: csrf, return_to: "list" }),
      });
      await response.arrayBuffer();
      if (response.status === 200 && !response.headers.get("hx-retarget")) { joined = !joined; report.mutations.ok++; }
      else report.mutations.failedOutsideFault++;
    } catch {
      report.mutations.failedOutsideFault++;
    }
    await sleep(500);
  }
  return joined;
}

let child;
let exitCode = 0;
let capability = "not required";
try {
  if (scenario === "delay" || scenario === "loss") {
    const probe = await control(scenario === "delay" ? { mode: "delay", duration_ms: 1000, delay_ms: delayMs } : { mode: "loss", duration_ms: 1000 });
    if (!probe.ok) {
      capability = `netem unavailable (${probe.status}); ${scenario} network fault NOT EXECUTED`;
      report.errors.push(capability);
      throw Object.assign(new Error(capability), { capability: true });
    }
    capability = "netem available";
    await control({ mode: "normal" });
  }
  report.sessionsBefore = await sessionsValid();
  report.snapshotBefore = JSON.parse(goState("snapshot"));

  child = spawn("node", ["tools/loadtest/browser-calibration.mjs"], {
    cwd: root, stdio: ["ignore", "pipe", "pipe"],
    env: { ...process.env, BASE_URL: "http://127.0.0.1:18081", REQUEST: request, SIMULATED_USERS: String(users), BROWSER_CLIENTS: String(browsers), DURATION: durationText, TRIGGER: "0", VIEW: "mixed", REFRESH_MODE: "browser-like", ALLOW_RECONNECT: "1", ALLOW_MISSING_EVENTS: "1" },
  });
  let stdout = "";
  child.stdout.setEncoding("utf8").on("data", (chunk) => { stdout += chunk; });
  const closed = new Promise((done) => child.once("close", done));
  await new Promise((ready, fail) => {
    child.stderr.setEncoding("utf8").on("data", (chunk) => { if (chunk.includes("loadtest-ready")) ready(); });
    child.once("close", () => fail(new Error("browser calibration ended before streams were started")));
  });
  t0 = Date.now();
  mark("baseline-start");
  const stopAt = t0 + (durationSeconds - 10) * 1000;
  const mutating = mutate(stopAt);
  await sleep(fault.at * 1000);
  const injected = await control(fault.body);
  mark("fault-start", { accepted: injected.ok, status: injected.status });
  if (!injected.ok) { report.errors.push(`fault control rejected (${injected.status})`); }
  report.qdiscDuringFault = scenario === "delay" || scenario === "loss" ? qdisc() : undefined;
  await sleep((fault.end - fault.at) * 1000);
  mark("fault-end");
  const expectedJoined = await mutating;
  mark("mutations-stopped");
  await closed;
  const summary = (() => { try { return JSON.parse(stdout.slice(stdout.indexOf("{"))); } catch { return null; } })();
  if (!summary) report.errors.push("no browser calibration summary");
  else {
    report.browserSummary = { runnerExitCode: summary.runnerExitCode, runnerError: summary.runnerError, idleError: summary.idleError, runner: summary.runner, totals: summary.totals, browsers: summary.browsers.map(({ view, events, opens, sseErrors, refreshes, failed, htmx, busy, disabledActions, converged, visibleState, authoritativeState }) => ({ view, events, opens, sseErrors, refreshes, failed, htmx, busy, disabledActions, converged, visibleState, authoritativeState })) };
    if (summary.runnerExitCode !== 0) report.errors.push("SSE runner failed (streams missing or not recovered)");
    if (summary.idleError) report.errors.push(summary.idleError);
    for (const [index, item] of summary.browsers.entries()) {
      if (!item.converged) report.errors.push(`browser ${index} DOM diverges from authoritative state after recovery`);
      if (item.busy > 0 || item.disabledActions > 0) report.errors.push(`browser ${index} has residual loading/disabled state`);
    }
  }
  report.sessionsAfter = await sessionsValid();
  if (report.sessionsAfter.invalid || report.sessionsBefore.invalid) report.errors.push("session validity changed or invalid");
  report.snapshotAfter = JSON.parse(goState("snapshot"));
  if (report.snapshotAfter.viewer_joined !== expectedJoined) report.errors.push("DB membership diverges from acknowledged control mutations");
  try { goState("audit"); report.audit = "ok"; } catch (error) { report.audit = "failed"; report.errors.push(`audit failed: ${error.message.split("\n")[0]}`); }
} catch (error) {
  report.errors.push(error.message);
  if (error.capability) exitCode = 2;
} finally {
  try {
    const response = await control({ mode: "normal" });
    const state = await fetch("http://127.0.0.1:18082/state").then((r) => r.json()).catch(() => null);
    report.proxyAfter = { normalAccepted: response.ok, mode: state?.mode ?? null };
    if (!response.ok || state?.mode !== "normal") report.errors.push("proxy did not return to normal");
  } catch (error) {
    report.errors.push(`proxy cleanup failed: ${error.message}`);
  }
  report.capability = capability;
  if (scenario === "delay" || scenario === "loss") report.qdiscAfter = qdisc();
  if (child && child.exitCode === null) child.kill("SIGTERM");
  report.finishedErrors = report.errors.length;
  const directory = join(reportRoot, `${new Date().toISOString().replaceAll(/[-:]/g, "").replace(/\.\d+Z$/, "Z")}-chaos-${scenario}`);
  await mkdir(directory, { recursive: true, mode: 0o700 });
  await writeFile(join(directory, "chaos.json"), `${JSON.stringify(report, null, 2)}\n`, { mode: 0o600, flag: "wx" });
  console.log(JSON.stringify({ scenario, users, errors: report.errors, capability, report: join(directory, "chaos.json") }, null, 2));
  if (report.errors.length > 0) process.exitCode = exitCode || 1;
}
