const notices = {
  join: new Set([
    'This Game Request is already full.',
    'You have already joined this Game Request.',
    'This Game Request is no longer open.',
  ]),
  leave: new Set([
    'You are not currently a member of this Game Request.',
    'The Host cannot leave their own Game Request.',
    'This Game Request is no longer open.',
  ]),
  chat: new Set([
    'You are not currently a member of this Game Request.',
    'You are sending messages too fast. Wait a moment and try again.',
    'Chat is closed for this Game Request.',
  ]),
};

// The CSRF middleware answers exactly this; any other 403 is a different refusal.
export function isCsrfRejection(response) {
  return response.status === 403 && (response.body || '').trim() === 'invalid CSRF token';
}

export function classifyHtmx(response, action, { plannedConflict = false } = {}) {
  if (action === 'csrf_negative') return isCsrfRejection(response) ? 'csrf_403' : 'unexpected';
  if (response.status === 409) return plannedConflict ? 'conflict' : 'unexpected';
  if (response.status === 429) return action === 'chat' ? 'conflict' : 'unexpected';
  if (response.status !== 200) return 'unexpected';
  const retarget = Object.entries(response.headers || {}).find(([key]) => key.toLowerCase() === 'hx-retarget')?.[1];
  if (retarget !== '#live-notices') return 'ok';
  return Array.from(notices[action] || []).some((notice) => (response.body || '').includes(notice)) ? 'conflict' : 'unexpected';
}

export async function waitForIdle(statsList, timeoutMs, quietMs = 1500) {
  const startedAt = Date.now();
  const deadline = startedAt + timeoutMs;
  for (const stats of statsList) stats.lastActivityAt ??= startedAt;
  while (true) {
    const idle = statsList.every((stats) => stats.active === 0);
    const lastActivityAt = Math.max(...statsList.map((stats) => stats.lastActivityAt));
    const now = Date.now();
    if (idle && now - lastActivityAt >= quietMs) return;
    if (now >= deadline) throw new Error('browser refresh requests did not settle');
    await new Promise((resolveWait) => setTimeout(resolveWait, 50));
  }
}

// Every reason a browser-calibration summary must not exit 0.
export function calibrationFailures(summary, browserClients) {
  const failures = [];
  if (summary.runnerExitCode !== 0) failures.push('SSE runner exited ' + summary.runnerExitCode);
  if (summary.idleError) failures.push(summary.idleError);
  if (summary.totals.failed > 0) failures.push(summary.totals.failed + ' refresh requests failed');
  if (summary.totals.convergedBrowsers !== browserClients) failures.push('only ' + summary.totals.convergedBrowsers + ' of ' + browserClients + ' browsers converged');
  if (summary.probe && summary.totals.eventToVisibleMs.missing > 0) failures.push(summary.totals.eventToVisibleMs.missing + ' event-to-visible samples missing');
  summary.browsers.forEach((item, index) => {
    if (Object.keys(item.statuses).some((status) => Number(status) !== 200)) failures.push('browser ' + index + ' saw non-200 refresh responses');
    if (item.active > 0) failures.push('browser ' + index + ' has ' + item.active + ' unfinished refresh requests');
    if (item.busy > 0 || item.disabledActions > 0) failures.push('browser ' + index + ' has residual loading/disabled state');
    if (item.htmx.before !== item.htmx.after) failures.push('browser ' + index + ' has unfinished HTMX requests');
  });
  return failures;
}
