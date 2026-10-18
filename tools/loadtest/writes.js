// Mixed read/write LANsurfer load profile (create, join, leave, chat) with real
// session + CSRF handling. Target is the private Docker Compose service only.
//
// Each virtual player uses its own seeded session in its own cookie jar. The
// lansurfer_csrf cookie is issued by LANsurfer itself on a normal page GET and
// echoed in the csrf_token form field exactly like the rendered forms do.
//
// Outcome classes:
//   ok          the action succeeded
//   conflict    expected business refusal (full, already member, not a member,
//               chat rate limit). HTMX answers 200 + HX-Retarget #live-notices.
//   unexpected  403 (CSRF/permission), redirect to login, 4xx/5xx, anything else
import http from 'k6/http';
import { check, sleep, randomSeed } from 'k6';
import { SharedArray } from 'k6/data';
import { Counter, Rate } from 'k6/metrics';
import { classifyHtmx, isCsrfRejection } from './outcomes.mjs';

const writeTimeouts = new Counter('loadtest_write_timeouts');
const writeHTTP502s = new Counter('loadtest_write_http_502s');
const BASE_URL = __ENV.BASE_URL || 'http://lansurfer:8080';
const SESSION_OFFSET = Number(__ENV.SESSION_OFFSET || '0');
const SEED = Number(__ENV.SEED || '139');
const users = Number(__ENV.USERS || '20');
const duration = __ENV.DURATION || '1m';
if (BASE_URL !== 'http://lansurfer:8080' && BASE_URL !== 'http://proxy:8080') {
  throw new Error('BASE_URL is not an allowed isolated origin');
}
if (!Number.isInteger(SESSION_OFFSET) || SESSION_OFFSET < 0 || !Number.isInteger(SEED) || SEED < 0) {
  throw new Error('SESSION_OFFSET and SEED must be non-negative integers');
}
if (!Number.isInteger(users) || users < 1 || users > 2000) {
  throw new Error('USERS must be an integer between 1 and 2000');
}

const sessions = new SharedArray('lansurfer-local-test-sessions', () => {
  const records = open('/sessions.jsonl').trim().split(/\r?\n/).map((line) => JSON.parse(line));
  if (records.length === 0 || records.some((item) => typeof item.session_token !== 'string' || !item.session_token)) {
    throw new Error('Invalid sessions JSONL file');
  }
  return records;
});
if (SESSION_OFFSET + users > sessions.length) {
  throw new Error('SESSION_OFFSET plus USERS exceeds the seeded sessions');
}
// Fixture IDs only (no tokens): { games: [...], requests: { open: [...], ... } }.
const fixtures = new SharedArray('lansurfer-local-test-fixtures', () => [JSON.parse(open('/fixtures.json'))])[0];
const joinable = [].concat(fixtures.requests.open, fixtures.requests.nearly_full, fixtures.requests.empty);

const actions = ['browse', 'create', 'join', 'leave', 'chat', 'csrf_negative'];
const outcomes = ['ok', 'conflict', 'unexpected'];
const outcomeCounter = new Counter('loadtest_outcomes');
const unexpectedRate = new Rate('loadtest_unexpected_response_rate');

// Fail the run on every unexpected outcome; latency thresholds export action metrics only.
const thresholds = { loadtest_unexpected_response_rate: ['rate==0'] };
for (const action of actions) {
  thresholds['http_req_duration{endpoint:' + action + '}'] = ['p(95)>=0', 'p(99)>=0'];
  for (const outcome of outcomes) {
    thresholds['loadtest_outcomes{action:' + action + ',outcome:' + outcome + '}'] = ['count>=0'];
  }
}
thresholds.loadtest_write_timeouts = ['count>=0'];
thresholds.loadtest_write_http_502s = ['count>=0'];

export const options = {
  scenarios: {
    authenticated_writes: { executor: 'constant-vus', vus: users, duration, gracefulStop: '10s' },
  },
  // k6 clears each VU's cookie jar between iterations by default; the session
  // and CSRF cookies must live as long as a browser's would.
  discardResponseBodies: false,
  noCookiesReset: true,
  summaryTrendStats: ['avg', 'min', 'med', 'p(90)', 'p(95)', 'p(99)', 'max'],
  thresholds,
};

export function setup() {
  console.log("loadtest-writers-ready");
  return {};
}

// Per-VU state.
let jar; // created inside the VU: cookie jars are unsupported in the init context
let primed = false;
const memberOf = []; // request IDs this VU joined (leavable)
const hosted = []; // request IDs this VU created (chat only)

function header(res, name) {
  const wanted = name.toLowerCase();
  for (const key of Object.keys(res.headers)) {
    if (key.toLowerCase() === wanted) return res.headers[key];
  }
  return '';
}

function csrfToken() {
  const cookie = jar.cookiesForURL(BASE_URL + '/game-requests/').lansurfer_csrf;
  return cookie && cookie.length ? cookie[0] : '';
}

// A normal page GET makes LANsurfer issue the CSRF cookie, like a browser.
function ensureCsrf() {
  if (csrfToken()) return;
  http.get(BASE_URL + '/game-requests/new', {
    headers: { Accept: 'text/html' }, redirects: 0, timeout: '20s', tags: { endpoint: 'page' },
  });
}

let unexpectedLogged = 0;
function record(action, outcome, res) {
  outcomeCounter.add(1, { action, outcome });
  unexpectedRate.add(outcome === 'unexpected', { action });
  if (res?.error_code === 1050) writeTimeouts.add(1, { action });
  if (res?.status === 502) writeHTTP502s.add(1, { action });
  // Diagnose without leaking secrets: status and redirect target only, first few per VU.
  if (outcome === 'unexpected' && res && unexpectedLogged++ < 3) {
    console.error('unexpected ' + action + ': status=' + res.status + ' location=' + header(res, 'Location') + ' token_sent=' + lastTokenSent + ' csrf_rejection=' + isCsrfRejection(res) + ' retarget=' + header(res, 'HX-Retarget'));
  }
}

let lastTokenSent = '';
function post(action, path, fields, htmx, withToken) {
  const body = Object.assign({}, fields);
  const token = withToken ? csrfToken() : '';
  lastTokenSent = withToken ? (token ? 'present' : 'ABSENT') : 'not-requested';
  if (withToken) body.csrf_token = token;
  const headers = { Accept: 'text/html' };
  if (htmx) headers['HX-Request'] = 'true';
  return http.post(BASE_URL + path, body, { headers, redirects: 0, timeout: '20s', tags: { endpoint: action } });
}


function classifyAction(res, action, plannedConflict = false) {
  if (action !== 'csrf_negative' && isCsrfRejection(res)) jar.delete(BASE_URL, 'lansurfer_csrf');
  return classifyHtmx({ status: res.status, headers: res.headers, body: res.body }, action, { plannedConflict });
}
function pick(list) {
  return list[Math.floor(Math.random() * list.length)];
}

function doBrowse() {
  const res = http.get(BASE_URL + '/game-requests/' + pick(['?scope=joined', '?q=craft', '?q=mine']), {
    headers: { Accept: 'text/html' }, redirects: 0, timeout: '20s', tags: { endpoint: 'browse' },
  });
  record('browse', res.status === 200 ? 'ok' : 'unexpected', res);
}

function doJoin() {
  const id = pick(joinable);
  const res = post('join', '/game-requests/' + id + '/join', { return_to: 'list' }, true, true);
  const outcome = classifyAction(res, 'join');
  if (outcome === 'ok' && memberOf.indexOf(id) < 0) memberOf.push(id);
  record('join', outcome, res);
}

function doLeave() {
  // With nothing joined yet, leave a random request: a legitimate "not a member" conflict.
  const id = memberOf.length ? memberOf[Math.floor(Math.random() * memberOf.length)] : pick(joinable);
  const res = post('leave', '/game-requests/' + id + '/leave', { return_to: 'list' }, true, true);
  const outcome = classifyAction(res, 'leave');
  const index = memberOf.indexOf(id);
  if (outcome === 'ok' && index >= 0) memberOf.splice(index, 1);
  record('leave', outcome, res);
}

function doChat() {
  const pool = memberOf.concat(hosted);
  const id = pool.length ? pick(pool) : pick(joinable);
  const res = post('chat', '/game-requests/' + id + '/chat', { body: 'load test message ' + __ITER }, true, true);
  record('chat', classifyAction(res, 'chat'), res);
}

function doCreate() {
  const res = post('create', '/game-requests', {
    game_id: pick(fixtures.games),
    max_players: String(2 + Math.floor(Math.random() * 7)),
    start_preset: '30',
    description: 'load test ' + __VU + '/' + __ITER,
  }, false, true);
  const location = header(res, 'Location');
  const match = res.status === 303 && /^\/game-requests\/([0-9a-f-]{36})\?notice=request-created$/.exec(location);
  if (match) {
    hosted.push(match[1]);
    record('create', 'ok');
  } else {
    if (isCsrfRejection(res)) jar.delete(BASE_URL, 'lansurfer_csrf');
    record('create', 'unexpected', res);
  }
}

// Authenticated POST without a CSRF token must be refused with 403 and change nothing.
function doCsrfNegative() {
  const res = post('csrf_negative', '/game-requests/' + pick(joinable) + '/join', { return_to: 'list' }, true, false);
  const refused = classifyAction(res, 'csrf_negative') === 'csrf_403';
  check(res, { 'missing CSRF token is refused as invalid CSRF token': () => refused });
  record('csrf_negative', refused ? 'ok' : 'unexpected', res);
}

export function runWriteAction(action) {
  if (!primed) {
    randomSeed(SEED + __VU);
    jar = http.cookieJar();
    jar.set(BASE_URL, 'lansurfer_session', sessions[SESSION_OFFSET + __VU - 1].session_token, { path: '/' });
    primed = true;
  }
  ensureCsrf();
  if (action === 'weighted') {
    const roll = Math.random();
    if (roll < 0.70) action = 'browse';
    else if (roll < 0.73) action = 'create';
    else if (roll < 0.83) action = 'join';
    else if (roll < 0.88) action = 'leave';
    else action = 'chat';
  } else if (action === 'mixed') {
    const roll = Math.random();
    if (roll < 0.05) action = 'create';
    else if (roll < 0.40) action = 'join';
    else if (roll < 0.55) action = 'leave';
    else if (roll < 0.97) action = 'chat';
    else action = 'csrf_negative';
  }
  if (action === 'browse') doBrowse();
  else if (action === 'create') doCreate();
  else if (action === 'join') doJoin();
  else if (action === 'leave') doLeave();
  else if (action === 'chat') doChat();
  else if (action === 'csrf_negative') doCsrfNegative();
  else throw new Error('Unknown write action: ' + action);
  sleep(__ENV.WORKLOAD === 'soak' ? 2 + Math.random() * 6 : 0.7 + Math.random() * 2.3);
}

export default function () {
  runWriteAction(__ENV.WORKLOAD === 'soak' ? 'weighted' : 'mixed');
}
