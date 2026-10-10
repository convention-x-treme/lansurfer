// Authenticated LANsurfer GET load profile; detail views write recent history.
// Target stays fixed to the private Compose service; never send session tokens elsewhere.
import http from 'k6/http';
import { check, sleep, randomSeed } from 'k6';
import { SharedArray } from 'k6/data';
import { Rate } from 'k6/metrics';

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

const requestIDs = new SharedArray('lansurfer-loadtest-request-ids', () => {
  const manifest = JSON.parse(open('/fixtures.json'));
  const ids = Object.keys(manifest.requests || {}).reduce((all, kind) => all.concat(manifest.requests[kind]), []);
  if (ids.length === 0 || ids.some((id) => typeof id !== 'string' || !id)) {
    throw new Error('Invalid Game Request IDs in fixtures JSON');
  }
  return ids;
});
export const options = {
  scenarios: {
    authenticated_browsing: {
      executor: 'constant-vus',
      vus: users,
      duration,
      gracefulStop: '10s',
    },
  },
  discardResponseBodies: false,
  summaryTrendStats: ['avg', 'min', 'med', 'p(90)', 'p(95)', 'p(99)', 'max'],
  thresholds: {
    loadtest_unexpected_response_rate: ['rate<0.01'],
  },
};

const unexpectedResponses = new Rate('loadtest_unexpected_response_rate');
const pageMix = [
  { tag: 'game_requests_list', path: '/game-requests/', htmx: false },
  { tag: 'game_requests_open', path: '/game-requests/?open_places=1', htmx: true, openOnly: true },
  { tag: 'game_requests_search', path: '/game-requests/?q=Warcraft%20III', htmx: true },
  { tag: 'game_requests_mine', path: '/game-requests/?q=mine', htmx: true },
  { tag: 'game_requests_craft', path: '/game-requests/?q=craft', htmx: true },
  { tag: 'game_requests_large_game', path: '/game-requests/?q=Loadtest%20Game%200999', htmx: true },
  { tag: 'game_requests_no_match', path: '/game-requests/?q=loadtest-no-match', htmx: true, empty: true },
  { tag: 'game_requests_my_games', path: '/game-requests/?scope=my-games', htmx: true },
  { tag: 'game_requests_joined', path: '/game-requests/?scope=joined', htmx: true },
  { tag: 'game_requests_created', path: '/game-requests/?scope=mine', htmx: true },
  { tag: 'game_requests_refresh', path: '/game-requests/refresh', htmx: true },
  { tag: 'game_catalog', path: '/games/', htmx: false },
  { tag: 'game_catalog_search', path: '/games/?q=craft', htmx: true },
  { tag: 'followed_games', path: '/games/?followed=1', htmx: false },
  { tag: 'game_requests_filtered', path: '/game-requests/?open_places=1&q=Warcraft%20III', htmx: true, openOnly: true },
  { tag: 'game_request_detail', path: '', detail: true, htmx: false },
  { tag: 'game_request_detail_refresh', path: '', detail: true, refresh: true, htmx: true },
];

// Fixture-owned expectations only: no whole-page text pinning.
function expectations(page, body, requestID) {
  if (page.detail) return page.refresh || body.includes('data-game-request-id="' + requestID + '"');
  if (page.empty) return !body.includes('data-game-request-id=');
  return true;
}

export default function () {
  if (__ITER === 0) randomSeed(SEED + __VU);
  const session = sessions[SESSION_OFFSET + __VU - 1];
  const page = pageMix[Math.floor(Math.random() * pageMix.length)];
  const requestID = requestIDs[Math.floor(Math.random() * requestIDs.length)];
  const path = page.detail
    ? `/game-requests/${requestID}${page.refresh ? '/refresh' : ''}`
    : page.path;
  const headers = { Accept: 'text/html' };
  if (page.htmx) headers['HX-Request'] = 'true';

  const result = http.get(BASE_URL + path, {
    headers,
    cookies: { lansurfer_session: { value: session.session_token, replace: true } },
    redirects: 0, // Login redirects must not masquerade as HTTP 200.
    timeout: '20s',
    tags: { endpoint: page.tag },
  });
  const ok = result.status === 200 && expectations(page, String(result.body || ''), requestID);
  unexpectedResponses.add(!ok, { endpoint: page.tag });
  check(result, { 'authenticated HTTP 200 with fixture-owned expectations': () => ok }, { endpoint: page.tag });
  sleep(0.7 + Math.random() * 2.3);
}
