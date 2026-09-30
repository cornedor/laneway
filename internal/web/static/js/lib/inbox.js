// Inbox state shared by the inbox view and the nav badge.
import { $, h } from './dom.js';
import { css } from './css.js';

export const latest = t => Date.parse(t.Entries[t.Entries.length - 1].When);

// Where a thread stands given its marks (Unix ms): unread, done until news, snoozed until then.
export function stateOf(t, mark, floor, now = Date.now()) {
  const mk = mark || {}, at = latest(t);
  return { at, unread: at > (mk.Read || floor), done: !!mk.Done && at <= mk.Done, snoozed: (mk.Snooze || 0) > now, till: mk.Snooze || 0 };
}
export function unreadCount(data, now = Date.now()) {
  let n = 0;
  for (const t of data.threads) { const s = stateOf(t, data.marks[t.Key], data.floor, now); if (s.unread && !s.done && !s.snoozed) n++; }
  return n;
}

export function setBadge(n) {
  const a = $('#nav a[data-name=inbox]'); if (!a) return;
  let b = a.querySelector('.nav-badge');
  if (!n) { if (b) b.remove(); return; }
  if (!b) { b = h('span.nav-badge'); a.append(b); }
  b.textContent = n > 99 ? '99+' : n;
  b.title = n + ' unread';
}

// Count unread threads now and every two minutes while the tab is visible.
export function installBadge(app) {
  css('inbox');
  let timer = 0;
  const poll = () => app.api.get('/inbox', { fresh: true }).then(d => setBadge(unreadCount(d))).catch(() => {});
  const arm = () => {
    clearInterval(timer); timer = 0;
    if (!document.hidden) { timer = setInterval(poll, 120000); }
  };
  document.addEventListener('visibilitychange', () => { arm(); if (!document.hidden) poll(); });
  arm(); poll();
}
