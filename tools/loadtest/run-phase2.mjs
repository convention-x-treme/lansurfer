import { spawn } from "node:child_process";
import { resolve } from "node:path";

const root = resolve(import.meta.dirname, "../..");
const request = process.env.REQUEST;
const duration = process.env.DURATION ?? "90s";
const browsers = Number(process.env.BROWSERS ?? 5);
const users = Number(process.env.USERS ?? 120);
if (!request) throw new Error("REQUEST must be reserved control Game Request ID");
if (![5, 10].includes(browsers)) throw new Error("BROWSERS must be 5 or 10");
if (![120, 400].includes(users)) throw new Error("USERS must be 120 or 400");
const modes = process.env.MODE ? [process.env.MODE] : ["browser-like", "worst-case", "worst-case", "browser-like"];

const phase = process.env.PHASE ?? "all";
if (!["calibrate", "loss", "all"].includes(phase)) throw new Error("PHASE must be calibrate, loss or all");
async function run(command, args, env = process.env) {
  return new Promise((resolveRun, reject) => {
    const child = spawn(command, args, { cwd: root, env, stdio: "inherit" });
    child.once("error", reject);
    child.once("close", (code) => code === 0 ? resolveRun() : reject(new Error(`${command} exited ${code}`)));
  });
}
async function snapshot() {
  return new Promise((resolveRun, reject) => {
    const child = spawn("go", ["run", "./tools/loadtest/state", "-mode", "snapshot", "-request", request], { cwd: root, env: process.env, stdio: ["ignore", "pipe", "inherit"] });
    let output = "";
    child.stdout.setEncoding("utf8").on("data", (chunk) => { output += chunk; });
    child.once("error", reject);
    child.once("close", (code) => code === 0 ? resolveRun(output.trim()) : reject(new Error(`snapshot exited ${code}`)));
  });
}
async function control(body) {
  const response = await fetch("http://127.0.0.1:18082/control", { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify(body) });
  if (!response.ok) throw new Error(`proxy control failed: ${response.status}`);
}
async function resetControl() {
  const home = process.env.HOME;
  await run("go", ["run", "./tools/loadtest/state", "-mode", "prepare", "-sessions", process.env.LANSURFER_LOADTEST_SESSIONS_FILE || `${home}/lansurfer-loadtest-sessions.jsonl`, "-fixtures", process.env.LANSURFER_LOADTEST_FIXTURES_FILE || `${home}/lansurfer-loadtest-fixtures.json`, "-output", `${home}/lansurfer-loadtest-control.json`]);
}
if (phase !== "loss") {
for (const mode of modes) {
  for (const writers of [0, 100]) {
    const env = { ...process.env, REQUEST: request, SIMULATED_USERS: String(users), BROWSER_CLIENTS: String(browsers), DURATION: duration, TRIGGER: "20", TRIGGER_INTERVAL: "500ms", VIEW: "mixed", REFRESH_MODE: mode };
    await resetControl();
    const before = await snapshot();
    const writer = writers ? run("bash", ["tools/loadtest/run-http.sh", "100", duration, "writes"], { ...env, SESSION_OFFSET: "5", BASE_URL: "http://proxy:8080" }) : Promise.resolve();
    try {
      await Promise.all([run("node", ["tools/loadtest/browser-calibration.mjs"], env), writer]);
    } catch (error) {
      process.exitCode = 1;
      console.error(`matrix cell ${mode}/${writers} writers failed: ${error.message}`);
    }
    console.log(JSON.stringify({ users, mode, writers, before: JSON.parse(before), after: JSON.parse(await snapshot()) }));
  }
}
if (users === 120) {
  // 115 synthetic streams and five browser sessions use 120 distinct identities.
  try {
    await run("node", ["tools/loadtest/browser-calibration.mjs"], { ...process.env, REQUEST: request, SIMULATED_USERS: "115", BROWSER_CLIENTS: "5", DURATION: duration, TRIGGER: "20", VIEW: "mixed" });
  } catch (error) {
    process.exitCode = 1;
    console.error(`115+5 identity run failed: ${error.message}`);
  }
}
for (const [trigger, interval] of [["20", "500ms"], ["40", "20ms"]]) {
  await resetControl();
  try {
    await run("node", ["tools/loadtest/browser-calibration.mjs"], { ...process.env, REQUEST: request, SIMULATED_USERS: String(users), BROWSER_CLIENTS: String(browsers), DURATION: "45s", TRIGGER: trigger, TRIGGER_INTERVAL: interval, VIEW: "mixed", REFRESH_MODE: modes[0] });
  } catch (error) {
    process.exitCode = 1;
    console.error(`burst ${trigger}/${interval} failed: ${error.message}`);
  }
  console.log(JSON.stringify({ trigger, interval, snapshot: JSON.parse(await snapshot()) }));
}
for (const fault of [{ mode: "hold-refresh", delay_ms: 500 }, { mode: "fail-refresh", duration_ms: 1000 }, { mode: "cut-streams", duration_ms: 1000 }]) {
  try {
    await resetControl();
    await control(fault);
    try {
      await run("node", ["tools/loadtest/browser-calibration.mjs"], { ...process.env, BASE_URL: "http://127.0.0.1:18081", REQUEST: request, SIMULATED_USERS: String(users), BROWSER_CLIENTS: String(browsers), DURATION: "10s", TRIGGER: "2", VIEW: "mixed", REFRESH_MODE: "browser-like" });
    } catch (error) {
      process.exitCode = 1;
      console.error(`${fault.mode} refresh fault run failed: ${error.message}`);
    }
  } finally {
    await control({ mode: "normal" });
  }
}
try {
  await resetControl();
  await run("node", ["tools/loadtest/browser-calibration.mjs"], { ...process.env, BASE_URL: "http://127.0.0.1:18081", REQUEST: request, SIMULATED_USERS: String(users), BROWSER_CLIENTS: String(browsers), DURATION: "10s", TRIGGER: "2", VIEW: "mixed", REFRESH_MODE: "browser-like", ABORT_REFRESH: "1" });
} catch (error) {
  process.exitCode = 1;
  console.error(`aborted refresh run failed (product or harness): ${error.message}`);
}
}
if (phase !== "calibrate") {
async function audit() {
  await run("go", ["run", "./tools/loadtest/state", "-mode", "audit", "-request", request]);
}
const home = process.env.HOME;
const controlFile = `${home}/lansurfer-loadtest-control.json`;
const sessionsFile = process.env.LANSURFER_LOADTEST_SESSIONS_FILE || `${home}/lansurfer-loadtest-sessions.jsonl`;
const fixturesFile = process.env.LANSURFER_LOADTEST_FIXTURES_FILE || `${home}/lansurfer-loadtest-fixtures.json`;
const controlGame = await new Promise((resolveRun, reject) => {
  const child = spawn("docker", ["compose", "-f", "tools/loadtest/compose.yaml", "exec", "-T", "postgres", "psql", "-U", "lansurfer_loadtest", "-d", "lansurfer_loadtest", "-Atc", `SELECT g.name FROM games g JOIN game_requests r ON r.game_id=g.id WHERE r.id='${request}'::uuid`], { cwd: root, stdio: ["ignore", "pipe", "inherit"] });
  let output = "";
  child.stdout.setEncoding("utf8").on("data", (chunk) => { output += chunk; });
  child.once("error", reject);
  child.once("close", (code) => code === 0 && output.trim() ? resolveRun(output.trim()) : reject(new Error("cannot read control game")));
});
for (const [view, eventType, triggerMode] of [["discovery", "GameRequestJoined", "join-leave"], ["detail", "GameRequestJoined", "join-leave"], ["lobby", "game-request-chat", "chat"]]) {
  try {
    // Previous runs may leave the trigger player joined; domain Leave restores the control request.
    await run("go", ["run", "./tools/loadtest/state", "-mode", "prepare", "-sessions", sessionsFile, "-fixtures", fixturesFile, "-output", controlFile]);
    await control({ mode: "drop-sse", drop_count: 1, request_id: request, event_type: eventType });
    const before = await snapshot();
    try {
      // Lost-signal runs stay red when reconnect-only convergence fails; recovery is reported inside the browser summary.
      await run("node", ["tools/loadtest/browser-calibration.mjs"], { ...process.env, BASE_URL: "http://127.0.0.1:18081", REQUEST: request, SIMULATED_USERS: String(users), BROWSER_CLIENTS: String(browsers), DURATION: "15s", TRIGGER: "1", TRIGGER_MODE: triggerMode, VIEW: view === "lobby" ? "mixed" : view, FILTER_GAME: controlGame, LOBBY_BROWSER: view === "lobby" ? "1" : "0", REFRESH_MODE: "browser-like", ALLOW_MISSING_EVENTS: "1", VISIBILITY_RECOVERY: "1" });
    } catch (error) {
      process.exitCode = 1;
      console.error(`lost-signal ${view} failed (product or harness): ${error.message}`);
    }
    console.log(JSON.stringify({ lostEventType: eventType, view, before: JSON.parse(before), after: JSON.parse(await snapshot()) }));
    await audit();
  } finally {
    await control({ mode: "normal" });
  }
}
}
