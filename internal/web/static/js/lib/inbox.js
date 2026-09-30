// Inbox state shared by the inbox view and the nav badge.
import { $, h } from './dom.js';
import { css } from './css.js';
import { notify, enabled } from './notify.js';

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

// Ping for threads that turned unread since the last look, while the tab is hidden.
let seen = null;
function pingNew(app, data, now = Date.now()) {
  const cur = new Map();
  for (const t of data.threads) { const s = stateOf(t, data.marks[t.Key], data.floor, now); if (s.unread && !s.done && !s.snoozed) cur.set(t.Key, s.at); }
  const fresh = seen && document.hidden ? [...cur].filter(([k, at]) => !seen.has(k) || seen.get(k) < at) : [];
  seen = cur;
  if (!fresh.length || !enabled()) return;
  const t = data.threads.find(x => x.Key === fresh[0][0]), e = t.Entries[t.Entries.length - 1];
  const title = fresh.length === 1 ? t.Key + ' ' + t.Summary : fresh.length + ' issues have news';
  notify(title, fresh.length === 1 ? (e.Who ? e.Who + ': ' : '') + (e.Body || e.What || 'updated') : fresh.map(([k]) => k).slice(0, 5).join(', '),
    () => (fresh.length === 1 ? app.panel.open(t.Key) : app.go('/inbox')), 'inbox');
}

// Count unread threads now and every two minutes while the tab is visible.
export function installBadge(app) {
  css('inbox');
  let timer = 0;
  const poll = () => app.api.get('/inbox', { fresh: true }).then(d => { setBadge(unreadCount(d)); pingNew(app, d); }).catch(() => {});
  const arm = () => {
    clearInterval(timer); timer = 0;
    if (!document.hidden) { timer = setInterval(poll, 120000); } else if (enabled()) { timer = setInterval(poll, 300000); }
  };
  document.addEventListener('visibilitychange', () => { arm(); if (!document.hidden) poll(); });
  arm(); poll();
}
