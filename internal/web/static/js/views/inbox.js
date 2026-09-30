// Inbox: what others did on my issues, one thread per issue with its own read / done / snoozed state.
import { h, clear, delegate, debounce } from '../lib/dom.js';
import { css } from '../lib/css.js';
import { vlist } from '../lib/vlist.js';
import { ago, dateTime } from '../lib/fmt.js';
import { render as md } from '../lib/md.js';
import { stateOf, latest, unreadCount, setBadge } from '../lib/inbox.js';
import { dayStart, addDays, workdays } from '../lib/worktime.js';

export default function mount(el, { app, scope, toolbar }) {
  css('inbox');
  const { api, ui } = app;
  let data = { threads: [], marks: {}, floor: 0 };
  let tab = app.prefs.get('inbox_tab', 'inbox'), rows = [], sel = 0, list = null, loaded = false, dead = false;
  let newFrom = 0; // entries after this were unread when the thread was selected
  let shown = '', hold = ''; // the detail's signature; a thread kept unread by u

  const listEl = h('div.inbox-scroll'), detail = h('div.inbox-detail'), head = h('div.ihead'), empty = h('div.empty', { hidden: true });
  el.append(h('div.inbox', h('div.inbox-list', head, listEl, empty), detail));

  const now = () => Date.now();
  const st = t => stateOf(t, data.marks[t.Key], data.floor, now());
  const mentioned = t => t.Entries.some(e => (e.Mention || e.Assigned) && Date.parse(e.When) > (data.marks[t.Key]?.Read || data.floor));
  const cur = () => rows[sel] || null;

  function buildRows() {
    const keep = cur() && cur().Key;
    const n = now();
    rows = data.threads.filter(t => { const s = st(t); return tab === 'all' || (!s.done && !s.snoozed); });
    rows = rows.map((t, i) => ({ t, i })).sort((a, b) => (mentioned(b.t) - mentioned(a.t)) || (a.i - b.i)).map(x => x.t);
    if (keep) { const i = rows.findIndex(t => t.Key === keep); sel = i >= 0 ? i : Math.min(sel, rows.length - 1); }
    sel = Math.max(0, Math.min(sel, rows.length - 1));
    const hidden = data.threads.length - rows.length;
    clear(head).append(h('div.itabs', [['inbox', 'Inbox'], ['all', 'All']].map(([k, name]) => h('button.btn' + (tab === k ? '.on' : '.ghost'), { onclick: () => setTab(k) }, name))),
      h('span.spacer'), tab === 'inbox' && hidden > 0 ? hidden + ' done or snoozed' : '', h('span', unreadCount(data, n) + ' unread'));
    setBadge(unreadCount(data, n));
    list.setCount(rows.length);
    list.refresh();
    empty.hidden = rows.length > 0;
    empty.textContent = !loaded ? 'Loading…' : tab === 'inbox' && data.threads.length ? 'Inbox zero. Tab shows the rest.' : 'Nothing new on your issues.';
    if (rows.length) list.scrollTo(sel);
    showThread(false);
  }

  function bindRow(row, i) {
    const t = rows[i]; if (!t) return;
    const s = st(t), last = t.Entries[t.Entries.length - 1];
    row.className = 'vl-row irow' + (i === sel ? ' sel' : '') + (s.unread ? ' unread' : ' read');
    row.dataset.i = i;
    row.replaceChildren(...[h('span.dot'), h('span.wkey.mono', t.Key), h('span.wsum', t.Summary),
      mentioned(t) && h('span.mention', { title: 'Mentions or assigns you' }, '@'),
      s.done && h('span.chip', 'done'), s.snoozed && h('span.chip', { title: 'Until ' + dateTime(new Date(s.till)) }, 'snoozed'),
      h('span.when', ago(last.When))].filter(Boolean));
  }

  // ---- detail
  let readTimer = 0;
  function showThread(force) {
    const t = cur();
    const s = t && st(t), sig = t ? t.Key + ':' + latest(t) + ':' + s.unread : '';
    if (!force && sig === shown) return;
    shown = sig;
    clearTimeout(readTimer);
    clear(detail);
    if (!t) return;
    if (force || !newFrom) newFrom = s.unread ? (data.marks[t.Key]?.Read || data.floor) : Infinity;
    detail.append(h('div.ithread',
      h('h2', h('a', { href: '#/issue/' + t.Key, onclick: e => { e.preventDefault(); app.panel.open(t.Key); } }, t.Key), ' ', t.Summary),
      h('div.sub', ui.statusPill(t.Status, /done|closed|resolved/i.test(t.Status) ? 'done' : 'indeterminate'), t.Assignee && h('span', t.Assignee), h('span', t.Entries.length + (t.Entries.length === 1 ? ' update' : ' updates'))),
      t.Entries.map(e => entry(e))));
    if (s.unread && hold !== t.Key) readTimer = setTimeout(() => mark(t, { Read: latest(t) }, true), 700);
  }
  function entry(e) {
    const when = Date.parse(e.When);
    return h('div.ientry' + (when > newFrom ? '.new' : '') + (e.Mention || e.Assigned ? '.mention' : ''),
      h('div.meta', h('span.who', e.Who), h('span', { title: dateTime(e.When) }, ago(e.When)), e.Mention && h('span', '@ mentions you'), e.Assigned && h('span', 'assigned you')),
      e.CommentID ? h('div.body', md(e.Body, { isKey: k => /^[A-Z][A-Z0-9]+-\d+$/.test(k), onKey: k => app.panel.open(k) }))
        : e.Changes && e.Changes.length ? e.Changes.map(c => h('div.chg', h('span.f', c.Field + ': '), c.From && h('s', clip(c.From) + ' '), c.From && '→ ', clip(c.To)))
          : h('div.chg', e.What));
  }
  const clip = s => (s && s.length > 160 ? s.slice(0, 160) + '…' : s || '—');

  // ---- marks
  function mark(t, patch, quiet) {
    const m = { ...(data.marks[t.Key] || {}), ...patch };
    data.marks[t.Key] = m;
    api.put('/inbox/state/' + t.Key, { Read: m.Read || 0, Done: m.Done || 0, Snooze: m.Snooze || 0 }).catch(e => { ui.errToast(e); load(true); });
    if (quiet) { setBadge(unreadCount(data)); list.refresh(); const f = head.lastChild; if (f) f.textContent = unreadCount(data) + ' unread'; } else buildRows();
  }
  function done() {
    const t = cur(); if (!t) return;
    const s = st(t);
    if (s.done) { mark(t, { Done: 0 }); ui.toast(t.Key + ' back in the inbox'); return; }
    mark(t, { Done: latest(t), Snooze: 0, Read: Math.max(data.marks[t.Key]?.Read || 0, latest(t)) });
    ui.toast(t.Key + ' done until something new happens', { action: { label: 'Undo', run: () => mark(t, { Done: 0 }) } });
  }
  async function snooze() {
    const t = cur(); if (!t) return;
    if (st(t).snoozed) { mark(t, { Snooze: 0 }); ui.toast(t.Key + ' back in the inbox'); return; }
    const n = new Date(), wd = workdays(app);
    let next = addDays(new Date(n.getFullYear(), n.getMonth(), n.getDate()), 1);
    while (!wd.includes(next.getDay())) next = addDays(next, 1);
    let week = addDays(new Date(n.getFullYear(), n.getMonth(), n.getDate()), 1);
    while (week.getDay() !== 1) week = addDays(week, 1);
    const opts = [{ name: 'In 1 hour', at: n.getTime() + 3600e3 }, { name: 'Tomorrow', at: +dayStart(app, next), sub: next }, { name: 'Next week', at: +dayStart(app, week), sub: week }];
    const o = await ui.pick({ title: 'Snooze ' + t.Key, items: opts, label: o => o.name, detail: o => new Date(o.at).toLocaleString(undefined, { weekday: 'short', hour: '2-digit', minute: '2-digit' }) });
    if (!o) return;
    mark(t, { Snooze: o.at });
    ui.toast(t.Key + ' snoozed until ' + dateTime(new Date(o.at)));
  }
  function unread() {
    const t = cur(); if (!t) return;
    hold = t.Key; newFrom = 0;
    mark(t, { Read: latest(t) - 1 });
    ui.toast(t.Key + ' unread');
  }
  function toggleRead() {
    const t = cur(); if (!t) return;
    if (st(t).unread) mark(t, { Read: latest(t) }, true); else mark(t, { Read: latest(t) - 1 }, true);
    clearTimeout(readTimer);
  }
  function doneRead() {
    const ts = rows.filter(t => { const s = st(t); return !s.unread && !s.done; });
    if (!ts.length) return ui.toast('No read threads to clear');
    for (const t of ts) { data.marks[t.Key] = { ...(data.marks[t.Key] || {}), Done: latest(t) }; api.put('/inbox/state/' + t.Key, { Read: data.marks[t.Key].Read || 0, Done: latest(t), Snooze: data.marks[t.Key].Snooze || 0 }).catch(ui.errToast); }
    ui.toast(ts.length + ' read threads done');
    buildRows();
  }

  function setTab(t) { tab = t; app.prefs.set('inbox_tab', t); sel = 0; buildRows(); }
  function select(i) {
    if (i < 0 || i >= rows.length || i === sel) return;
    const old = sel; sel = i; hold = '';
    list.refresh(old); list.refresh(sel); list.scrollTo(sel);
    showThread(true);
  }
  function open() { const t = cur(); if (t) { clearTimeout(readTimer); mark(t, { Read: latest(t) }, true); app.panel.open(t.Key); } }

  function load(fresh) {
    const p = fresh ? api.get('/inbox', { fresh: true }).then(d => { data = d; loaded = true; buildRows(); }) : api.swr('/inbox?days=7', d => { data = d; loaded = true; buildRows(); }, { persist: false });
    return p.catch(e => { loaded = true; buildRows(); ui.errToast(e); });
  }
  const reload = debounce(() => { if (!dead && !document.hidden) load(true); }, 300);

  list = vlist(listEl, { count: 0, rowHeight: Math.max(34, parseFloat(getComputedStyle(document.documentElement).getPropertyValue('--row')) || 32), create: () => h('div'), bind: bindRow });
  delegate(listEl, 'click', '.irow', (e, row) => select(+row.dataset.i));
  delegate(listEl, 'dblclick', '.irow', open);

  const G = { group: 'Inbox' };
  scope.bind(['j', 'ArrowDown'], () => select(sel + 1), 'next thread', G);
  scope.bind(['k', 'ArrowUp'], () => select(sel - 1), 'previous thread', G);
  scope.bind('Enter', open, 'open issue', G);
  scope.bind('e', done, 'done until news / back to inbox', G);
  scope.bind('E', doneRead, 'all read threads done', G);
  scope.bind('s', snooze, 'snooze / unsnooze', G);
  scope.bind('u', unread, 'mark unread', G);
  scope.bind('a', toggleRead, 'toggle read', G);
  scope.bind('r', () => { load(true); ui.toast('Refreshing'); }, 'refresh', G);
  scope.bind('Tab', () => setTab(tab === 'inbox' ? 'all' : 'inbox'), 'inbox / all', { ...G, when: () => !app.panel.key });
  scope.bind('A', () => setTab(tab === 'inbox' ? 'all' : 'inbox'), 'inbox / all', G);
  scope.bind('o', () => { const t = cur(); if (t && app.session.baseURL) window.open(app.session.baseURL.replace(/\/$/, '') + '/browse/' + t.Key, '_blank', 'noopener'); }, 'open in Jira', G);

  const off = [app.bus.on('focus', reload)];
  clear(toolbar).append(h('h3.dim', 'Inbox'));
  load(false);
  return () => { dead = true; clearTimeout(readTimer); list.destroy(); off.forEach(f => f()); };
}
