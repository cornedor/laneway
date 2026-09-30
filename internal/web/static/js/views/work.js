// My work: what's assigned to me, and the time I logged (day and week).
import { h, clear, delegate, debounce } from '../lib/dom.js';
import { css } from '../lib/css.js';
import { vlist } from '../lib/vlist.js';
import { isZero, date as toDate, shortDate, duration } from '../lib/fmt.js';
import { ymd, addDays, weekStart, hm, dayStart, targetSeconds, workdays, logDialog } from '../lib/worktime.js';

const KEY_RE = /^[A-Z][A-Z0-9]+-\d+$/;
const today = () => { const n = new Date(); return new Date(n.getFullYear(), n.getMonth(), n.getDate()); };
const catOf = c => (c.Done ? 'done' : c.InProgress ? 'indeterminate' : 'new');
const rank = c => (c.Done ? 2 : c.InProgress ? 1 : 0);
const kids = a => a.flat().filter(Boolean);
const sum = ws => ws.reduce((n, w) => n + w.Seconds, 0);

export default function mount(el, { app, scope, toolbar, query }) {
  css('work');
  const { api, ui, bus, prefs } = app;
  let tab = ['issues', 'day', 'week'].includes(query.tab) ? query.tab : prefs.get('work_tab', 'issues');
  if (!['issues', 'day', 'week'].includes(tab)) tab = 'issues';
  let cards = [], loaded = false, filter = '', group = prefs.get('work_group', 'status'), hideDone = prefs.get('work_hide_done', 'false') === 'true';
  let rows = [], sel = -1, list = null, empty = null;
  let day = today(), logs = [], logsPath = '', wsel = 0, cell = { row: 0, col: 0 }, weekRows = [];
  const extra = new Set();
  let dead = false;

  const root = h('div.work');
  el.append(root);
  const body = h('div.work-body');
  const inTab = (...t) => ({ when: () => t.includes(tab) });

  function setTab(t) {
    if (t === tab) return;
    tab = t; prefs.set('work_tab', t); app.setQuery({ tab: t });
    paintToolbar(); render();
  }

  // ---- toolbar
  const filterIn = h('input.input.work-filter', { type: 'search', placeholder: 'Filter (f)', spellcheck: false, oninput: debounce(() => { filter = filterIn.value.trim().toLowerCase(); buildRows(); }, 80) });
  filterIn.addEventListener('keydown', e => { if (e.key === 'Escape' || e.key === 'Enter') { e.stopPropagation(); filterIn.blur(); } });
  const groupSel = h('select.input.work-group', { title: 'Group by (v)', onchange: () => { group = groupSel.value; prefs.set('work_group', group); buildRows(); } },
    ['status', 'project', 'none'].map(g => h('option', { value: g, selected: g === group }, g === 'none' ? 'No grouping' : 'By ' + g)));
  const doneBtn = h('button.btn.ghost', { title: 'Hide done (d)', onclick: () => toggleDone() });
  function paintToolbar() {
    clear(toolbar).append(...kids([
      h('div.seg', [['issues', 'Issues', '1'], ['day', 'Day', '2'], ['week', 'Week', '3']].map(([t, n, k]) => h('button.btn' + (tab === t ? '.on' : ''), { onclick: () => setTab(t), title: k }, n))),
      tab === 'issues' && [filterIn, groupSel, doneBtn], h('span.spacer')]));
    doneBtn.textContent = hideDone ? 'Done hidden' : 'Hide done';
    doneBtn.classList.toggle('on', hideDone);
  }
  function toggleDone() { hideDone = !hideDone; prefs.set('work_hide_done', hideDone); paintToolbar(); buildRows(); }

  // ---- issues
  function buildRows() {
    const shown = cards.filter(c => (!hideDone || !c.Done) && (!filter || (c.Key + ' ' + c.Summary + ' ' + c.Status).toLowerCase().includes(filter)));
    const by = new Map();
    for (const c of shown) {
      const g = group === 'status' ? c.Status : group === 'project' ? c.Key.split('-')[0] : '';
      (by.get(g) || by.set(g, []).get(g)).push(c);
    }
    const order = [...by.keys()];
    if (group === 'status') order.sort((a, b) => rank(by.get(a)[0]) - rank(by.get(b)[0]) || a.localeCompare(b));
    else order.sort();
    const keep = rows[sel] && rows[sel].card ? rows[sel].card.Key : null;
    rows = [];
    for (const g of order) {
      if (group !== 'none') rows.push({ head: g, count: by.get(g).length });
      for (const c of by.get(g)) rows.push({ card: c });
    }
    sel = keep ? rows.findIndex(r => r.card && r.card.Key === keep) : -1;
    if (sel < 0) sel = rows.findIndex(r => r.card);
    paintList();
  }
  const rowHeight = () => parseFloat(getComputedStyle(document.documentElement).getPropertyValue('--row')) || 32;
  function paintList() {
    if (!list) return;
    list.setCount(rows.length);
    empty.hidden = rows.length > 0;
    empty.textContent = cards.length ? 'Nothing matches' : loaded ? 'Nothing assigned to you' : 'Loading…';
    if (sel >= 0) list.scrollTo(sel);
  }
  function bindRow(elRow, i) {
    const r = rows[i];
    if (!r) return;
    if (r.head !== undefined) {
      elRow.className = 'vl-row wrow head';
      delete elRow.dataset.key;
      elRow.replaceChildren(h('span.wname', r.head || 'Assigned to me'), h('span.chip', r.count));
      return;
    }
    const c = r.card, t = app.timer && app.timer.current;
    const due = isZero(c.Due) ? null : toDate(c.Due);
    elRow.className = 'vl-row wrow' + (i === sel ? ' sel' : '') + (c.Done ? ' done' : '');
    elRow.dataset.key = c.Key;
    elRow.replaceChildren(...kids([
      h('span.wkey.mono', c.Key),
      h('span.wsum', c.Summary),
      t && t.key === c.Key && h('span.wtimer', { title: 'Timer running' }, '⏱'),
      c.Flagged && h('span.wflag', { title: 'Flagged' }, '⚑'),
      due && h('span.wdue' + (!c.Done && due < new Date() ? '.late' : ''), shortDate(c.Due)),
      c.Points && h('span.chip', c.Points),
      group !== 'status' && ui.statusPill(c.Status, catOf(c)),
      h('span.wprio.dim', c.Priority)]));
  }
  function move(d) {
    let i = sel + d;
    while (i >= 0 && i < rows.length && !rows[i].card) i += d;
    if (i >= 0 && i < rows.length) select(i);
  }
  function select(i) {
    const old = sel; sel = i;
    list.refresh(old); list.refresh(sel);
    list.scrollTo(sel);
    if (i > 0 && rows[i - 1].head !== undefined) list.scrollTo(i - 1);
    if (app.panel.key) app.panel.open(rows[i].card.Key);
  }
  const cur = () => (rows[sel] && rows[sel].card) || null;
  function loadWork() {
    return api.swr('/work', d => { cards = d.cards || []; loaded = true; buildRows(); }).catch(e => { loaded = true; ui.errToast(e); paintList(); });
  }

  // ---- time
  function loadLogs() {
    const s = tab === 'week' ? weekStart(day) : day, path = '/worklogs?from=' + ymd(s) + '&to=' + ymd(addDays(s, tab === 'week' ? 7 : 1));
    logsPath = path;
    return api.swr(path, d => {
      if (dead || logsPath !== path) return;
      logs = (d.worklogs || []).map(w => ({ ...w, at: new Date(w.Started) }));
      if (tab !== 'issues') paintTime();
    }).catch(e => { if (!dead) ui.errToast(e); });
  }
  const dayLogs = d => logs.filter(w => ymd(w.at) === ymd(d));
  const curLog = () => dayLogs(day)[wsel];
  function paintTime() { clear(body); tab === 'week' ? paintWeek() : paintDay(); }
  function nav(label, sub) {
    return h('div.time-head',
      h('button.btn.ghost', { onclick: () => step(-1), title: 'Previous (h)' }, '‹'),
      h('div.time-title', h('h2', label), h('div.dim', sub)),
      h('button.btn.ghost', { onclick: () => step(1), title: 'Next (l)' }, '›'),
      h('button.btn', { onclick: goToday, title: '0' }, 'Today'),
      h('span.spacer'),
      h('button.btn', { onclick: () => addLog(), title: 'a' }, 'Log work'),
      h('button.btn', { onclick: copyText, title: 'y' }, 'Copy'));
  }
  const step = n => { day = addDays(day, tab === 'week' ? 7 * n : n); reload(); };
  const goToday = () => { day = today(); reload(); };
  function reload() { logs = []; logsPath = ''; wsel = 0; paintTime(); loadLogs(); }
  function openDay(d) { day = d; tab = 'day'; paintToolbar(); reload(); }

  function paintDay() {
    const ws = dayLogs(day), total = sum(ws), target = targetSeconds(app);
    wsel = Math.min(wsel, Math.max(ws.length - 1, 0));
    body.append(nav(day.toLocaleDateString(undefined, { weekday: 'long', day: 'numeric', month: 'long' }), duration(total) + ' of ' + duration(target)),
      h('div.wbar', h('div.wbar-fill' + (total >= target ? '.full' : ''), { style: { width: Math.min(100, (total / target) * 100) + '%' } })));
    if (!ws.length) { body.append(h('div.empty', logsPath ? 'Nothing logged. ' : 'Loading…', logsPath && h('button.btn.link', { onclick: () => addLog() }, 'Log work (a)'))); return; }
    const t = h('div.wlist');
    ws.forEach((w, i) => t.append(h('div.wlog' + (i === wsel ? '.sel' : ''), { dataset: { key: w.Key, i } },
      h('span.wtime.dim', hm(w.at)), h('span.wkey.mono', w.Key),
      h('span.wsum', w.Summary, w.Comment && h('span.wcomment.dim', ' — ' + w.Comment.replace(/\s+/g, ' '))),
      h('span.wdur', duration(w.Seconds)))));
    body.append(t);
  }

  function paintWeek() {
    const s = weekStart(day), days = Array.from({ length: 7 }, (_, i) => addDays(s, i)), wd = workdays(app), target = targetSeconds(app), now = today();
    const byKey = new Map();
    for (const w of logs) {
      const r = byKey.get(w.Key) || byKey.set(w.Key, { key: w.Key, summary: w.Summary, days: days.map(() => []) }).get(w.Key);
      const di = Math.round((new Date(w.at.getFullYear(), w.at.getMonth(), w.at.getDate()) - s) / 864e5);
      if (r.days[di]) r.days[di].push(w);
    }
    for (const k of extra) if (!byKey.has(k)) byKey.set(k, { key: k, summary: (cards.find(c => c.Key === k) || {}).Summary || '', days: days.map(() => []) });
    weekRows = [...byKey.values()].sort((a, b) => a.key.localeCompare(b.key, undefined, { numeric: true }));
    cell.row = Math.min(cell.row, Math.max(weekRows.length - 1, 0));
    body.append(nav(s.toLocaleDateString(undefined, { day: 'numeric', month: 'short' }) + ' – ' + addDays(s, 6).toLocaleDateString(undefined, { day: 'numeric', month: 'short', year: 'numeric' }), duration(sum(logs)) + ' this week'));
    const tbl = h('table.wweek');
    tbl.append(h('thead', h('tr', h('th.wissue', h('button.btn.link', { onclick: addRow, title: '#' }, '+ row')),
      days.map(d => h('th.wcol' + (wd.includes(d.getDay()) ? '' : '.off') + (ymd(d) === ymd(now) ? '.today' : ''), { onclick: () => openDay(d) },
        d.toLocaleDateString(undefined, { weekday: 'short' }), h('div.dim', d.getDate()))), h('th.wcol', 'Total'))));
    const tb = h('tbody');
    weekRows.forEach((r, ri) => tb.append(h('tr' + (ri === cell.row ? '.sel' : ''), { dataset: { key: r.key } },
      h('td.wissue', h('span.wkey.mono', r.key), h('span.wsum', r.summary)),
      r.days.map((ws, ci) => h('td.wcell' + (ri === cell.row && ci === cell.col ? '.cur' : '') + (ws.length ? '.has' : ''), { dataset: { r: ri, c: ci } }, ws.length ? duration(sum(ws)) : '')),
      h('td.wcell.tot', duration(sum(r.days.flat()))))));
    if (!weekRows.length) tb.append(h('tr', h('td', { colSpan: 9 }, h('div.empty', logsPath ? 'Nothing logged this week.' : 'Loading…'))));
    tbl.append(tb, h('tfoot', h('tr', h('td.wissue', 'Total'), days.map(d => {
      const t = sum(dayLogs(d)), past = d < now && wd.includes(d.getDay()), miss = target - t;
      return h('td.wcell.foot' + (past ? (miss > 0 ? '.short' : '.ok') : ''), t ? duration(t) : '', past && miss > 0 && h('div.miss', '−' + duration(miss)));
    }), h('td.wcell.tot', duration(sum(logs))))));
    body.append(tbl);
  }

  async function pickIssue() {
    const c = await ui.pick({ title: 'Log work on', items: cards, label: c => c.Key + ' ' + c.Summary, placeholder: 'Issue key, or filter your work',
      create: q => ({ Key: q.trim().toUpperCase(), Summary: '' }), empty: 'Type an issue key' });
    if (!c) return null;
    if (!KEY_RE.test(c.Key)) { ui.toast('Not an issue key: ' + c.Key, { kind: 'err' }); return null; }
    return c;
  }
  async function addLog(key, d) {
    const c = key ? { Key: key, Summary: (cards.find(x => x.Key === key) || {}).Summary } : await pickIssue();
    if (!c) return;
    const at = d || day;
    if (await logDialog(app, { key: c.Key, summary: c.Summary, started: ymd(at) === ymd(today()) ? undefined : dayStart(app, at) }) === 'logged') refreshSoon();
  }
  async function addRow() {
    const c = await pickIssue();
    if (c) { extra.add(c.Key); paintTime(); }
  }
  async function editLog() {
    const w = curLog(); if (!w) return;
    if (await logDialog(app, { key: w.Key, summary: w.Summary, seconds: w.Seconds, started: w.at, comment: w.Comment, edit: w.ID }) === 'logged') refreshSoon();
  }
  async function delLog() {
    const w = curLog(); if (!w) return;
    if (!(await ui.confirm({ title: 'Delete worklog', text: duration(w.Seconds) + ' on ' + w.Key + (w.Comment ? ' — ' + w.Comment : ''), ok: 'Delete', danger: true }))) return;
    try {
      await api.del('/worklog/' + w.Key + '/' + w.ID);
      ui.toast('Deleted ' + duration(w.Seconds) + ' on ' + w.Key);
      bus.emit('issue:changed', { key: w.Key });
    } catch (e) { ui.errToast(e); }
  }
  function copyText() {
    let t;
    if (tab === 'week') {
      const s = weekStart(day);
      t = ['Issue\tSummary\t' + Array.from({ length: 7 }, (_, i) => addDays(s, i).toLocaleDateString(undefined, { weekday: 'short' })).join('\t') + '\tTotal',
        ...weekRows.map(r => [r.key, r.summary, ...r.days.map(ws => (ws.length ? duration(sum(ws)) : '')), duration(sum(r.days.flat()))].join('\t'))].join('\n');
    } else {
      const ws = dayLogs(day);
      t = [ymd(day), ...ws.map(w => [w.Key, w.Summary, duration(w.Seconds), w.Comment].join('\t')), 'Total\t\t' + duration(sum(ws))].join('\n');
    }
    navigator.clipboard.writeText(t).then(() => ui.toast('Copied'), e => ui.errToast(e));
  }
  const refreshSoon = debounce(() => { if (dead) return; if (tab !== 'issues') loadLogs(); loadWork(); }, 150);

  function cellEnter() {
    const r = weekRows[cell.row]; if (!r) return;
    const d = addDays(weekStart(day), cell.col);
    r.days[cell.col].length ? openDay(d) : addLog(r.key, d);
  }
  function moveCell(dr, dc) {
    cell = { row: Math.min(Math.max(cell.row + dr, 0), Math.max(weekRows.length - 1, 0)), col: Math.min(Math.max(cell.col + dc, 0), 6) };
    paintTime();
  }

  // ---- layout
  function render() {
    if (list) { list.destroy(); list = null; }
    clear(root);
    if (tab === 'issues') {
      const scroller = h('div.work-scroll');
      empty = h('div.empty', { hidden: true });
      root.append(scroller, empty);
      list = vlist(scroller, { count: rows.length, rowHeight: rowHeight(), create: () => h('div'), bind: bindRow });
      paintList();
    } else {
      root.append(body);
      paintTime();
      loadLogs();
    }
  }

  delegate(root, 'click', '.vl-row', (e, row) => {
    const i = rows.findIndex(r => r.card && r.card.Key === row.dataset.key);
    if (i >= 0) { select(i); app.panel.open(row.dataset.key); }
  });
  delegate(root, 'click', '.wlog', (e, row) => { wsel = +row.dataset.i; paintTime(); app.panel.open(row.dataset.key); });
  delegate(root, 'dblclick', '.wlog', () => editLog());
  delegate(root, 'click', '.wcell:not(.tot):not(.foot)', (e, td) => { cell = { row: +td.dataset.r, col: +td.dataset.c }; paintTime(); });
  delegate(root, 'dblclick', '.wcell:not(.tot):not(.foot)', () => cellEnter());
  delegate(root, 'click', '.wweek td.wissue', (e, td) => { const k = td.parentNode.dataset.key; if (k) app.panel.open(k); });

  // ---- keys
  const G = { group: 'My work' };
  const I = inTab('issues'), D = inTab('day'), W = inTab('week'), T = inTab('day', 'week');
  scope.bind(['j', 'ArrowDown'], () => move(1), 'next', { ...G, ...I });
  scope.bind(['k', 'ArrowUp'], () => move(-1), 'previous', { ...G, ...I });
  scope.bind('Enter', () => { const c = cur(); if (c) app.panel.open(c.Key); }, 'open issue', { ...G, ...I });
  scope.bind('f', () => filterIn.focus(), 'filter', { ...G, ...I });
  scope.bind('v', () => { group = { status: 'project', project: 'none', none: 'status' }[group]; prefs.set('work_group', group); groupSel.value = group; buildRows(); }, 'group by status / project / none', { ...G, ...I });
  scope.bind('d', toggleDone, 'hide done', { ...G, ...I });
  scope.bind('r', () => { api.forget(); loadWork(); ui.toast('Refreshed'); }, 'refresh', { ...G, ...I });
  scope.bind('1', () => setTab('issues'), 'issues', G);
  scope.bind('2', () => setTab('day'), 'worklogs of a day', G);
  scope.bind('3', () => setTab('week'), 'worklogs of a week', G);
  scope.bind('w', () => setTab(tab === 'day' ? 'week' : 'day'), 'worklogs: day / week', G);
  scope.bind('h', () => step(-1), 'previous day / week', { ...G, ...T });
  scope.bind('l', () => step(1), 'next day / week', { ...G, ...T });
  scope.bind('0', goToday, 'today / this week', { ...G, ...T });
  scope.bind('y', copyText, 'copy as text', { ...G, ...T });
  scope.bind('a', () => addLog(), 'log work', { ...G, ...T });
  scope.bind(['j', 'ArrowDown'], () => { wsel = Math.min(wsel + 1, dayLogs(day).length - 1); paintTime(); }, 'next', { ...G, ...D });
  scope.bind(['k', 'ArrowUp'], () => { wsel = Math.max(wsel - 1, 0); paintTime(); }, 'previous', { ...G, ...D });
  scope.bind('ArrowLeft', () => step(-1), 'previous day', { ...G, hidden: true, ...D });
  scope.bind('ArrowRight', () => step(1), 'next day', { ...G, hidden: true, ...D });
  scope.bind('e', editLog, 'edit worklog', { ...G, ...D });
  scope.bind(['d', 'Delete'], delLog, 'delete worklog', { ...G, ...D });
  scope.bind('Enter', () => { const w = curLog(); if (w) app.panel.open(w.Key); }, 'open issue', { ...G, ...D });
  scope.bind(['j', 'ArrowDown'], () => moveCell(1, 0), 'next row', { ...G, ...W });
  scope.bind(['k', 'ArrowUp'], () => moveCell(-1, 0), 'previous row', { ...G, ...W });
  scope.bind('ArrowLeft', () => moveCell(0, -1), 'previous day', { ...G, ...W });
  scope.bind('ArrowRight', () => moveCell(0, 1), 'next day', { ...G, ...W });
  scope.bind('Enter', cellEnter, 'log work in the cell, or open its day', { ...G, ...W });
  scope.bind('#', addRow, 'add an issue row', { ...G, ...W });
  scope.bind('o', () => { const r = weekRows[cell.row]; if (r) app.panel.open(r.key); }, 'open issue', { ...G, ...W });

  const off = [bus.on('issue:changed', refreshSoon), bus.on('focus', refreshSoon), bus.on('timer', () => list && list.refresh())];
  paintToolbar();
  render();
  loadWork();

  return () => { dead = true; if (list) list.destroy(); off.forEach(f => f()); };
}
