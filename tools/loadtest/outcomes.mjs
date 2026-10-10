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

export function classifyHtmx(response, action, { plannedConflict = false } = {}) {
  if (action === 'csrf_negative') return response.status === 403 ? 'csrf_403' : 'unexpected';
  if (response.status === 409) return plannedConflict ? 'conflict' : 'unexpected';
  if (response.status === 429) return action === 'chat' ? 'conflict' : 'unexpected';
  if (response.status !== 200) return 'unexpected';
  const retarget = Object.entries(response.headers || {}).find(([key]) => key.toLowerCase() === 'hx-retarget')?.[1];
  if (retarget !== '#live-notices') return 'ok';
  return Array.from(notices[action] || []).some((notice) => (response.body || '').includes(notice)) ? 'conflict' : 'unexpected';
}
