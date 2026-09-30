// The board: swim lanes by column or a sortable list, with filters, keyboard
// navigation, drag and drop, and quiet refreshes. Cards render through lib/vlist.js
// so a lane or list of thousands stays smooth.
import { h, clear, delegate, debounce } from '../lib/dom.js';
import { css } from '../lib/css.js';
import { vlist } from '../lib/vlist.js';
import { hwheel } from '../lib/hscroll.js';
import { isZero, date, shortDate, ago } from '../lib/fmt.js';
import { goDate } from '../lib/godate.js';
import * as cq from '../lib/cardquery.js';
import { openFilterBuilder } from './board_filter.js';
import { lastProject, lastBoard, setCtx, pickProject, pickBoard as pickBoardOf, boardOf } from './plan_ctx.js';

css('board');

const DAY = 864e5;
const PRIO_ORD = { blocker: 0, highest: 0, critical: 0, high: 1, major: 1, medium: 2, normal: 2, low: 3, minor: 3, lowest: 4, trivial: 4 };
const PRIO_GLYPH = ['▲▲', '▲', '●', '▼', '▼▼'];
const TYPE_CLS = { bug: 't-bug', story: 't-story', task: 't-task', epic: 't-epic', subtask: 't-sub', 'sub-task': 't-sub' };
const PR_TEXT = { OPEN: '⇄ open', MERGED: '✓ merged', DECLINED: '✕ declined' };
const SORTS = ['rank', 'priority', 'points', 'assignee', 'epic', 'key', 'status', 'updated', 'due', 'created'];
// List columns: id, header, width, class of the cell.
const COLS = {
  mark: ['', '26px'], key: ['Key', '86px'], summary: ['Summary', 'minmax(120px, 1fr)'], status: ['Status', '118px'], priority: ['Prio', '34px'],
  points: ['Pts', '40px'], assignee: ['Assignee', '150px'], epic: ['Epic', '130px'], labels: ['Labels', '110px'], reporter: ['Reporter', '110px'],
  due: ['Due', '64px'], updated: ['Updated', '84px'], created: ['Created', '84px'], age: ['Age', '44px'],
};
const DEFAULT_COLS = ['mark', 'key', 'summary', 'status', 'priority', 'points', 'assignee', 'due', 'updated'];
const FIELD_COL = { type: null, priority: 'priority', status: 'status', points: 'points', assignee: 'assignee', parent: 'epic', due: 'due', age: 'age' };
const SWIMS = ['none', 'assignee', 'epic', 'priority'];
const SEP = '\x1f';

const prioOrd = c => { const v = PRIO_ORD[(c.Priority || '').toLowerCase()]; return v == null ? 5 : v; };
const time = t => (isZero(t) ? Infinity : Date.parse(t));
const num = c => (c.Points === '' || c.Points == null ? -1 : Number(c.Points) || 0);
const who = c => c.AssigneeID || c.Assignee || '';
const catClass = c => (c.Done ? 'done' : c.InProgress ? 'prog' : 'todo');
const avatarURL = u => (u ? '/api/avatar?u=' + encodeURIComponent(u) : '');
const startOfToday = () => { const d = new Date(); d.setHours(0, 0, 0, 0); return d.getTime(); };
const firstOf = (api, path) => new Promise((res, rej) => {
  let got = false;
  api.swr(path, d => { if (!got) { got = true; res(d); } }).catch(e => { if (!got) rej(e); });
});

function parseEvery(s) {
  s = String(s || '').trim().toLowerCase();
  if (s === 'off' || s === '0') return 0;
  const m = s.match(/^(\d+)\s*(s|m|h)$/);
  return m ? Number(m[1]) * { s: 1e3, m: 6e4, h: 36e5 }[m[2]] : 12e4;
}

// Pinned issues are palette commands, first in the list; they outlive the view.
let pinCmds = [];
function registerPins(app) {
  pinCmds.forEach(u => u()); pinCmds = [];
  let l = [];
  try { l = JSON.parse(app.prefs.get('pins', '[]')); } catch (e) { l = []; }
  for (const p of Array.isArray(l) ? l : []) if (Array.isArray(p) && p[0]) pinCmds.push(app.commands.register({ id: 'pin:' + p[0], title: '★ ' + p[0] + '  ' + (p[1] || ''), group: 'Pinned', run: () => app.panel.open(p[0]) }));
}

export default function mount(el, { app, params, query, scope, context, toolbar }) {
  const { api, bus, ui } = app;
  const me = (app.session.me && app.session.me.AccountID) || '';
  const UI = app.session.ui || {};
  const DF = UI.DateFormat || '';
  const CF = UI.CardFields && UI.CardFields.length ? new Set(UI.CardFields.map(x => String(x).toLowerCase().trim())) : null;
  const cf = name => !CF || CF.has(name);
  const CUSTOM = (UI.CustomFields || []).map(n => 'x:' + n);
  const allCols = () => [...Object.keys(COLS), ...CUSTOM];
  const S = {
    project: '', boards: [], board: null, bundle: null, people: new Map(),
    cards: [], total: 0, loaded: false, path: '', fetched: 0, busy: 0,
    scope: query.sprint || 'active', saved: [],
    qf: new Set(), mine: false, who: null, text: '', textFn: null,
    mode: 'lanes', sort: 'rank', dir: 1, swim: 'none', fold: new Set(), compact: false, cols: DEFAULT_COLS,
    past: null, closed: null, pins: new Set(), colors: null, lastEdit: null,
    sel: null, marks: new Set(), rowMem: 0,
    panes: [], where: new Map(), visible: [], built: '', rowH: 0,
    drag: null, dead: false,
  };
  const timers = new Set();
  const later = (fn, ms) => { const t = setTimeout(() => { timers.delete(t); fn(); }, ms); timers.add(t); return t; };
  const offs = [], unreg = [];
  let lastInput = Date.now(), autoT = 0;

  // ---- skeleton
  const filterIn = h('input.input.bd-filter', { type: 'text', placeholder: 'Filter  (f)', spellcheck: false, autocomplete: 'off', title: 'words, status:review,test  points>2  prio>=high  is:flagged  age>3d  due<7d  epic:  -negate  (F builds a query)' });
  const chips = h('div.bd-chips');
  const stats = h('span.bd-stats.dim');
  const bar = h('div.bd-bar', chips, h('span.sp'), filterIn, stats);
  const banner = h('div.bd-banner', { hidden: true });
  const main = h('div.bd-main');
  const root = h('div.bd', bar, banner, main);
  el.append(root);
  const bdMsg = text => clear(main).append(h('div.empty', text));

  // ---- toolbar
  const projectBtn = app.chrome.crumb('Project  (alt+p): switches to its last board', () => pickProjectCtx());
  const boardBtn = app.chrome.crumb('Board  (B)', () => pickBoard());
  const sprintBtn = app.chrome.crumb('View: sprint, backlog, whole board, your views  (v, [ ])', () => pickSprint());
  const modeBtn = h('button.btn', { title: 'Lanes / list  (t)', onclick: () => setMode(S.mode === 'lanes' ? 'list' : 'lanes') });
  const swimBtn = h('button.btn', { title: 'Swimlanes: none, assignee, epic, priority  (O)', onclick: () => cycleSwim() });
  const colsBtn = h('button.btn', { title: 'List columns  (C)', onclick: () => pickCols() }, '▦ Columns');
  const compactBtn = h('button.btn', { title: 'Compact (one-line) cards  (c)', 'aria-pressed': 'false', onclick: () => setCompact(!S.compact) });
  const refreshBtn = h('button.btn.ghost.bd-refresh', { title: 'Refresh  (r)', 'aria-label': 'Refresh', onclick: () => refresh(true) }, h('span.ico-spin', { 'aria-hidden': 'true' }, '⟳'));
  context.append(projectBtn, boardBtn, sprintBtn);
  toolbar.append(h('span.spacer'), swimBtn, colsBtn, compactBtn, modeBtn, refreshBtn);

  function renderToolbar() {
    app.chrome.label(projectBtn, S.project || '…');
    app.chrome.label(boardBtn, S.board ? S.board.Name : '…');
    const sp = scopeLabel();
    sprintBtn.textContent = sp;
    sprintBtn.hidden = !isScrum() && !viewItems().some(i => i.id.includes(':') && !i.id.startsWith('closed'));
    modeBtn.textContent = S.mode === 'lanes' ? '▥ Lanes' : '☰ List';
    swimBtn.hidden = S.mode !== 'lanes'; colsBtn.hidden = S.mode !== 'list'; compactBtn.hidden = S.mode !== 'lanes';
    swimBtn.textContent = S.swim === 'none' ? '☰ Swimlanes' : '☰ by ' + S.swim;
    compactBtn.textContent = S.compact ? '▭ Compact' : '▤ Full';
    compactBtn.setAttribute('aria-pressed', S.compact ? 'true' : 'false');
  }
  const isScrum = () => !!S.board && S.board.Type !== 'kanban';
  const sprints = () => (S.bundle && S.bundle.sprints) || [];
  // Everything [ ] walks through: sprints, backlog, whole board, then ui.views and starred filters.
  function viewItems() {
    const out = [];
    if (isScrum()) {
      sprints().forEach(s => out.push({ id: String(s.ID), name: s.Name, info: s.State }));
      out.push({ id: 'backlog', name: 'Backlog' }, { id: 'all', name: 'Whole board' });
    } else out.push({ id: 'active', name: 'Board' });
    ((app.session.ui && app.session.ui.Views) || []).forEach((v, i) => { if (v.Name && v.JQL) out.push({ id: 'view:' + i, name: v.Name, info: 'view', jql: v.JQL, kind: 'jql' }); });
    for (const f of S.saved) out.push({ id: 'filter:' + f.ID, name: f.Name, info: 'saved filter', jql: f.JQL, kind: 'filter' });
    return out;
  }
  function scopeLabel() {
    if (S.closed) return S.closed.Name + ' (closed)';
    const it = viewItems().find(i => i.id === S.scope);
    if (it && it.kind) return it.name;
    if (S.scope === 'backlog') return 'Backlog';
    if (S.scope === 'all') return 'Whole board';
    const r = resolveScope();
    const sp = r.sprint && sprints().find(s => s.ID === r.sprint);
    return sp ? sp.Name : 'Whole board';
  }

  // ---- filter bar
  function qfs() { return (S.bundle && S.bundle.quickFilters) || []; }
  const anyFilter = () => S.mine || S.who != null || S.qf.size || S.text.trim();
  function renderBar() {
    const kids = [];
    kids.push(h('button.fchip' + (S.mine ? '.on' : ''), { dataset: { act: 'mine' }, title: 'Assigned to me  (m)' }, h('kbd', 'm'), 'Mine'));
    kids.push(h('button.fchip' + (S.who != null ? '.on' : ''), { dataset: { act: 'who' }, title: 'Assignee  (A)' }, h('kbd', 'A'), S.who == null ? 'Assignee' : whoName(S.who)));
    qfs().slice(0, 9).forEach((q, i) => kids.push(h('button.fchip' + (S.qf.has(q.ID) ? '.on' : ''), { dataset: { qf: q.ID }, title: q.JQL }, h('kbd', i + 1), q.Name)));
    cq.words(S.text).forEach((w, i) => kids.push(h('button.fchip.term', { dataset: { term: i }, title: 'Remove ' + w }, w, ' ✕')));
    if (anyFilter()) kids.push(h('button.fchip.clear', { dataset: { act: 'clear' }, title: 'Clear filters  (0)' }, '✕ clear'));
    clear(chips).append(...kids);
  }
  const whoName = id => {
    if (id === '-') return 'Unassigned';
    const c = S.cards.find(c => who(c) === id);
    return c ? c.Assignee : (S.people.get(id) || id);
  };
  // The project's assignable people (like the TUI), fetched once per project.
  const peopleCache = (app._boardPeople = app._boardPeople || new Map());
  async function loadPeople() {
    const p = S.project;
    if (!p) return;
    if (!peopleCache.has(p)) peopleCache.set(p, api.get('/users?project=' + encodeURIComponent(p)).catch(() => []));
    const us = await peopleCache.get(p);
    if (p !== S.project) return;
    S.people = new Map((us || []).map(u => [u.AccountID, u.DisplayName]));
  }
  delegate(chips, 'click', 'button', (e, b) => {
    if (b.dataset.term != null) { filterIn.value = cq.removeTerm(S.text, Number(b.dataset.term)); setText(filterIn.value); }
    else if (b.dataset.qf) toggleQF(Number(b.dataset.qf));
    else if (b.dataset.act === 'mine') toggleMine();
    else if (b.dataset.act === 'who') pickWho();
    else clearFilters();
  });
  const setText = t => { S.text = t; S.textFn = cq.compile(t, { me, pins: S.pins }); layout(); renderBar(); };
  const onText = debounce(() => setText(filterIn.value), 70);
  filterIn.addEventListener('input', onText);
  filterIn.addEventListener('keydown', e => {
    if (e.key === 'Enter' || e.key === 'Escape') { e.stopPropagation(); if (e.key === 'Escape' && filterIn.value) { filterIn.value = ''; onText(); } filterIn.blur(); }
  });

  function renderStats() {
    const vis = S.visible;
    let pts = 0;
    for (const c of vis) pts += Number(c.Points) || 0;
    const filtered = vis.length !== S.cards.length;
    stats.textContent = (filtered ? vis.length + ' of ' : '') + S.cards.length + ' issues' + (pts ? ' · ' + (Math.round(pts * 10) / 10) + ' pts' : '') + (S.total > S.cards.length ? ' (' + S.total + ' in Jira)' : '');
  }

  // ---- data
  function resolveScope() {
    const sps = sprints();
    if (S.closed) return { sprint: S.closed.ID };
    const vi = viewItems().find(i => i.id === S.scope);
    if (vi && vi.kind) return { view: vi };
    if (!isScrum() || S.scope === 'all') return {};
    if (S.scope === 'backlog') return { backlog: 1 };
    if (S.scope === 'active') {
      const a = sps.find(s => s.State === 'active') || sps[0];
      return a ? { sprint: a.ID } : {};
    }
    return { sprint: Number(S.scope) };
  }
  function cardsPath() {
    const p = new URLSearchParams(), r = resolveScope();
    if (r.sprint) p.set('sprint', r.sprint);
    if (r.backlog) p.set('backlog', 1);
    const jql = qfs().filter(q => S.qf.has(q.ID)).map(q => '(' + q.JQL + ')').join(' AND ');
    if (r.view) {
      p.set('kind', r.view.kind); p.set('jql', r.view.jql);
      if (jql) p.set('filter', jql);
      return '/boards/' + S.board.ID + '/viewcards?' + p;
    }
    if (jql) p.set('jql', jql);
    const s = p.toString();
    return '/boards/' + S.board.ID + '/cards' + (s ? '?' + s : '');
  }
  function setBusy(d) { S.busy += d; refreshBtn.classList.toggle('busy', S.busy > 0); }

  // Show cached data at once, then the fresh answer.
  function loadCards() {
    loadPeople().then(renderBar);
    const path = cardsPath();
    if (path === S.path) return;
    S.path = path;
    setBusy(1);
    api.swr(path, d => { if (S.path === path && !S.dead) setCards(d); })
      .catch(e => { if (S.path === path) fail(e); })
      .finally(() => { setBusy(-1); S.fetched = Date.now(); });
  }
  // A quiet refetch: no spinner in the lanes, only changed cards touched.
  function refresh(loud) {
    if (!S.board || S.dead) return Promise.resolve();
    const path = S.path || cardsPath();
    S.path = path;
    setBusy(1);
    const reqs = [api.get(path, { fresh: true }).then(d => { if (S.path === path && !S.dead) setCards(d); })];
    if (loud) reqs.push(api.get('/boards/' + S.board.ID, { fresh: true }).then(setBundle));
    return Promise.all(reqs)
      .then(() => { S.fetched = Date.now(); if (loud) ui.toast('Refreshed'); })
      .catch(e => { if (loud || !S.loaded) fail(e); })
      .finally(() => setBusy(-1));
  }
  function fail(e) {
    if (S.loaded) ui.errToast(e);
    else bdMsg(h('div', h('h2', 'Could not load the board'), h('pre', e.message)));
  }
  function setBundle(b) {
    const first = !S.bundle;
    S.bundle = b;
    if (first && UI.CardColors !== 'off') {
      api.swr('/boards/' + S.board.ID + '/cardcolors?scope=' + encodeURIComponent('project = ' + S.project), c => { S.colors = c; for (const p of S.panes) p.vl.refresh(); }).catch(() => {});
    }
    registerQFCommands();
    renderToolbar(); renderBar();
    if (first || S.scope === 'active') loadCards();
    layout();
  }
  function setCards(d) {
    const list = d.cards || [];
    if (S.loaded && sameCards(S.cards, list)) { S.total = d.total || list.length; return; }
    S.cards = list; S.total = d.total || list.length; S.loaded = true;
    layout();
    renderBar();
    if (S.past && !S.past.moves && !S.past.fetching) { S.past.fetching = true; fetchMoves(); }
  }
  function sameCards(a, b) {
    if (a.length !== b.length) return false;
    for (let i = 0; i < a.length; i++) if (a[i] !== b[i] && JSON.stringify(a[i]) !== JSON.stringify(b[i])) return false;
    return true;
  }

  // ---- filtering and layout
  function passes(c) {
    if (S.mine && c.AssigneeID !== me) return false;
    if (S.who != null && (S.who === '-' ? !!who(c) : who(c) !== S.who)) return false;
    if (S.textFn && !S.textFn(c)) return false;
    return true;
  }
  const CMP = {
    key: (a, b) => a.Key.localeCompare(b.Key, undefined, { numeric: true }),
    summary: (a, b) => a.Summary.localeCompare(b.Summary),
    status: (a, b) => colIndexOf(a) - colIndexOf(b),
    priority: (a, b) => prioOrd(a) - prioOrd(b),
    points: (a, b) => num(b) - num(a),
    assignee: (a, b) => (!a.Assignee) - (!b.Assignee) || a.Assignee.localeCompare(b.Assignee),
    epic: (a, b) => (!a.ParentSummary) - (!b.ParentSummary) || a.ParentSummary.localeCompare(b.ParentSummary),
    updated: (a, b) => time(b.Updated) - time(a.Updated) || 0,
    created: (a, b) => time(b.Created) - time(a.Created) || 0,
    due: (a, b) => (time(a.Due) === time(b.Due) ? 0 : time(a.Due) < time(b.Due) ? -1 : 1),
  };
  function columns() { return (S.bundle && S.bundle.config && S.bundle.config.Columns) || []; }
  function statusCol() {
    const m = new Map();
    columns().forEach((c, i) => (c.StatusIDs || []).forEach(id => m.set(String(id), i)));
    return m;
  }
  let sc = new Map();
  const colIndexOf = c => { const i = sc.get(String(c.StatusID)); return i == null ? columns().length : i; };

  // ---- the time machine: the cards where their status changes had them at the end of a day
  const asOf = p => {
    if (p.at) return p.at;
    const n = new Date();
    return new Date(n.getFullYear(), n.getMonth(), n.getDate() - p.days + 1).getTime();
  };
  function statusAt(moves, t, cur) {
    if (!moves || !moves.length) return cur;
    for (let i = moves.length - 1; i >= 0; i--) if (Date.parse(moves[i].When) <= t) return moves[i].To;
    return moves[0].From;
  }
  function shownCards() {
    const p = S.past;
    if (!p || !p.moves) return S.cards;
    const at = asOf(p), last = columns().length - 1, names = (S.bundle && S.bundle.statusNames) || {};
    const out = [];
    for (const c of S.cards) {
      if (!isZero(c.Created) && Date.parse(c.Created) >= at) continue;
      const id = statusAt(p.moves[c.Key], at, c.StatusID);
      if (String(id) === String(c.StatusID)) { out.push(c); continue; }
      const col = sc.get(String(id));
      out.push({ ...c, StatusID: String(id), Status: names[id] || c.Status, Done: col === last, InProgress: col != null && col > 0 && col < last });
    }
    return out;
  }

  // ---- swimlanes: bands by assignee, epic or priority
  const groupName = c => (S.swim === 'assignee' ? c.Assignee || 'Unassigned' : S.swim === 'epic' ? c.ParentSummary || 'No epic' : c.Priority || 'No priority');
  function groupsOf(cards) {
    const m = new Map();
    for (const c of cards) { const n = groupName(c); (m.get(n) || m.set(n, []).get(n)).push(c); }
    const out = [...m].map(([name, list]) => ({ name, cards: list }));
    const last = S.swim === 'assignee' ? 'Unassigned' : S.swim === 'epic' ? 'No epic' : 'No priority';
    out.sort((a, b) => (a.name === last) - (b.name === last)
      || (S.swim === 'priority' ? prioOrd(a.cards[0]) - prioOrd(b.cards[0]) : 0) || a.name.localeCompare(b.name));
    return out;
  }

  function layout() {
    if (!S.bundle || !S.loaded || S.dead) return;
    sc = statusCol();
    const cols = columns(), base = S.base = shownCards(), vis = base.filter(passes);
    S.visible = vis;
    const lanes = S.mode === 'lanes', swim = lanes && S.swim !== 'none';
    const other = lanes && base.some(c => !sc.has(String(c.StatusID)));
    const groups = swim ? groupsOf(vis) : null;
    const sig = S.mode + '|' + cols.map(c => c.Name).join(',') + (other ? '|other' : '') + (swim ? '|swim:' + S.swim + ':' + groups.map(g => g.name).join('\x1e') : '') + (lanes ? '' : '|' + S.cols.join(','));
    root.classList.toggle('compact', S.compact);
    if (sig !== S.built) build(sig, lanes, cols, other, groups);
    if (swim) {
      const nc = S.ncols;
      S.colTotal.fill(0);
      S.panes.forEach(p => {
        const g = groups[p.group];
        p.all = []; p.total = 0; p.folded = S.fold.has(g.name);
      });
      for (const c of base) S.colTotal[Math.min(colIndexOf(c), nc - 1)]++;
      groups.forEach((g, gi) => { for (const c of g.cards) S.panes[gi * nc + Math.min(colIndexOf(c), nc - 1)].all.push(c); });
    } else if (lanes) {
      for (const p of S.panes) { p.all = []; p.total = 0; }
      for (const c of base) S.panes[Math.min(colIndexOf(c), S.panes.length - 1)].total++;
      for (const c of vis) S.panes[Math.min(colIndexOf(c), S.panes.length - 1)].all.push(c);
    } else {
      let l = vis;
      if (S.sort !== 'rank') { const f = CMP[S.sort]; l = vis.slice().sort((a, b) => f(a, b) * S.dir); }
      else if (S.dir < 0) l = vis.slice().reverse();
      S.panes[0].all = l; S.panes[0].total = base.length;
    }
    for (const p of S.panes) p.cards = p.folded ? [] : p.all;
    S.where.clear();
    for (const p of S.panes) p.cards.forEach((c, i) => S.where.set(c.Key, { p, i }));
    for (const k of [...S.marks]) if (!S.where.has(k)) S.marks.delete(k);
    if (S.sel && !S.where.has(S.sel)) S.sel = null;
    const rh = swim ? 0 : probeHeight(lanes);
    for (const p of S.panes) {
      if (rh !== p.rh) { p.rh = rh; p.vl.setRowHeight(rh); }
      p.vl.setCount(p.cards.length);
      if (!swim) paintHead(p);
    }
    if (swim) paintSwim(groups);
    renderStats(); renderBanner();
  }

  // Row height is measured from a real card, so density and fonts decide it.
  function probeHeight(lanes) {
    const w = lanes ? buildCard() : buildRow();
    const c = { Key: 'X-1', Summary: 'Probe', Type: 'Task', Status: 'x', Priority: 'High', Points: '1', Labels: 'a', ParentKey: 'X-0', Assignee: 'A B', Subtasks: 1, Due: '2026-01-01T00:00:00Z' };
    lanes ? fillCard(w, c) : fillRow(w, c);
    w.style.cssText = 'position:absolute;visibility:hidden;left:0;right:0;top:0';
    const host = main.firstChild && main.firstChild.querySelector('.bd-lane-body, .bd-lbody');
    (host || main).append(w);
    const hgt = Math.ceil(w.getBoundingClientRect().height) || (lanes ? 96 : 32);
    w.remove();
    return hgt;
  }

  // A list of rows without virtualisation, for a swimlane's small cells. Same surface as vlist.
  function plainList(body, o) {
    const rows = [];
    return {
      setCount(n) {
        while (rows.length < n) { const el = o.create(); el.classList.add('vl-row', 'vl-static'); body.append(el); rows.push(el); }
        while (rows.length > n) rows.pop().remove();
        rows.forEach((el, i) => o.bind(el, i));
      },
      setRowHeight() {},
      refresh(i) { if (i == null) rows.forEach((el, j) => o.bind(el, j)); else if (rows[i]) o.bind(rows[i], i); },
      scrollTo(i) { const el = rows[i]; if (el && el.scrollIntoView) el.scrollIntoView({ block: 'nearest', inline: 'nearest' }); },
      rowEl: i => rows[i] || null,
      destroy() { rows.length = 0; body.replaceChildren(); },
    };
  }

  // ---- building the lanes or list
  function laneNames(cols, other) {
    return cols.map((c, i) => ({ name: c.Name, max: c.Max || 0, col: i })).concat(other ? [{ name: 'Other', max: 0, col: cols.length }] : []);
  }
  const catOf = (i, n, other) => (i === 0 ? 'new' : i === n - 1 && !other ? 'done' : 'prog');
  function build(sig, lanes, cols, other, groups) {
    for (const p of S.panes) p.vl.destroy();
    S.panes = []; S.built = sig; clear(main);
    if (lanes && groups) {
      const names = laneNames(cols, other);
      const wrap = h('div.bd-swim'); hwheel(wrap);
      wrap.style.setProperty('--ncols', names.length);
      S.ncols = names.length;
      S.heads = names.map((n, i) => {
        const hd = { max: n.max };
        hd.count = h('span.bd-count'); hd.wip = h('span.bd-wip');
        hd.el = h('div.bd-lane-head.k' + catOf(i, names.length, other), h('span.bd-lane-name', n.name), hd.count, hd.wip);
        return hd;
      });
      S.colTotal = names.map(() => 0);
      wrap.append(h('div.bd-swim-head', S.heads.map(x => x.el)));
      S.bands = groups.map((g, gi) => {
        const band = { name: g.name, caret: h('span.bd-caret'), count: h('span.bd-count') };
        band.head = h('div.bd-band-head', { dataset: { fold: g.name }, title: 'Fold / unfold  (z, Z)' }, band.caret, h('span.bd-band-name', g.name), band.count);
        const row = h('div.bd-band-row');
        names.forEach(n => {
          const pane = { col: n.col, group: gi, name: n.name, cards: [], all: [], total: 0, rh: 0 };
          pane.body = h('div.bd-lane-body.static');
          pane.el = h('section.bd-lane.cell', { dataset: { pane: S.panes.length } }, pane.body);
          pane.vl = plainList(pane.body, { create: buildCard, bind: (w, j) => fillCard(w, pane.cards[j]) });
          S.panes.push(pane);
          row.append(pane.el);
        });
        band.el = h('section.bd-band', band.head, row);
        wrap.append(band.el);
        return band;
      });
      main.append(wrap);
    } else if (lanes) {
      const wrap = h('div.bd-lanes'); hwheel(wrap);
      const names = laneNames(cols, other);
      names.forEach((n, i) => {
        const pane = { col: n.col, name: n.name, max: n.max, cards: [], all: [], total: 0, rh: 0 };
        pane.count = h('span.bd-count');
        pane.wip = h('span.bd-wip');
        pane.head = h('div.bd-lane-head.k' + catOf(i, names.length, other), h('span.bd-lane-name', n.name), pane.count, pane.wip);
        pane.body = h('div.bd-lane-body', { role: 'list', 'aria-label': n.name });
        pane.drop = h('div.bd-drop', { hidden: true });
        pane.body.append(pane.drop);
        pane.el = h('section.bd-lane', { dataset: { pane: i }, 'aria-label': n.name }, pane.head, pane.body);
        pane.vl = vlist(pane.body, { rowHeight: 96, create: buildCard, bind: (w, j) => fillCard(w, pane.cards[j]) });
        S.panes.push(pane);
        wrap.append(pane.el);
      });
      main.append(wrap);
    } else {
      const pane = { col: -1, name: 'list', cards: [], all: [], total: 0, rh: 0 };
      pane.head = h('div.bd-lhead', S.cols.map(id => h('span', { class: 'lh-' + id, dataset: { sort: sortable(id) ? id : '' } }, colLabel(id), sortMark(id))));
      pane.body = h('div.bd-lbody');
      pane.drop = h('div.bd-drop', { hidden: true });
      pane.body.append(pane.drop);
      pane.vl = vlist(pane.body, { rowHeight: 32, create: buildRow, bind: (w, j) => fillRow(w, pane.cards[j]) });
      S.panes.push(pane);
      const list = h('div.bd-list', pane.head, pane.body);
      list.style.setProperty('--lcols', S.cols.map(colWidth).join(' '));
      pane.el = list;
      main.append(list);
    }
  }
  const sortable = id => id !== 'mark' && id !== 'age' && !id.startsWith('x:') && id !== 'labels' && id !== 'reporter';
  const colLabel = id => (id.startsWith('x:') ? id.slice(2) : (COLS[id] || ['', ''])[0]);
  const colWidth = id => (COLS[id] ? COLS[id][1] : '120px');
  const sortMark = id => (S.sort === id ? (S.dir > 0 ? ' ▲' : ' ▼') : '');
  function paintHead(p) {
    if (S.mode !== 'lanes') {
      for (const s of p.head.children) s.textContent = colLabel(s.className.replace('lh-', '')) + sortMark(s.dataset.sort);
      return;
    }
    const filtered = p.all.length !== p.total;
    p.count.textContent = filtered ? p.all.length + '/' + p.total : p.total;
    const over = p.max && p.total > p.max;
    p.wip.textContent = p.max ? 'max ' + p.max : '';
    p.head.classList.toggle('over', !!over);
    p.wip.title = over ? 'Over the WIP limit of ' + p.max : 'WIP limit';
    p.body.classList.toggle('empty', !p.cards.length);
  }
  function paintSwim(groups) {
    S.heads.forEach((hd, i) => {
      const shown = S.panes.reduce((n, p) => n + (p.col === i ? p.all.length : 0), 0);
      hd.count.textContent = shown !== S.colTotal[i] ? shown + '/' + S.colTotal[i] : S.colTotal[i];
      const over = hd.max && S.colTotal[i] > hd.max;
      hd.wip.textContent = hd.max ? 'max ' + hd.max : '';
      hd.el.classList.toggle('over', !!over);
    });
    S.bands.forEach((b, gi) => {
      const f = S.fold.has(b.name);
      b.caret.textContent = f ? '▸' : '▾';
      b.count.textContent = groups[gi].cards.length;
      b.el.classList.toggle('folded', f);
    });
    for (const p of S.panes) p.body.classList.toggle('empty', !p.all.length);
  }

  // ---- cards
  function buildCard() {
    const r = {};
    const w = h('div.bcw', { role: 'listitem' }, h('div.card', { draggable: true },
      h('div.c1', r.type = h('span.ctype'), r.key = h('span.ckey'), r.pin = h('span.cpin', { title: 'Pinned' }, '★'), r.flag = h('span.cflag', { title: 'Flagged' }, '⚑'), h('span.sp'), r.prio = h('span.cprio'), r.pts = h('span.cpts')),
      r.sum = h('div.csum'),
      h('div.c3', r.parent = h('span.cparent'), r.sub = h('span.csub'), r.due = h('span.cdue'), r.pr = h('span.cpr'), r.dep = h('span.cdep'), r.extra = h('span.cextra'), r.labels = h('span.clabels'), h('span.sp'), r.age = h('span.cage'), r.av = h('span.cav'))));
    w._r = r;
    return w;
  }
  const hourTick = () => Math.floor(Date.now() / 36e5);
  const ribbonOf = c => {
    const cc = S.colors;
    if (!cc || !cc.By) return '';
    if (cc.By === 'custom') return (cc.Keys && cc.Keys[c.Key]) || '';
    const v = cc.By === 'priority' ? c.Priority : cc.By === 'issuetype' ? c.Type : cc.By === 'assignee' ? c.Assignee : '';
    const hit = v && (cc.Colors || []).find(x => x.Value.toLowerCase() === v.toLowerCase());
    return hit ? hit.Color : '';
  };
  const sigOf = c => [c.Key, c.Summary, c.Type, c.Status, c.Priority, c.AssigneeID, c.AvatarURL, c.Points, c.ParentKey, c.Subtasks, c.SubtasksDone, c.Due, c.Done, c.Flagged, c.InProgress, c.PR, c.Deploy, c.Labels, c.Updated, c.Extra, S.sel === c.Key, S.marks.has(c.Key), S.pins.has(c.Key), ribbonOf(c), hourTick()].join('|');
  function ageText(c) {
    if (c.Done) return '';
    const t = !isZero(c.Since) ? c.Since : c.Created;
    if (isZero(t)) return '';
    const d = Math.floor((Date.now() - Date.parse(t)) / DAY);
    return d > 0 ? d + 'd' : Math.max(1, Math.floor((Date.now() - Date.parse(t)) / 36e5)) + 'h';
  }
  function setAvatar(slot, c, name) {
    const k = c.Assignee + '|' + c.AvatarURL + '|' + !!name;
    if (slot._k === k) return;
    slot._k = k;
    slot.replaceChildren(ui.avatar(c.Assignee, avatarURL(c.AvatarURL), 20), name && c.Assignee ? h('span.l-name', ' ' + c.Assignee) : '');
  }
  const extraValues = c => (c.Extra ? c.Extra.split(SEP).map(kv => kv.slice(kv.indexOf('=') + 1)) : []);
  const fdate = (t, fallback) => (DF ? goDate(t, DF) : fallback);
  function fillCard(w, c) {
    if (!c) return;
    const sig = sigOf(c);
    if (w._sig === sig) return;
    w._sig = sig;
    const r = w._r, card = w.firstChild;
    w.dataset.key = c.Key;
    const rib = ribbonOf(c);
    card.className = 'card ' + catClass(c) + (c.Flagged && cf('flagged') ? ' flagged' : '') + (S.sel === c.Key ? ' sel' : '') + (S.marks.has(c.Key) ? ' mark' : '') + (rib ? ' ribbon' : '');
    if (rib) card.style.setProperty('--ribbon', rib); else card.style.removeProperty('--ribbon');
    r.type.hidden = !cf('type');
    r.type.className = 'ctype ' + (TYPE_CLS[(c.Type || '').toLowerCase()] || 't-other');
    r.type.textContent = (c.Type || '?')[0].toUpperCase(); r.type.title = c.Type;
    r.key.textContent = c.Key; app.agents && app.agents.stamp(r.key, c.Key);
    r.pin.hidden = !S.pins.has(c.Key);
    r.flag.hidden = !c.Flagged || !cf('flagged');
    const po = prioOrd(c);
    r.prio.hidden = !c.Priority || !cf('priority');
    r.prio.className = 'cprio p' + po; r.prio.textContent = po < 5 ? PRIO_GLYPH[po] : (c.Priority || '').slice(0, 3); r.prio.title = c.Priority;
    r.pts.hidden = c.Points === '' || c.Points == null || !cf('points'); r.pts.textContent = c.Points;
    r.sum.textContent = c.Summary; r.sum.title = c.Summary;
    r.parent.hidden = !c.ParentKey || !cf('parent'); r.parent.textContent = c.ParentKey ? c.ParentSummary || c.ParentKey : '';
    r.parent.dataset.open = c.ParentKey || ''; r.parent.title = c.ParentKey ? c.ParentKey + ' ' + c.ParentSummary : '';
    r.sub.hidden = !c.Subtasks || !cf('subtasks');
    if (c.Subtasks) { r.sub.textContent = c.SubtasksDone + '/' + c.Subtasks; r.sub.style.setProperty('--p', Math.round(100 * c.SubtasksDone / c.Subtasks) + '%'); r.sub.title = 'Subtasks done'; }
    const due = date(c.Due);
    r.due.hidden = !due || !cf('due');
    if (due) { r.due.textContent = fdate(c.Due, shortDate(c.Due)); r.due.className = 'cdue' + (!c.Done && due.getTime() < startOfToday() ? ' overdue' : ''); r.due.title = 'Due ' + due.toLocaleDateString(); }
    r.pr.hidden = !c.PR || !cf('pr'); r.pr.textContent = PR_TEXT[c.PR] || c.PR; r.pr.className = 'cpr pr-' + (c.PR || '').toLowerCase();
    r.dep.hidden = !c.Deploy || !cf('deploy'); r.dep.textContent = '↑ ' + c.Deploy;
    const ev = extraValues(c);
    r.extra.hidden = !ev.length; r.extra.textContent = ev.join(' · '); r.extra.title = ev.length ? c.Extra.split(SEP).join(', ') : '';
    const ls = c.Labels ? c.Labels.split(' ') : [];
    r.labels.hidden = !ls.length; r.labels.textContent = ls.slice(0, 2).map(l => '#' + l).join(' ') + (ls.length > 2 ? ' +' + (ls.length - 2) : ''); r.labels.title = c.Labels;
    const a = cf('age') ? ageText(c) : '';
    r.age.textContent = a; r.age.title = a ? 'In status since ' + ago(!isZero(c.Since) ? c.Since : c.Created) : '';
    r.av.hidden = !(cf('avatar') || cf('assignee'));
    setAvatar(r.av, c);
  }

  function buildRow() {
    const r = {};
    const w = h('div.lrow', { draggable: true }, S.cols.map(id => (r[id] = h('span', { class: 'l-' + id.replace(/\W+/g, '-') }))));
    w._r = r;
    return w;
  }
  const FILL = {
    mark: (e, c) => { e.textContent = S.marks.has(c.Key) ? '☑' : S.pins.has(c.Key) ? '★' : c.Flagged ? '⚑' : ''; },
    key: (e, c) => { e.textContent = c.Key; app.agents && app.agents.stamp(e, c.Key); },
    summary: (e, c) => { e.textContent = c.Summary; e.title = c.Summary; },
    status: (e, c) => { e.textContent = c.Status; e.className = 'l-status pill cat-' + (c.Done ? 'done' : c.InProgress ? 'indeterminate' : 'new'); },
    priority: (e, c) => { const po = prioOrd(c); e.className = 'l-priority cprio p' + po; e.textContent = po < 5 ? PRIO_GLYPH[po] : ''; e.title = c.Priority; },
    points: (e, c) => { e.textContent = c.Points; },
    assignee: (e, c) => setAvatar(e, c, true),
    epic: (e, c) => { e.textContent = c.ParentSummary || ''; e.title = c.ParentKey ? c.ParentKey + ' ' + c.ParentSummary : ''; },
    labels: (e, c) => { e.textContent = (c.Labels || '').split(' ').filter(Boolean).map(l => '#' + l).join(' '); },
    reporter: (e, c) => { e.textContent = c.Reporter || ''; },
    due: (e, c) => { const d = date(c.Due); e.textContent = d ? fdate(c.Due, shortDate(c.Due)) : ''; e.className = 'l-due' + (d && !c.Done && d.getTime() < startOfToday() ? ' overdue' : ''); },
    updated: (e, c) => { e.textContent = ago(c.Updated); e.title = fdate(c.Updated, ''); },
    created: (e, c) => { e.textContent = ago(c.Created); e.title = fdate(c.Created, ''); },
    age: (e, c) => { e.textContent = ageText(c); },
  };
  function fillRow(w, c) {
    if (!c) return;
    const sig = sigOf(c);
    if (w._sig === sig) return;
    w._sig = sig;
    const r = w._r;
    w.dataset.key = c.Key;
    w.className = 'vl-row lrow ' + catClass(c) + (S.sel === c.Key ? ' sel' : '') + (S.marks.has(c.Key) ? ' mark' : '') + (c.Flagged ? ' flagged' : '');
    const rib = ribbonOf(c);
    if (rib) w.style.setProperty('--ribbon', rib); else w.style.removeProperty('--ribbon');
    w.classList.toggle('ribbon', !!rib);
    for (const id of S.cols) {
      if (FILL[id]) FILL[id](r[id], c);
      else if (id.startsWith('x:')) r[id].textContent = cq.extraOf(c)[id.slice(2).toLowerCase()] || '';
    }
  }

  // ---- selection and navigation
  function rebind(key) { const w = S.where.get(key); if (w) w.p.vl.refresh(w.i); }
  const openPanel = debounce(() => { if (S.sel && app.panel.key && app.panel.key !== S.sel) app.panel.open(S.sel); }, 110);
  function select(key, { scroll = true, row = true } = {}) {
    const old = S.sel;
    S.sel = key;
    if (old && old !== key) rebind(old);
    if (key) {
      const w = S.where.get(key);
      if (w) { if (row) S.rowMem = w.i; if (scroll) w.p.vl.scrollTo(w.i); w.p.vl.refresh(w.i); }
      openPanel();
    }
  }
  const cur = () => S.where.get(S.sel);
  const curCard = () => (cur() ? cur().p.cards[cur().i] : null);
  const cardEl = key => { const w = S.where.get(key); const e = w && w.p.vl.rowEl(w.i); return e ? e.querySelector('.card') || e : null; };
  const swimming = () => S.mode === 'lanes' && S.swim !== 'none';
  function move(dx, dy) {
    const w = cur();
    if (!w) {
      const p = S.panes.find(p => p.cards.length);
      if (p) { S.rowMem = 0; select(p.cards[0].Key); }
      return;
    }
    if (dy) {
      const n = w.p.cards.length, i = w.i + dy;
      if (swimming() && Math.abs(dy) === 1 && (i < 0 || i >= n)) {
        // past the end of a cell: the same column in the next band that has cards
        for (let g = w.p.group + dy; g >= 0 && g < S.panes.length / S.ncols; g += dy) {
          const q = S.panes[g * S.ncols + w.p.col];
          if (q.cards.length) return select(dy > 0 ? q.cards[0].Key : q.cards[q.cards.length - 1].Key);
        }
        return;
      }
      return select(w.p.cards[Math.max(0, Math.min(n - 1, i))].Key);
    }
    if (dx && S.mode === 'lanes') {
      const P = swimming() ? S.panes.filter(q => q.group === w.p.group) : S.panes;
      let pi = P.indexOf(w.p) + dx;
      while (P[pi] && !P[pi].cards.length) pi += dx;
      const p = P[pi];
      if (p) select(p.cards[Math.min(S.rowMem, p.cards.length - 1)].Key, { row: false });
    }
  }
  function edge(end) {
    const w = cur(); const p = w ? w.p : S.panes.find(p => p.cards.length);
    if (p && p.cards.length) select(p.cards[end ? p.cards.length - 1 : 0].Key);
  }
  const page = d => { const w = cur(); if (w) move(0, d * Math.max(1, Math.floor(w.p.body.clientHeight / (w.p.rh || 60)) - 1)); else move(0, 1); };

  function toggleMark(key) {
    if (!key) return;
    S.marks.has(key) ? S.marks.delete(key) : S.marks.add(key);
    rebind(key);
  }

  // ---- mouse
  delegate(main, 'click', '[data-key]', (e, t) => {
    const key = t.dataset.key;
    if (e.target.closest('.cparent') && e.target.closest('.cparent').dataset.open) { app.panel.open(e.target.closest('.cparent').dataset.open); return; }
    if (e.ctrlKey || e.metaKey || e.shiftKey) { toggleMark(key); select(key, { scroll: false }); return; }
    select(key, { scroll: false });
    app.panel.open(key);
  });
  delegate(main, 'click', '[data-fold]', (e, t) => foldBand(t.dataset.fold));
  delegate(main, 'click', '[data-sort]', (e, t) => { if (t.dataset.sort) setSort(t.dataset.sort, S.sort === t.dataset.sort ? -S.dir : 1); });
  delegate(main, 'dblclick', '[data-key]', (e, t) => window.open(app.session.baseURL + '/browse/' + t.dataset.key, '_blank', 'noopener'));

  // ---- drag and drop
  const paneOfEl = t => {
    const s = t.closest && t.closest('.bd-lane');
    if (s) return S.panes[Number(s.dataset.pane)];
    return S.mode === 'list' && t.closest && t.closest('.bd-lbody') ? S.panes[0] : null;
  };
  const listRanks = () => S.mode === 'list' && S.sort === 'rank' && S.dir > 0;
  main.addEventListener('dragstart', e => {
    const w = e.target.closest && e.target.closest('[data-key]');
    if (!w || S.past || (S.mode === 'list' && !listRanks())) { if (w) e.preventDefault(); return; }
    S.drag = w.dataset.key;
    e.dataTransfer.effectAllowed = 'move';
    e.dataTransfer.setData('text/plain', S.drag);
    (w.firstChild.classList ? w.firstChild : w).classList.add('dragging');
    const key = S.drag;
    setTimeout(() => { if (S.drag === key) showZones(key); }, 0);
  });
  function dropIndex(p, e) {
    if (!p.rh) {
      // a swimlane cell: count the cards whose middle is above the pointer
      let n = 0;
      for (let i = 0; i < p.cards.length; i++) { const el = p.vl.rowEl(i); if (el && el.getBoundingClientRect().top + el.offsetHeight / 2 < e.clientY) n = i + 1; }
      return n;
    }
    const r = p.body.getBoundingClientRect();
    const y = e.clientY - r.top + p.body.scrollTop;
    return Math.max(0, Math.min(p.cards.length, Math.round(y / (p.rh || 96))));
  }
  function clearDrop() {
    for (const p of S.panes) { if (p.drop) p.drop.hidden = true; if (p.el) p.el.classList.remove('drop'); }
    for (const z of main.querySelectorAll('.bd-zone.over')) z.classList.remove('over');
  }

  // While a card is dragged, a lane of several statuses splits into one labelled zone per status,
  // dimmed where the card's workflow has no transition.
  let catsP = null;
  const cats = new Map(); // status name -> category key
  function loadCats() {
    if (!catsP) {
      catsP = api.get('/projects/' + encodeURIComponent(S.project) + '/statuses')
        .then(l => { for (const t of l || []) for (const s of t.Statuses || []) cats.set(s.Name, s.Category); })
        .catch(() => { catsP = null; });
    }
    return catsP;
  }
  const trsCache = new Map(); // issue key -> Promise of its transitions
  offs.push(bus.on('issue:changed', ({ key }) => trsCache.delete(key)));
  function transitionsOf(key) {
    if (!trsCache.has(key)) trsCache.set(key, api.get('/issues/' + key + '/transitions').catch(() => { trsCache.delete(key); return null; }));
    return trsCache.get(key);
  }
  function clearZones() { for (const z of main.querySelectorAll('.bd-zones')) z.remove(); }
  async function showZones(key) {
    const card = S.cards.find(c => c.Key === key);
    const multi = S.mode === 'lanes' ? S.panes.filter(p => { const c = columns()[p.col]; return c && (c.StatusIDs || []).length > 1; }) : [];
    if (!card || !multi.length) return;
    const [trs] = await Promise.all([transitionsOf(key), loadCats()]);
    if (S.drag !== key) return;
    const names = S.bundle.statusNames || {};
    const can = trs && new Set(trs.flatMap(t => [String(t.StatusID), t.Name]));
    const last = columns().length - 1;
    clearZones();
    for (const p of multi) {
      const zs = h('div.bd-zones');
      for (const id of columns()[p.col].StatusIDs) {
        const name = names[id] || id, cur = String(card.StatusID) === String(id);
        const ok = !trs || can.has(String(id)) || can.has(name);
        const cat = cats.get(name) || (p.col === 0 ? 'new' : p.col === last ? 'done' : 'indeterminate');
        zs.append(h('div.bd-zone.cat-' + cat + (cur ? '.cur' : ok ? '' : '.no'), { dataset: { status: id }, title: cur ? 'Current status' : ok ? 'Move to ' + name : 'No transition from ' + card.Status + ' to ' + name },
          h('span.bd-zone-name', name), cur ? h('span.bd-zone-tag', 'current') : ok ? '' : h('span.bd-zone-tag', 'no transition')));
      }
      p.el.insertBefore(zs, p.body);
    }
  }
  main.addEventListener('dragover', e => {
    const p = S.drag && paneOfEl(e.target);
    if (!p) return;
    const z = e.target.closest && e.target.closest('.bd-zone');
    if (z) {
      clearDrop();
      if (z.classList.contains('no') || z.classList.contains('cur')) return;
      e.preventDefault(); e.dataTransfer.dropEffect = 'move';
      z.classList.add('over'); p.el.classList.add('drop');
      return;
    }
    e.preventDefault(); e.dataTransfer.dropEffect = 'move';
    clearDrop();
    if (S.mode === 'lanes') p.el.classList.add('drop');
    if (p.drop) { p.drop.hidden = false; p.drop.style.top = (dropIndex(p, e) * p.rh - 3) + 'px'; }
  });
  main.addEventListener('dragleave', e => { if (!main.contains(e.relatedTarget)) clearDrop(); });
  main.addEventListener('dragend', () => { S.drag = null; clearDrop(); clearZones(); for (const d of main.querySelectorAll('.dragging')) d.classList.remove('dragging'); });
  main.addEventListener('drop', e => {
    const p = S.drag && paneOfEl(e.target);
    if (!p) return;
    e.preventDefault();
    const z = e.target.closest && e.target.closest('.bd-zone');
    const key = S.drag, idx = z ? 0 : dropIndex(p, e);
    S.drag = null; clearDrop(); clearZones();
    if (z) { const card = S.cards.find(c => c.Key === key); if (card) moveCol(card, p.col, null, z.dataset.status); return; }
    dropCard(key, p, idx);
  });

  function dropCard(key, pane, idx) {
    const card = S.cards.find(c => c.Key === key); if (!card) return;
    const from = S.where.get(key);
    const same = from && from.p === pane;
    if (swimming()) { if (same) return ui.toast('Ranking needs the swimlanes off  (O)'); return moveCol(card, pane.col, null); }
    if (same && (idx === from.i || idx === from.i + 1)) return;
    const L = pane.cards.filter(c => c.Key !== key);
    let k = idx; if (same && from.i < idx) k--;
    const at = k < L.length ? { other: L[k].Key, after: false } : L.length ? { other: L[L.length - 1].Key, after: true } : null;
    if (same) return rankTo(card, at);
    moveCol(card, pane.col, at);
  }

  // ---- writes (optimistic, rolled back with a toast)
  function replaceCard(card, patch) {
    const next = { ...card, ...patch };
    S.cards = S.cards.map(c => (c === card ? next : c));
    return next;
  }
  function reorder(card, at) {
    const rest = S.cards.filter(c => c.Key !== card.Key);
    let i = at ? rest.findIndex(c => c.Key === at.other) : -1;
    if (i < 0) rest.push(card); else rest.splice(at.after ? i + 1 : i, 0, card);
    S.cards = rest;
  }
  const writable = () => { if (S.past) { ui.toast('The time machine only looks  ·  esc back to now'); return false; } return true; };
  const pushUndo = (what, run) => import('./fields.js').then(m => m.pushUndo(app, what, run));
  // Undo one failed move: only this card goes back, other moves made meanwhile stay.
  function revertCard(key, before) {
    const orig = before.find(c => c.Key === key); if (!orig) return;
    const rest = S.cards.filter(c => c.Key !== key);
    const prev = before.slice(0, before.indexOf(orig)).reverse().find(c => rest.some(x => x.Key === c.Key));
    rest.splice(prev ? rest.findIndex(c => c.Key === prev.Key) + 1 : 0, 0, orig);
    S.cards = rest; layout();
  }
  async function rankTo(card, at) {
    if (!at) return;
    const before = S.cards, i = before.findIndex(c => c.Key === card.Key);
    const back = before[i + 1] ? { other: before[i + 1].Key, after: false } : before[i - 1] ? { other: before[i - 1].Key, after: true } : null;
    reorder(card, at); layout();
    try {
      await api.post('/issues/' + card.Key + '/rank', { Other: at.other, After: at.after });
      if (back) pushUndo(card.Key + "'s rank", async () => { await api.post('/issues/' + card.Key + '/rank', { Other: back.other, After: back.after }); bus.emit('issue:changed', { key: card.Key }); });
      bus.emit('issue:changed', { key: card.Key });
    } catch (e) { revertCard(card.Key, before); ui.errToast(e); }
  }
  async function moveCol(card, to, at, statusID) {
    const col = columns()[to];
    if (!col || !writable()) return;
    S.lastEdit = { what: '→ ' + col.Name, run: key => { const c = S.cards.find(x => x.Key === key); return c && moveCol(c, to, null); } };
    let trs;
    try { trs = await api.get('/issues/' + card.Key + '/transitions', { fresh: true }); } catch (e) { return ui.errToast(e); }
    const names = (col.StatusIDs || []).map(id => (S.bundle.statusNames || {})[id]);
    let opts = trs.filter(t => (col.StatusIDs || []).includes(String(t.StatusID)) || names.includes(t.Name));
    if (statusID) opts = opts.filter(t => String(t.StatusID) === String(statusID) || t.Name === (S.bundle.statusNames || {})[statusID]);
    else if (opts.length > 1) { const other = opts.filter(t => String(t.StatusID) !== String(card.StatusID)); if (other.length) opts = other; }
    if (!opts.length) return ui.toast('No transition from ' + card.Status + ' to ' + col.Name, { kind: 'err' });
    let t = opts[0];
    if (opts.length > 1) {
      t = await ui.pick({ title: card.Key + ' → ' + col.Name, items: opts, label: o => o.Name });
      if (!t) return;
    }
    const before = S.cards;
    const done = to === columns().length - 1;
    const next = replaceCard(card, { Status: t.Name, StatusID: String(t.StatusID || col.StatusIDs[0]), Done: done, InProgress: !done && to > 0 });
    if (at) reorder(next, at);
    layout();
    if (S.sel === card.Key) select(card.Key);
    try {
      await api.post('/issues/' + card.Key + '/transition', { ID: t.ID });
      if (at) await api.post('/issues/' + card.Key + '/rank', { Other: at.other, After: at.after }).catch(e => ui.errToast(e));
      bus.emit('issue:changed', { key: card.Key });
    } catch (e) {
      revertCard(card.Key, before); ui.toast('Could not move ' + card.Key + ': ' + e.message, { kind: 'err' });
    }
  }
  function stepCol(d) {
    const c = curCard(); if (!c) return;
    const to = colIndexOf(c) + d;
    if (to < 0 || to >= columns().length) return;
    moveCol(c, to, null);
  }
  // d is -1/1, or -Infinity/Infinity for the top/bottom. A list sorted by anything but rank still ranks in rank order.
  function stepRank(d) {
    if (!writable()) return;
    if (swimming()) return ui.toast('Ranking needs the swimlanes off  (O)');
    const w = cur(), c = curCard(); if (!c) return;
    const L = S.mode === 'list' && !listRanks() ? S.base.filter(passes) : w.p.cards;
    const i = L.findIndex(x => x.Key === c.Key);
    const j = Math.max(0, Math.min(L.length - 1, i + (Number.isFinite(d) ? d : d * L.length)));
    if (j === i) return ui.toast(c.Key + (d < 0 ? ' is ranked first already' : ' is ranked last already'));
    rankTo(c, { other: L[j].Key, after: j > i });
  }

  // ---- repeat the last change (.) on the selected card
  offs.push(bus.on('issue:patch', ({ key, field, value }) => {
    if (field === 'summary' || value === undefined) return;
    S.lastEdit = { what: field + ' → ' + (value && value.DisplayName ? value.DisplayName : Array.isArray(value) ? value.join(' ') : value == null || value === '' ? 'none' : value), run: key2 => repeatField(key2, field, value) };
  }));
  async function repeatField(key, field, value) {
    let body;
    switch (field) {
      case 'status': body = { To: value }; break;
      case 'priority': {
        const p = (await api.get('/priorities')).find(x => x.Name === value);
        if (!p) return ui.toast('No priority ' + value, { kind: 'err' });
        body = { ID: p.ID }; break;
      }
      case 'assignee': body = { ID: value ? value.AccountID : '' }; break;
      case 'points': case 'duedate': body = { Text: String(value == null ? '' : value) }; break;
      case 'labels': body = { Labels: value }; break;
      case 'flag': body = { On: !!value }; break;
      default: return ui.toast('Cannot repeat ' + field);
    }
    try {
      const r = await api.put('/issues/' + key + '/field/' + field, body);
      bus.emit('issue:changed', { key });
      const u = r && r.Undo;
      if (u) pushUndo(key + ' ' + field, async () => { await api.put('/issues/' + key + '/field/' + u.Field, u); bus.emit('issue:changed', { key }); });
      ui.toast(key + ' ' + field + ' repeated', { kind: 'ok' });
    } catch (e) { ui.errToast(e); bus.emit('issue:changed', { key }); }
  }
  function repeat() {
    const c = curCard();
    if (!S.lastEdit) return ui.toast('Nothing to repeat yet');
    if (!c) return ui.toast('Select a card first');
    if (!writable()) return;
    ui.toast('Repeating ' + S.lastEdit.what + ' on ' + c.Key + '…');
    S.lastEdit.run(c.Key);
  }

  // ---- filters
  function toggleQF(id) { S.qf.has(id) ? S.qf.delete(id) : S.qf.add(id); renderBar(); S.path === cardsPath() || loadCards(); }
  function toggleMine() { S.mine = !S.mine; layout(); renderBar(); }
  function clearFilters() {
    const reload = S.qf.size > 0;
    S.mine = false; S.who = null; S.qf.clear(); S.text = ''; S.textFn = null; filterIn.value = '';
    layout(); renderBar();
    if (reload) loadCards();
  }
  async function pickWho() {
    const seen = new Map();
    for (const c of S.cards) if (who(c)) seen.set(who(c), c.Assignee);
    const show = () => { for (const [id, n] of S.people) if (!seen.has(id)) seen.set(id, n); };
    show(); await Promise.race([loadPeople(), new Promise(r => setTimeout(r, 400))]); show();
    const items = [{ id: null, name: 'Anyone' }, { id: '-', name: 'Unassigned' }, ...[...seen].map(([id, name]) => ({ id, name })).sort((a, b) => a.name.localeCompare(b.name))];
    const r = await ui.pick({ title: 'Assignee', items, label: i => i.name, render: i => h('span.pick-label', i.id ? ui.avatar(i.name, '', 18) : '', ' ', i.name) });
    if (!r) return;
    S.who = r.id; layout(); renderBar();
  }
  function setSort(col, dir) {
    S.sort = col; S.dir = dir;
    app.prefs.set('board.sort.' + S.board.ID, col + ':' + dir);
    if (S.mode !== 'list') setMode('list'); else { S.built = ''; layout(); }
  }
  function setMode(m) {
    if (m === S.mode) return;
    S.mode = m; S.built = '';
    app.prefs.set('board.mode.' + S.board.ID, m);
    renderToolbar(); layout();
    if (S.sel) { const w = S.where.get(S.sel); if (w) { w.p.vl.scrollTo(w.i, 'center'); w.p.vl.refresh(w.i); } }
  }

  // ---- swimlanes, fold, compact, list columns
  function setSwim(v) {
    S.swim = v; S.fold.clear(); S.built = '';
    if (S.board) app.prefs.set('board.swim.' + S.board.ID, v);
    if (S.mode !== 'lanes') return;
    layout();
    ui.toast(v === 'none' ? 'No swimlanes' : 'Swimlanes by ' + v);
  }
  function foldBand(name) {
    if (!swimming()) return ui.toast('Folding needs swimlanes  (O)');
    const w = cur(), g = name != null ? name : w && groupName(w.p.cards[w.i]);
    if (g == null) return;
    S.fold.has(g) ? S.fold.delete(g) : S.fold.add(g);
    const keep = S.sel;
    layout();
    if (keep && !S.where.has(keep)) {
      S.sel = null;
      const p = S.panes.find(q => q.cards.length && !q.folded);
      if (p) select(p.cards[0].Key);
    }
  }
  function unfoldAll() { S.fold.clear(); layout(); }
  function setCompact(v) { S.compact = v; app.prefs.set('board.compact', v ? '1' : '0'); layout(); }
  function setCols(list) {
    S.cols = ['mark', 'key', 'summary', ...list.filter(c => !['mark', 'key', 'summary'].includes(c))];
    app.prefs.set('board.cols', S.cols.join(','));
    S.built = ''; if (S.sort !== 'rank' && !S.cols.includes(S.sort)) { S.sort = 'rank'; S.dir = 1; }
    layout();
  }
  async function pickCols() {
    const items = allCols().filter(c => !['mark', 'key', 'summary'].includes(c));
    const r = await ui.pick({ title: 'List columns', items, multi: true, selected: items.filter(c => S.cols.includes(c)), label: colLabel, placeholder: 'Columns…' });
    if (r) setCols(allCols().filter(c => r.includes(c)));
  }

  // ---- filter builder, named filters
  const env = () => ({ me, pins: S.pins, now: Date.now() });
  function openBuilder() {
    if (!S.loaded) return;
    openFilterBuilder({ app, cards: S.base || S.cards, env: env(), query: S.text, apply: t => { filterIn.value = t; setText(t); } });
  }
  let filterCmds = [];
  function registerNamedFilters() {
    filterCmds.forEach(u => u()); filterCmds = [];
    ((app.session.ui && app.session.ui.Filters) || []).forEach((f, i) => {
      if (f.Name && f.Query) filterCmds.push(app.commands.register({ id: 'board:filter:' + i, title: 'Filter: ' + f.Name, group: 'Filters', run: () => { filterIn.value = f.Query; setText(f.Query); } }));
    });
  }

  // ---- time machine and closed sprints
  async function fetchMoves() {
    const p = S.past; if (!p) return;
    try {
      const r = await api.post('/cards/statusmoves', { Keys: S.cards.map(c => c.Key) });
      if (S.past !== p) return;
      p.moves = r || {}; p.loading = false;
    } catch (e) { if (S.past === p) S.past = null; ui.errToast(e); }
    layout();
  }
  function openPast() {
    if (S.past) return;
    if (!S.loaded) return;
    S.past = { days: 1, at: 0, moves: null, loading: true, fetching: true };
    renderBanner(); fetchMoves();
  }
  function stepPast(d) {
    const p = S.past;
    if (!p) return false;
    if (p.loading) { ui.toast('Still reading the history  ·  esc cancels'); return true; }
    if (p.at) { ui.toast('A closed sprint shows as it closed  ·  esc leaves it'); return true; }
    p.days = Math.min(p.days + d, 90);
    if (p.days <= 0) leavePast(); else layout();
    return true;
  }
  function leavePast() {
    if (S.closed) return leaveClosed();
    S.past = null; layout();
  }
  async function pickClosed() {
    if (!isScrum()) return ui.toast("Closed sprints are a scrum board's");
    let list;
    try { list = (await api.get('/boards/' + S.board.ID + '?closed=1')).closedSprints || []; } catch (e) { return ui.errToast(e); }
    if (!list.length) return ui.toast('No closed sprints');
    const closedOn = s => (!isZero(s.Complete) ? s.Complete : s.End);
    const r = await ui.pick({ title: 'Closed sprints', items: list, label: s => s.Name, detail: s => [isZero(closedOn(s)) ? '' : 'closed ' + shortDate(closedOn(s)), s.Goal].filter(Boolean).join('  ·  ') });
    if (r) openClosed(r, closedOn(r));
  }
  function openClosed(sp, closedAt) {
    if (!S.closed) S.closedFrom = S.scope;
    S.closed = sp;
    S.past = { days: 0, at: isZero(closedAt) ? Date.now() : Date.parse(closedAt), moves: null, loading: true };
    S.loaded = false; S.cards = []; S.path = '';
    renderToolbar(); renderBanner(); loadCards();
  }
  function leaveClosed() {
    const from = S.closedFrom || 'active';
    S.closed = null; S.past = null; S.path = '';
    setScope(from);
  }
  // What a closed sprint came to: done, and where the rest went.
  function closedLine() {
    let done = 0, carried = 0;
    const to = {};
    for (const c of S.cards) {
      if (c.Sprint) to[c.Sprint] = (to[c.Sprint] || 0) + 1;
      else if (c.Done) { done++; continue; } else to.backlog = (to.backlog || 0) + 1;
      carried++;
    }
    if (!carried) return done + ' done';
    return done + ' done  ·  ' + carried + ' carried over: ' + Object.keys(to).sort().map(k => to[k] + ' → ' + k).join(', ');
  }
  function renderBanner() {
    const p = S.past;
    banner.hidden = !p;
    if (!p) return;
    const day = t => new Date(t).toLocaleDateString(undefined, { weekday: 'short', day: 'numeric', month: 'short' });
    let t;
    if (S.closed) t = '⏲ ' + S.closed.Name + ', ' + (p.loading ? 'reading history…' : 'as it closed ' + day(p.at) + '  ·  ' + closedLine() + '  ·  esc leaves it');
    else t = p.loading ? '⏲ reading the board’s history…  ·  esc cancels' : '⏲ as of ' + day(asOf(p) - 60000) + '  ·  ← earlier  ·  → later  ·  esc back to now';
    banner.textContent = t;
  }

  // ---- pins (★, first in the palette), mark all, copy
  function loadPins() {
    let l = [];
    try { l = JSON.parse(app.prefs.get('pins', '[]')); } catch (e) { l = []; }
    S.pinList = Array.isArray(l) ? l.filter(p => Array.isArray(p) && p[0]) : [];
    S.pins = new Set(S.pinList.map(p => p[0]));
    if (S.textFn) S.textFn = cq.compile(S.text, { me, pins: S.pins });
  }
  function togglePin(c) {
    if (!c) return;
    const on = S.pins.has(c.Key);
    const next = on ? S.pinList.filter(p => p[0] !== c.Key) : [...S.pinList, [c.Key, c.Summary]];
    app.prefs.set('pins', JSON.stringify(next));
    loadPins(); registerPins(app);
    for (const p of S.panes) p.vl.refresh();
    ui.toast(on ? 'Unpinned ' + c.Key : 'Pinned ' + c.Key + '  ·  first in the palette');
  }
  function markAll() {
    const w = cur();
    const list = S.mode === 'list' ? S.visible : w ? w.p.cards : S.panes.reduce((a, p) => a.concat(p.cards), []);
    if (!list.length) return;
    const all = list.every(c => S.marks.has(c.Key));
    for (const c of list) all ? S.marks.delete(c.Key) : S.marks.add(c.Key);
    for (const p of S.panes) p.vl.refresh();
    ui.toast(all ? 'Cleared marks' : S.marks.size + ' marked  ·  X edits them  ·  esc clears');
  }
  const copy = (text, what) => (navigator.clipboard ? navigator.clipboard.writeText(text).then(() => ui.toast('Copied ' + what), ui.errToast) : ui.toast('No clipboard here', { kind: 'err' }));
  function copyTable() {
    const cards = S.visible.filter(c => S.marks.has(c.Key));
    const esc = x => String(x || '').replace(/\|/g, '\\|');
    const rows = cards.map(c => `| [${c.Key}](${app.session.baseURL}/browse/${c.Key}) | ${esc(c.Summary)} | ${esc(c.Status)} | ${esc(c.Assignee)} | ${esc(c.Points)} |`);
    copy(['| Key | Summary | Status | Assignee | Points |', '| --- | --- | --- | --- | --- |', ...rows].join('\n'), cards.length + ' rows as a table');
  }
  const slug = t => {
    const words = t.toLowerCase().split(/[^a-z0-9]+/).filter(Boolean);
    let out = '';
    for (const w of words) { const next = out ? out + '-' + w : w; if (next.length > 40 && out) break; out = next; }
    return out || 'work';
  };
  function copyBranch(c) {
    const tmpl = (app.session.ui && app.session.ui.BranchTemplate) || '{key}-{summary}';
    const name = tmpl.replace(/\{key\}/g, c.Key).replace(/\{summary\}/g, slug(c.Summary)).replace(/\{type\}/g, slug(c.Type || '')).replace(/\{project\}/g, c.Key.split('-')[0]);
    copy(name, name);
  }

  // ---- pickers
  async function pickBoard() {
    const b = await pickBoardOf(app, S.project, { scrum: false });
    if (b) app.go('/board/' + S.project + '/' + b.ID);
  }
  async function pickProjectCtx() {
    const p = await pickProject(app); if (!p) return;
    let b;
    try { b = await boardOf(app, p, { scrum: false }); } catch (e) { return ui.errToast(e); }
    if (b) app.go('/board/' + p + '/' + b.ID);
    else ui.toast('No boards in ' + p, { kind: 'err' });
  }
  async function pickSprint() {
    const items = viewItems();
    if (items.length < 2) return;
    const r = await ui.pick({ title: 'View', items, label: i => i.name, detail: i => i.info || '' });
    if (r) setScope(r.id);
  }
  function setScope(id) {
    if (S.past) leavePast(true);
    S.closed = null;
    S.scope = id;
    app.setQuery({ sprint: id === 'active' ? null : id });
    renderToolbar();
    loadCards();
  }
  function cycleScope(d) {
    const ids = viewItems().map(i => i.id);
    if (ids.length < 2) return;
    const r = resolveScope();
    const now = S.closed ? (S.closedFrom || ids[0]) : S.scope === 'active' && isScrum() ? String(r.sprint || 'all') : S.scope;
    const i = ids.indexOf(now);
    setScope(ids[(i + d + ids.length) % ids.length]);
  }

  // ---- actions
  const need = fn => () => { const c = curCard(); if (c) fn(c); else ui.toast('Select a card first'); };
  const needW = fn => need(c => { if (writable()) fn(c); });
  const edit = field => needW(c => app.actions.edit(c.Key, field, cardEl(c.Key)));
  const bulkKeys = () => (S.marks.size ? [...S.marks] : S.sel ? [S.sel] : []);
  const cycleO = () => {
    if (S.mode === 'lanes') setSwim(SWIMS[(SWIMS.indexOf(S.swim) + 1) % SWIMS.length]);
    else setSort(SORTS[(SORTS.indexOf(S.sort) + 1) % SORTS.length], 1);
  };
  const cycleSwim = () => setSwim(SWIMS[(SWIMS.indexOf(S.swim) + 1) % SWIMS.length]);

  function bindKeys() {
    const k = scope, G = 'Board', E = 'Board: edit', F = 'Board: filter', V = 'Board: view';
    k.bind(['j', 'ArrowDown'], () => move(0, 1), 'next card', { group: G });
    k.bind(['k', 'ArrowUp'], () => move(0, -1), 'previous card', { group: G });
    k.bind('h', () => move(-1, 0), 'previous lane', { group: G });
    k.bind('l', () => move(1, 0), 'next lane', { group: G });
    k.bind('ArrowLeft', () => (S.past ? stepPast(1) : move(-1, 0)), 'previous lane (time machine: a day earlier)', { group: G, hidden: true });
    k.bind('ArrowRight', () => (S.past ? stepPast(-1) : move(1, 0)), 'next lane (time machine: a day later)', { group: G, hidden: true });
    k.bind('Home', () => edge(false), 'first card in lane', { group: G });
    k.bind('End', () => edge(true), 'last card in lane', { group: G });
    k.bind('PageDown', () => page(1), 'page down', { group: G, hidden: true });
    k.bind('PageUp', () => page(-1), 'page up', { group: G, hidden: true });
    k.bind('Enter', need(c => app.panel.open(c.Key)), 'open issue', { group: G });
    k.bind('Escape', () => {
      if (S.past) leavePast();
      else if (app.panel.key) app.panel.close();
      else if (S.marks.size) { const ks = [...S.marks]; S.marks.clear(); ks.forEach(rebind); }
      else if (S.sel) { const o = S.sel; S.sel = null; rebind(o); }
    }, 'leave time machine / close panel / clear selection', { group: G });
    k.bind('s', needW(c => app.actions.transition(c.Key)), 'change status', { group: E });
    k.bind('e', edit('summary'), 'edit summary', { group: E });
    k.bind('a', edit('assignee'), 'assign', { group: E });
    k.bind('p', edit('priority'), 'set priority', { group: E });
    k.bind('P', edit('points'), 'set points', { group: E });
    k.bind('H', () => { if (writable()) stepCol(-1); }, 'move card to previous column', { group: E });
    k.bind('L', () => { if (writable()) stepCol(1); }, 'move card to next column', { group: E });
    k.bind('J', () => stepRank(1), 'rank card down', { group: E });
    k.bind('K', () => stepRank(-1), 'rank card up', { group: E });
    k.bind('alt+j', () => stepRank(Infinity), 'rank card to the bottom', { group: E });
    k.bind('alt+k', () => stepRank(-Infinity), 'rank card to the top', { group: E });
    k.bind('.', repeat, 'repeat the last change on this card', { group: E });
    k.bind('x', () => { toggleMark(S.sel); move(0, 1); }, 'mark card (multi-select)', { group: E });
    k.bind('ctrl+a', markAll, 'mark all in the lane / list', { group: E });
    k.bind('X', () => { if (!writable()) return; const ks = bulkKeys(); if (ks.length) app.actions.bulk(ks); }, 'bulk edit marked cards', { group: E });
    k.bind('n', () => app.actions.create({ project: S.project }), 'new issue', { group: E });
    k.bind('*', need(togglePin), 'pin / unpin issue (first in the palette)', { group: G });
    k.bind('o', need(c => window.open(app.session.baseURL + '/browse/' + c.Key, '_blank', 'noopener')), 'open in Jira', { group: G });
    k.bind('y', () => { if (S.marks.size) return copyTable(); const c = curCard(); if (c) copy(c.Key, c.Key); else ui.toast('Select a card first'); }, 'copy key (marked cards: as a table)', { group: G });
    k.bind('Y', need(c => copy(app.session.baseURL + '/browse/' + c.Key, 'link')), 'copy link', { group: G });
    k.bind('ctrl+y', need(copyBranch), 'copy branch name', { group: G });
    k.bind('r', () => refresh(true), 'refresh', { group: G });
    k.bind('t', () => setMode(S.mode === 'lanes' ? 'list' : 'lanes'), 'lanes / list', { group: V });
    k.bind('O', cycleO, 'lanes: cycle swimlanes · list: cycle sort', { group: V });
    k.bind('z', () => foldBand(), 'fold the swimlane', { group: V });
    k.bind('Z', unfoldAll, 'unfold all swimlanes', { group: V });
    k.bind('c', () => setCompact(!S.compact), 'one-line cards', { group: V });
    k.bind('C', pickCols, 'list columns', { group: V });
    k.bind('alt+t', openPast, 'time machine: the board on earlier days', { group: V });
    k.bind('alt+o', pickClosed, 'closed sprints: one as it ended', { group: V });
    k.bind('B', () => pickBoard(), 'switch board (same project)', { group: V });
    k.bind('alt+p', () => pickProjectCtx(), 'switch project (its last board)', { group: V });
    k.bind('v', () => pickSprint(), 'pick a view: sprint, backlog, your views', { group: V });
    k.bind('[', () => cycleScope(-1), 'previous view', { group: V });
    k.bind(']', () => cycleScope(1), 'next view', { group: V });
    k.bind('f', () => { filterIn.focus(); filterIn.select(); }, 'filter cards', { group: F });
    k.bind('F', openBuilder, 'filter builder', { group: F });
    k.bind('m', () => toggleMine(), 'only my cards', { group: F });
    k.bind('A', () => pickWho(), 'filter by assignee', { group: F });
    k.bind('0', () => clearFilters(), 'clear filters', { group: F });
    for (let i = 1; i <= 9; i++) k.bind(String(i), () => { const q = qfs()[i - 1]; if (q) toggleQF(q.ID); }, i === 1 ? 'toggle quick filter 1-9' : '', { group: F, hidden: i > 1 });
  }

  let qfCmds = [];
  function registerQFCommands() {
    qfCmds.forEach(u => u()); qfCmds = [];
    for (const q of qfs()) qfCmds.push(app.commands.register({ id: 'board:qf:' + q.ID, title: 'Board: toggle quick filter ' + q.Name, group: 'Board', run: () => toggleQF(q.ID) }));
  }
  function registerCommands() {
    const C = (id, title, run) => unreg.push(app.commands.register({ id: 'board:' + id, title: 'Board: ' + title, group: 'Board', run }));
    C('lanes', 'show lanes', () => setMode('lanes'));
    C('list', 'show list', () => setMode('list'));
    C('toggle', 'toggle lanes / list', () => setMode(S.mode === 'lanes' ? 'list' : 'lanes'));
    C('refresh', 'refresh', () => refresh(true));
    C('mine', 'only my cards', toggleMine);
    C('who', 'filter by assignee', pickWho);
    C('clear', 'clear filters', clearFilters);
    C('filter', 'filter cards', () => { filterIn.focus(); filterIn.select(); });
    C('switch', 'switch board', pickBoard);
    C('project', 'switch project', pickProjectCtx);
    C('sprint', 'pick a view: sprint, backlog, whole board', pickSprint);
    C('new', 'new issue', () => app.actions.create({ project: S.project }));
    for (const s of SORTS) C('sort:' + s, 'sort list by ' + s, () => setSort(s, 1));
    for (const v of SWIMS) C('swim:' + v, v === 'none' ? 'no swimlanes' : 'swimlanes by ' + v, () => { if (S.mode !== 'lanes') setMode('lanes'); setSwim(v); });
    C('fold', 'fold the swimlane', () => foldBand());
    C('unfold', 'unfold all swimlanes', unfoldAll);
    C('compact', 'one-line cards', () => setCompact(!S.compact));
    C('columns', 'list columns', pickCols);
    C('builder', 'filter builder', openBuilder);
    C('past', 'time machine: the board on earlier days', openPast);
    C('closed', 'closed sprints: one as it ended', pickClosed);
    C('pin', 'pin / unpin the selected issue', () => togglePin(curCard()));
    C('repeat', 'repeat the last change', repeat);
    C('top', 'rank to the top', () => stepRank(-Infinity));
    C('bottom', 'rank to the bottom', () => stepRank(Infinity));
    C('markall', 'mark all in the lane / list', markAll);
    C('branch', 'copy branch name', () => { const c = curCard(); if (c) copyBranch(c); });
    C('table', 'copy marked cards as a table', copyTable);
    registerNamedFilters();
    unreg.push(() => filterCmds.forEach(u => u()));
    unreg.push(() => qfCmds.forEach(u => u()));
  }

  // ---- lifecycle
  function touch() { lastInput = Date.now(); }
  document.addEventListener('keydown', touch, true);
  document.addEventListener('pointermove', touch, { passive: true });
  function armAuto() {
    const pr = app.prefs.get('board.refresh', ''); const every = pr !== '' ? Number(pr) * 1000 : parseEvery(app.session.ui && app.session.ui.AutoRefresh);
    if (!every) return;
    const tick = () => {
      if (S.dead) return;
      const idle = Date.now() - lastInput > 8000 && !document.hidden && !S.drag;
      if (idle) { refresh(false); autoT = later(tick, every); } else autoT = later(tick, 8000);
    };
    autoT = later(tick, every);
  }
  offs.push(bus.on('focus', () => { if (Date.now() - S.fetched > 30000) refresh(false); }));
  const onChanged = debounce(() => refresh(false), 250);
  offs.push(bus.on('issue:changed', onChanged));
  offs.push(bus.on('panel', ({ key }) => { app.setQuery({ issue: key || null }); }));

  function listCols() {
    const all = allCols(), want = app.prefs.get('board.cols', '');
    let cols;
    if (want) cols = want.split(',').filter(c => all.includes(c));
    else if (CF) cols = all.filter(c => ['mark', 'key', 'summary'].includes(c) || [...CF].some(f => FIELD_COL[f] === c) || CUSTOM.includes(c));
    else cols = all.filter(c => DEFAULT_COLS.includes(c) || CUSTOM.includes(c));
    return ['mark', 'key', 'summary', ...cols.filter(c => !['mark', 'key', 'summary'].includes(c))];
  }

  async function start() {
    bdMsg('Loading…');
    let project = params.project, bid = params.board ? Number(params.board) : 0;
    try {
      if (!project) project = lastProject(app) || (app.session.projects || [])[0];
      if (project && !bid) bid = lastBoard(app, project);
      if (!project) { const ps = await firstOf(api, '/projects'); project = ps[0] && ps[0].Key; }
      if (!project) return bdMsg(h('div', h('h2', 'No project'), h('p', 'Add one under jira.projects in the config.')));
      S.project = project;
      S.boards = await firstOf(api, '/projects/' + project + '/boards');
      S.board = S.boards.find(b => b.ID === bid) || S.boards[0];
      if (!S.board) return bdMsg(h('div', h('h2', 'No board in ' + project)));
    } catch (e) { return fail(e); }
    if (S.dead) return;
    const canon = '#/board/' + S.project + '/' + S.board.ID;
    if (!location.hash.startsWith(canon)) { const q = location.hash.split('?')[1]; history.replaceState(null, '', canon + (q ? '?' + q : '')); }
    if (app.route) { app.route.params.project = S.project; app.route.params.board = String(S.board.ID); }
    setCtx(app, S.project, S.board);
    const dm = app.session.ui && app.session.ui.DefaultMode;
    S.mode = app.prefs.get('board.mode.' + S.board.ID, app.prefs.get('board.mode', dm === 'list' ? 'list' : 'lanes')) === 'list' ? 'list' : 'lanes';
    const [sort, dir] = String(app.prefs.get('board.sort.' + S.board.ID, 'rank:1')).split(':');
    S.sort = SORTS.includes(sort) || allCols().includes(sort) ? sort : 'rank'; S.dir = dir === '-1' ? -1 : 1;
    const sw = app.prefs.get('board.swim.' + S.board.ID, 'none'); S.swim = SWIMS.includes(sw) ? sw : 'none';
    S.compact = app.prefs.get('board.compact', '0') === '1';
    S.cols = listCols();
    loadPins(); registerPins(app);
    renderToolbar(); renderBar();
    const savedP = UI.SavedFilters === 'off' ? Promise.resolve() : new Promise(res => {
      api.swr('/filters/favourite', d => { S.saved = d || []; renderToolbar(); res(); }).catch(() => res());
    });
    if (S.scope.startsWith('filter:')) await savedP;
    if (S.dead) return;
    if (!isScrum()) loadCards(); // kanban: no sprints to wait for
    setBusy(1);
    api.swr('/boards/' + S.board.ID, d => { if (!S.dead) setBundle(d); })
      .catch(fail).finally(() => setBusy(-1));
    if (query.issue) {
      const t0 = Date.now();
      const pickIssue = () => {
        if (S.where.has(query.issue)) { select(query.issue); app.panel.open(query.issue); }
        else if (!S.dead && Date.now() - t0 < 8000) later(pickIssue, 150);
      };
      later(pickIssue, 100);
    }
    armAuto();
  }

  bindKeys();
  registerCommands();
  start();

  // A console handle for testing: laneway.boardView.synth(1500).
  app.boardView = {
    state: S,
    setCards(cards) { S.cards = cards; S.total = cards.length; S.loaded = true; layout(); renderBar(); },
    synth(n = 1500) {
      const cols = columns(), base = S.cards.length ? S.cards : [{ Key: 'X-1', Summary: 's', Type: 'Task', Status: 'To Do', StatusID: '1', Priority: 'Medium' }];
      const out = [];
      for (let i = 0; i < n; i++) {
        const b = base[i % base.length], col = cols[i % Math.max(1, cols.length)];
        out.push({ ...b, Key: 'SYN-' + (i + 1), Summary: 'Synthetic card ' + (i + 1) + ' ' + b.Summary, StatusID: col ? col.StatusIDs[0] : b.StatusID, Status: col ? col.Name : b.Status, Points: String(1 + (i % 8)) });
      }
      this.setCards(out);
    },
    layout,
  };

  return () => {
    S.dead = true;
    for (const t of timers) clearTimeout(t);
    offs.forEach(f => f()); unreg.forEach(f => f());
    document.removeEventListener('keydown', touch, true);
    document.removeEventListener('pointermove', touch);
    for (const p of S.panes) p.vl.destroy();
    delete app.boardView;
  };
}
