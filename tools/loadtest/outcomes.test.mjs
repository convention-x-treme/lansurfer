import test from 'node:test';
import assert from 'node:assert/strict';
import { classifyHtmx } from './outcomes.mjs';

test('classifies only expected action notices and statuses', () => {
  assert.equal(classifyHtmx({ status: 200, headers: { 'HX-Retarget': '#live-notices' }, body: 'This Game Request is already full.' }, 'join'), 'conflict');
  assert.equal(classifyHtmx({ status: 409 }, 'join', { plannedConflict: true }), 'conflict');
  assert.equal(classifyHtmx({ status: 429 }, 'chat'), 'conflict');
  for (const result of [
    { status: 200, headers: { 'HX-Retarget': '#live-notices' }, body: 'Unknown notice' },
    { status: 403 }, { status: 404 }, { status: 422 }, { status: 500 }, { status: 302 },
    { status: 409 }, { status: 429 },
  ]) assert.equal(classifyHtmx(result, 'join'), 'unexpected');
  assert.equal(classifyHtmx({ status: 403 }, 'csrf_negative'), 'csrf_403');
});

test('classifies action notices inside rendered HTML fragments', () => {
  assert.equal(classifyHtmx({
    status: 200,
    headers: { 'HX-Retarget': '#live-notices' },
    body: '<div><span>You have already joined this Game Request.</span></div>',
  }, 'join'), 'conflict');
});
