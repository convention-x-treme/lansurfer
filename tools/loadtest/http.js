// Read-only LANsurfer load profile. The target is deliberately fixed to the
// private Docker Compose service; never send session tokens to other hosts.
import http from 'k6/http';
import { check, sleep } from 'k6';
import { SharedArray } from 'k6/data';
import { Rate } from 'k6/metrics';

const BASE_URL = 'http://lansurfer:8080';
const users = Number(__ENV.USERS || '20');
const duration = __ENV.DURATION || '1m';

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
if (users > sessions.length) {
  throw new Error('USERS=' + users + ' exceeds the ' + sessions.length + ' seeded sessions');
}

export const options = {
  scenarios: {
    authenticated_browsing: {
      executor: 'constant-vus',
      vus: users,
      duration,
      gracefulStop: '10s',
    },
  },
  discardResponseBodies: true,
  summaryTrendStats: ['avg', 'min', 'med', 'p(90)', 'p(95)', 'p(99)', 'max'],
  thresholds: {
    loadtest_unexpected_response_rate: ['rate<0.01'],
  },
};

const unexpectedResponses = new Rate('loadtest_unexpected_response_rate');
const pageMix = [
  { tag: 'game_requests_list', path: '/game-requests/', htmx: false },
  { tag: 'game_requests_open', path: '/game-requests/?open_places=1', htmx: true },
  { tag: 'game_requests_search', path: '/game-requests/?q=mine', htmx: true },
  { tag: 'game_requests_refresh', path: '/game-requests/refresh', htmx: true },
  { tag: 'game_catalog', path: '/games/', htmx: false },
  { tag: 'game_catalog_search', path: '/games/?q=craft', htmx: true },
  { tag: 'followed_games', path: '/games/?followed=1', htmx: false },
  { tag: 'new_game_request', path: '/game-requests/new', htmx: false },
];

export default function () {
  // One virtual player owns one independent real LANsurfer session.
  const session = sessions[__VU - 1];
  const page = pageMix[Math.floor(Math.random() * pageMix.length)];
  const headers = { Accept: 'text/html' };
  if (page.htmx) headers['HX-Request'] = 'true';

  const result = http.get(BASE_URL + page.path, {
    headers,
    cookies: { lansurfer_session: { value: session.session_token, replace: true } },
    redirects: 0, // Login redirects must not masquerade as HTTP 200.
    timeout: '20s',
    tags: { endpoint: page.tag },
  });
  const ok = result.status === 200;
  unexpectedResponses.add(!ok, { endpoint: page.tag });
  check(result, { 'authenticated HTTP 200': (response) => response.status === 200 }, { endpoint: page.tag });
  sleep(0.7 + Math.random() * 2.3);
}
