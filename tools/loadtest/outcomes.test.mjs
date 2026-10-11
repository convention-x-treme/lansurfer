import test from 'node:test';
import assert from 'node:assert/strict';
import { calibrationFailures, classifyHtmx, isCsrfRejection, waitForIdle } from './outcomes.mjs';

test('classifies only expected action notices and statuses', () => {
  assert.equal(classifyHtmx({ status: 200, headers: { 'HX-Retarget': '#live-notices' }, body: 'This Game Request is already full.' }, 'join'), 'conflict');
  assert.equal(classifyHtmx({ status: 409 }, 'join', { plannedConflict: true }), 'conflict');
  assert.equal(classifyHtmx({ status: 429 }, 'chat'), 'conflict');
  for (const result of [
    { status: 200, headers: { 'HX-Retarget': '#live-notices' }, body: 'Unknown notice' },
    { status: 403 }, { status: 404 }, { status: 422 }, { status: 500 }, { status: 302 },
    { status: 409 }, { status: 429 },
  ]) assert.equal(classifyHtmx(result, 'join'), 'unexpected');
  assert.equal(classifyHtmx({ status: 403, body: 'invalid CSRF token' }, 'csrf_negative'), 'csrf_403');
});

test('classifies action notices inside rendered HTML fragments', () => {
  assert.equal(classifyHtmx({
    status: 200,
    headers: { 'HX-Retarget': '#live-notices' },
    body: '<div><span>You have already joined this Game Request.</span></div>',
  }, 'join'), 'conflict');
});

test('accepts only the CSRF middleware rejection as the negative-path success', () => {
  assert.equal(classifyHtmx({ status: 403, body: 'invalid CSRF token\n' }, 'csrf_negative'), 'csrf_403');
  for (const result of [
    { status: 403 }, { status: 403, body: 'Forbidden' }, { status: 403, body: 'You may not join this Game Request.' },
    { status: 200, body: 'invalid CSRF token' }, { status: 303 },
  ]) assert.equal(classifyHtmx(result, 'csrf_negative'), 'unexpected');
  assert.equal(isCsrfRejection({ status: 403, body: 'invalid CSRF token' }), true);
  assert.equal(isCsrfRejection({ status: 403, body: 'forbidden' }), false);
});

const cleanBrowser = { statuses: { 200: 3 }, active: 0, busy: 0, disabledActions: 0, htmx: { before: 3, after: 3 } };
const cleanSummary = (browsers = [cleanBrowser], extra = {}) => ({ runnerExitCode: 0, idleError: '', totals: { failed: 0, convergedBrowsers: browsers.length }, browsers, ...extra });

test('a hung refresh fails the calibration even when the visible state matches', async () => {
  const hung = { active: 1 };
  await assert.rejects(waitForIdle([{ active: 0 }, hung], 120), /did not settle/);
  const failures = calibrationFailures(cleanSummary([{ ...cleanBrowser, active: 1 }], { idleError: 'browser refresh requests did not settle' }), 1);
  assert.deepEqual(failures, ['browser refresh requests did not settle', 'browser 0 has 1 unfinished refresh requests']);
});

test('residual loading, disabled or unfinished HTMX state fails the calibration', () => {
  for (const stuck of [{ busy: 1 }, { disabledActions: 1 }, { htmx: { before: 4, after: 3 } }, { statuses: { 200: 2, 0: 1 } }]) {
    assert.equal(calibrationFailures(cleanSummary([cleanBrowser, { ...cleanBrowser, ...stuck }]), 2).length, 1, JSON.stringify(stuck));
  }
  assert.equal(calibrationFailures(cleanSummary([cleanBrowser], { runnerExitCode: 1 }), 1).length, 1);
  assert.equal(calibrationFailures(cleanSummary([cleanBrowser]), 2).length, 1);
});

test('a fully settled calibration passes', async () => {
  await waitForIdle([{ active: 0 }], 50);
  assert.deepEqual(calibrationFailures(cleanSummary([cleanBrowser, cleanBrowser]), 2), []);
});
