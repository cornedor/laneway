// Inbox: what others did on my issues on every configured site, one thread per issue with its own read / done / snoozed state.
import { h, clear, delegate, debounce } from '../lib/dom.js';
import { css } from '../lib/css.js';
import { vlist } from '../lib/vlist.js';
import { rowPx, px14, onChange as onMetrics } from '../lib/metrics.js';
import { ago, dateTime } from '../lib/fmt.js';
import { render as md } from '../lib/md.js';
import { mdEdit } from '../lib/mdedit.js';
import { stateOf, latest, unreadCount, setBadge } from '../lib/inbox.js';
import { dayStart, addDays, workdays } from '../lib/worktime.js';
import { delightOn } from '../lib/delight.js';

export default function mount(el, { app, scope, toolbar }) {
  css('inbox');
  const { api, ui } = app;
  let data = { threads: [], marks: {}, floor: 0 };
  let tab = app.prefs.get('inbox_tab', 'inbox'), rows = [], sel = 0, list = null, loaded = false, dead = false;
  let newFrom = 0; // entries after this were unread when the thread was selected
  let shown = '', hold = ''; // the detail's signature; a thread kept unread by u

  const listEl = h('div.inbox-scroll'), detail = h('div.inbox-detail'), empty = h('div.empty', { hidden: true });
  el.append(h('div.inbox', h('div.inbox-list', listEl, empty), detail));

  const now = () => Date.now();
  const st = t => stateOf(t, data.marks[t.ID], data.floor, now());
  const TABS = [['inbox', 'Inbox'], ['mentions', 'Mentions'], ['all', 'All']];
  const everMentioned = t => t.Entries.some(e => e.Mention || e.Assigned);
  const mentioned = t => t.Entries.some(e => (e.Mention || e.Assigned) && Date.parse(e.When) > (data.marks[t.ID]?.Read || data.floor));
  const away = t => t.Site !== data.site; // another site's: opens in Jira
  const siteName = t => t.Site || app.session.defaultName || 'jira';
  const cur = () => rows[sel] || null;
  // As the TUI: news for you first, then the unread, the read, the done or snoozed; newest first in each (the server's order).
  const rank = t => { const s = st(t); return s.done || s.snoozed ? 3 : s.unread && mentioned(t) ? 0 : s.unread ? 1 : 2; };

  function buildRows() {
    const keep = cur() && cur().ID;
    const n = now();
    rows = data.threads.filter(t => { const s = st(t); return tab === 'all' || (tab === 'mentions' ? !s.done && everMentioned(t) : !s.done && !s.snoozed); });
    rows = rows.map((t, i) => ({ t, i, r: rank(t) })).sort((a, b) => (a.r - b.r) || (a.i - b.i)).map(x => x.t);
    if (keep) { const i = rows.findIndex(t => t.ID === keep); sel = i >= 0 ? i : Math.min(sel, rows.length - 1); }
    sel = Math.max(0, Math.min(sel, rows.length - 1));
    const hidden = data.threads.length - rows.length;
    const counts = { m: data.threads.filter(t => !st(t).done && everMentioned(t)).length };
    clear(toolbar).append(h('div.seg', TABS.map(([k, name]) => h('button.btn' + (tab === k ? '.on' : ''), { onclick: () => setTab(k) }, name, k === 'mentions' && counts.m ? ' ' + counts.m : ''))),
      h('span.spacer'), h('span.faint', (tab !== 'all' && hidden > 0 ? hidden + ' done or snoozed · ' : '') + unreadCount(data, n) + ' unread'));
    setBadge(unreadCount(data, n));
    list.setCount(rows.length);
    list.refresh();
    empty.hidden = rows.length > 0;
    empty.textContent = !loaded ? 'Loading…' : tab === 'mentions' ? 'No one mentioned you.' : tab === 'inbox' && data.threads.length ? 'Inbox zero' + (delightOn(app) ? ', all caught up ✓' : '') + '. Tab shows the rest.' : 'Nothing new on your issues.';
    if (rows.length) list.scrollTo(sel);
    showThread(false);
  }

  function bindRow(row, i) {
    const t = rows[i]; if (!t) return;
    const s = st(t), last = t.Entries[t.Entries.length - 1];
    row.className = 'vl-row irow' + (i === sel ? ' sel' : '') + (s.unread ? ' unread' : ' read');
    row.dataset.i = i; row.dataset.key = t.Key;
    row.replaceChildren(...[h('span.dot'), away(t) && h('span.chip', { title: 'On ' + siteName(t) + ': opens in Jira' }, siteName(t)), h('span.wkey.mono', t.Key), h('span.wsum', t.Summary),
      mentioned(t) && h('span.mention', { title: 'Mentions or assigns you' }, '@'),
      s.done && h('span.chip', 'done'), s.snoozed && h('span.chip', { title: 'Until ' + dateTime(new Date(s.till)) }, 'snoozed'),
      h('span.when', ago(last.When))].filter(Boolean));
  }

  // ---- detail
  let readTimer = 0;
  function showThread(force) {
    const t = cur();
    const s = t && st(t), sig = t ? t.ID + ':' + latest(t) + ':' + s.unread : '';
    if (!force && sig === shown) return;
    shown = sig;
    clearTimeout(readTimer);
    clear(detail);
    if (!t) return;
    if (force || !newFrom) newFrom = s.unread ? (data.marks[t.ID]?.Read || data.floor) : Infinity;
    detail.append(h('div.ithread',
      h('h2', away(t) ? h('a', { href: t.URL, target: '_blank', rel: 'noopener' }, t.Key) : h('a', { href: '#/issue/' + t.Key, onclick: e => { e.preventDefault(); app.panel.open(t.Key); } }, t.Key), ' ', t.Summary),
      h('div.sub', ui.statusPill(t.Status, /done|closed|resolved/i.test(t.Status) ? 'done' : 'indeterminate'), t.Assignee && h('span', t.Assignee), away(t) && h('span', 'on ' + siteName(t)), h('span', t.Entries.length + (t.Entries.length === 1 ? ' update' : ' updates'))),
      t.Entries.map(e => entry(e))));
    if (s.unread && hold !== t.ID) readTimer = setTimeout(() => mark(t, { Read: latest(t) }, true), 700);
  }
  function entry(e) {
    const when = Date.parse(e.When);
    return h('div.ientry' + (when > newFrom ? '.new' : '') + (e.Mention || e.Assigned ? '.mention' : ''),
      h('div.meta', h('span.who', e.Who), h('span', { title: dateTime(e.When) }, ago(e.When)), e.Mention && h('span', '@ mentions you'), e.Assigned && h('span', 'assigned you')),
      e.CommentID ? h('div.body', md(e.Body, away(cur()) ? {} : { isKey: k => /^[A-Z][A-Z0-9]+-\d+$/.test(k), onKey: k => app.panel.open(k) }))
        : e.Changes && e.Changes.length ? e.Changes.map(c => h('div.chg', h('span.f', c.Field + ': '), c.From && h('s', clip(c.From) + ' '), c.From && '→ ', clip(c.To)))
          : h('div.chg', e.What));
  }
  const clip = s => (s && s.length > 160 ? s.slice(0, 160) + '…' : s || '—');

  // ---- marks
  const put = (t, m) => api.put('/inbox/state/' + t.Key, { Site: t.Site, Read: m.Read || 0, Done: m.Done || 0, Snooze: m.Snooze || 0 });
  function mark(t, patch, quiet) {
    const m = { ...(data.marks[t.ID] || {}), ...patch };
    data.marks[t.ID] = m;
    put(t, m).catch(e => { ui.errToast(e); load(true); });
    if (quiet) { setBadge(unreadCount(data)); list.refresh(); const f = toolbar.lastChild; if (f) f.textContent = unreadCount(data) + ' unread'; } else buildRows();
  }
  function done() {
    const t = cur(); if (!t) return;
    const s = st(t);
    if (s.done) { mark(t, { Done: 0 }); ui.toast(t.Key + ' back in the inbox'); return; }
    mark(t, { Done: latest(t), Snooze: 0, Read: Math.max(data.marks[t.ID]?.Read || 0, latest(t)) });
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
    hold = t.ID; newFrom = 0;
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
    for (const t of ts) { data.marks[t.ID] = { ...(data.marks[t.ID] || {}), Done: latest(t) }; put(t, data.marks[t.ID]).catch(ui.errToast); }
    ui.toast(ts.length + ' read threads done');
    buildRows();
  }

  // Comment or reply from the thread, without leaving the inbox.
  function compose(reply) {
    const t = cur(); if (!t) return;
    if (away(t)) return ui.toast('On ' + siteName(t) + ': o opens it in Jira');
    let last = null;
    if (reply) {
      last = [...t.Entries].reverse().find(e => e.CommentID);
      if (!last) return ui.toast('No comments to reply to');
    }
    const at = last && last.Who ? '@' + last.Who + ' ' : '';
    const ment = last && last.WhoID ? [{ AccountID: last.WhoID, DisplayName: last.Who }] : [];
    const ed = mdEdit(app, { value: at, rows: 3, issueKey: t.Key, label: reply ? 'Reply' : 'Comment', placeholder: 'Comment on ' + t.Key + '… (@ mentions, / formats)',
      save: async (text, mentions) => {
        await api.post('/issues/' + t.Key + '/comments', { Markdown: text, Mentions: [...ment.filter(m => text.includes('@' + m.DisplayName)), ...mentions] });
        m.close(); ui.toast('Commented on ' + t.Key, { kind: 'ok' }); app.bus.emit('issue:changed', { key: t.Key }); load(true);
      } });
    const m = ui.modal(h('div.prompt', ed.el), { title: (reply ? 'Reply on ' : 'Comment on ') + t.Key + '  ' + t.Summary, wide: true, onClose: () => ed.dispose() });
    m.scope.bind('ctrl+Enter', () => ed.el._save(), '', { input: true, hidden: true });
    ed.focus();
  }

  function setTab(t) { tab = t; app.prefs.set('inbox_tab', t); sel = 0; buildRows(); }
  function select(i) {
    if (i < 0 || i >= rows.length || i === sel) return;
    const old = sel; sel = i; hold = '';
    list.refresh(old); list.refresh(sel); list.scrollTo(sel);
    showThread(true);
  }
  const browse = t => window.open(away(t) ? t.URL : app.session.baseURL.replace(/\/$/, '') + '/browse/' + t.Key, '_blank', 'noopener');
  function open() { const t = cur(); if (t) { clearTimeout(readTimer); mark(t, { Read: latest(t) }, true); if (away(t)) browse(t); else app.panel.open(t.Key); } }

  function load(fresh) {
    const p = fresh ? api.get('/inbox', { fresh: true }).then(d => { data = d; loaded = true; buildRows(); }) : api.swr('/inbox', d => { data = d; loaded = true; buildRows(); }, { persist: false });
    return p.catch(e => { loaded = true; buildRows(); ui.errToast(e); });
  }
  const reload = debounce(() => { if (!dead && !document.hidden) load(true); }, 300);

  list = vlist(listEl, { count: 0, rowHeight: Math.max(px14(34), rowPx()), create: () => h('div'), bind: bindRow });
  delegate(listEl, 'click', '.irow', (e, row) => select(+row.dataset.i));
  delegate(listEl, 'dblclick', '.irow', open);

  const G = { group: 'Inbox' };
  scope.bind(['j', 'ArrowDown'], () => select(sel + 1), 'next thread', G);
  scope.bind(['k', 'ArrowUp'], () => select(sel - 1), 'previous thread', G);
  const to = i => select(Math.min(Math.max(i, 0), rows.length - 1));
  const page = () => Math.max(1, Math.floor(listEl.clientHeight / Math.max(px14(34), rowPx())) - 1);
  scope.bind('Home', () => to(0), 'first thread', { ...G, hidden: true });
  scope.bind('End', () => to(rows.length - 1), 'last thread', { ...G, hidden: true });
  scope.bind('PageDown', () => to(sel + page()), 'page down', { ...G, hidden: true });
  scope.bind('PageUp', () => to(sel - page()), 'page up', { ...G, hidden: true });
  scope.bind('Enter', open, 'open issue', { ...G, bar: 'open' });
  scope.bind('e', done, 'done until news / back to inbox', { ...G, bar: 'done' });
  scope.bind('E', doneRead, 'all read threads done', G);
  scope.bind('s', snooze, 'snooze / unsnooze', { ...G, bar: 'snooze' });
  scope.bind('u', unread, 'mark unread', G);
  scope.bind('a', toggleRead, 'toggle read', G);
  scope.bind('r', () => { load(true); ui.toast('Refreshing'); }, 'refresh', G);
  const cycle = d => setTab(TABS[(TABS.findIndex(x => x[0] === tab) + d + TABS.length) % TABS.length][0]);
  scope.bind('Tab', () => cycle(1), 'next tab: inbox, mentions, all', { ...G, when: () => !app.panel.key });
  scope.bind('A', () => cycle(1), 'next tab: inbox, mentions, all', { ...G, bar: 'tab' });
  scope.bind(['1', '2', '3'], e => setTab(TABS[+e.key - 1][0]), 'inbox / mentions / all', G);
  scope.bind('c', () => compose(false), 'comment on the issue', { ...G, bar: 'comment' });
  scope.bind('R', () => compose(true), 'reply to the latest comment', { ...G, bar: 'reply' });
  scope.bind('y', () => { const t = cur(); if (t) navigator.clipboard.writeText(t.Key).then(() => ui.toast(t.Key + ' copied')).catch(() => {}); }, 'copy key', G);
  scope.bind('o', () => { const t = cur(); if (t && (away(t) || app.session.baseURL)) browse(t); }, 'open in Jira', G);

  const off = [app.bus.on('focus', reload), onMetrics(() => list.setRowHeight(Math.max(px14(34), rowPx())))];
  buildRows(); // the tabs and Loading… now, so the view bar does not wait for the data
  load(false);
  return () => { dead = true; clearTimeout(readTimer); list.destroy(); off.forEach(f => f()); };
}
