// The board: swim lanes by column or a sortable list, with filters, keyboard
// navigation, drag and drop, and quiet refreshes. Cards render through lib/vlist.js
// so a lane or list of thousands stays smooth.
import { h, clear, delegate, debounce } from '../lib/dom.js';
import { css } from '../lib/css.js';
import { vlist } from '../lib/vlist.js';
import { isZero, date, shortDate, ago } from '../lib/fmt.js';

css('board');

const DAY = 864e5;
const PRIO_ORD = { blocker: 0, highest: 0, critical: 0, high: 1, major: 1, medium: 2, normal: 2, low: 3, minor: 3, lowest: 4, trivial: 4 };
const PRIO_GLYPH = ['▲▲', '▲', '●', '▼', '▼▼'];
const TYPE_CLS = { bug: 't-bug', story: 't-story', task: 't-task', epic: 't-epic', subtask: 't-sub', 'sub-task': 't-sub' };
const PR_TEXT = { OPEN: '⇄ open', MERGED: '✓ merged', DECLINED: '✕ declined' };
const SORTS = ['rank', 'priority', 'points', 'assignee', 'key', 'status', 'updated', 'due', 'created'];
const LIST_COLS = [
  ['', ''], ['key', 'Key'], ['summary', 'Summary'], ['status', 'Status'], ['priority', 'Prio'], ['points', 'Pts'],
  ['assignee', 'Assignee'], ['due', 'Due'], ['updated', 'Updated'],
];

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

// ---- text filter: plain words, field:value, -negation, is:flag
const FIELDS = {
  status: c => c.Status, assignee: c => c.Assignee, type: c => c.Type, label: c => c.Labels, labels: c => c.Labels,
  prio: c => c.Priority, priority: c => c.Priority, epic: c => c.ParentKey + ' ' + c.ParentSummary, parent: c => c.ParentKey + ' ' + c.ParentSummary,
  key: c => c.Key, sprint: c => c.Sprint, reporter: c => c.Reporter, component: c => c.Components,
};
const hay = new WeakMap();
function haystack(c) {
  let v = hay.get(c);
  if (v === undefined) {
    v = [c.Key, c.Summary, c.Assignee, c.ParentKey, c.ParentSummary, c.Labels, c.Status, c.Type].join(' ').toLowerCase();
    hay.set(c, v);
  }
  return v;
}
function compileText(text, me) {
  const out = [];
  for (let tok of text.trim().toLowerCase().split(/\s+/)) {
    if (!tok) continue;
    const neg = tok.length > 1 && tok[0] === '-';
    if (neg) tok = tok.slice(1);
    const i = tok.indexOf(':');
    let fn;
    if (i > 0 && tok.slice(0, i) === 'is') {
      const f = tok.slice(i + 1), today = startOfToday();
      fn = { flagged: c => c.Flagged, done: c => c.Done, open: c => !c.Done, inprogress: c => c.InProgress, mine: c => !!me && c.AssigneeID === me,
        unassigned: c => !who(c), overdue: c => !c.Done && time(c.Due) < today, blocked: c => c.Flagged }[f];
      if (!fn) continue;
    } else if (i > 0 && FIELDS[tok.slice(0, i)]) {
      const get = FIELDS[tok.slice(0, i)], vals = tok.slice(i + 1).split(',').filter(Boolean);
      if (!vals.length) continue;
      fn = c => { const s = String(get(c) || '').toLowerCase(); return vals.some(v => s.includes(v)); };
    } else {
      fn = c => haystack(c).includes(tok);
    }
    out.push(neg ? c => !fn(c) : fn);
  }
  return out;
}

export default function mount(el, { app, params, query, scope, toolbar }) {
  const { api, bus, ui } = app;
  const me = (app.session.me && app.session.me.AccountID) || '';
  const S = {
    project: '', boards: [], board: null, bundle: null,
    cards: [], total: 0, loaded: false, path: '', fetched: 0, busy: 0,
    scope: query.sprint || 'active',
    qf: new Set(), mine: false, who: null, text: '', textFns: [],
    mode: 'lanes', sort: 'rank', dir: 1,
    sel: null, marks: new Set(), rowMem: 0,
    panes: [], where: new Map(), visible: [], built: '', rowH: 0,
    drag: null, dead: false,
  };
  const timers = new Set();
  const later = (fn, ms) => { const t = setTimeout(() => { timers.delete(t); fn(); }, ms); timers.add(t); return t; };
  const offs = [], unreg = [];
  let lastInput = Date.now(), autoT = 0;

  // ---- skeleton
  const filterIn = h('input.input.bd-filter', { type: 'text', placeholder: 'Filter  (f)', spellcheck: false, autocomplete: 'off', title: 'words, status:review, assignee:ada, label:ui, is:flagged, -negate' });
  const chips = h('div.bd-chips');
  const stats = h('span.bd-stats.dim');
  const bar = h('div.bd-bar', chips, h('span.sp'), filterIn, stats);
  const main = h('div.bd-main');
  const root = h('div.bd', bar, main);
  el.append(root);
  const bdMsg = text => clear(main).append(h('div.empty', text));

  // ---- toolbar
  const boardBtn = h('button.btn', { title: 'Project and board  (B)', onclick: () => pickBoard() });
  const sprintBtn = h('button.btn', { title: 'Sprint, backlog or whole board  (v)', onclick: () => pickSprint() });
  const modeBtn = h('button.btn', { title: 'Lanes / list  (t)', onclick: () => setMode(S.mode === 'lanes' ? 'list' : 'lanes') });
  const refreshBtn = h('button.btn.ghost.bd-refresh', { title: 'Refresh  (r)', onclick: () => refresh(true) }, '⟳');
  toolbar.append(boardBtn, sprintBtn, h('span.spacer'), modeBtn, refreshBtn);

  function renderToolbar() {
    boardBtn.textContent = (S.project || '…') + (S.board ? ' › ' + S.board.Name : '') + ' ▾';
    const sp = scopeLabel();
    sprintBtn.textContent = sp + ' ▾';
    sprintBtn.hidden = !isScrum();
    modeBtn.textContent = S.mode === 'lanes' ? '▥ Lanes' : '☰ List';
  }
  const isScrum = () => !!S.board && S.board.Type !== 'kanban';
  const sprints = () => (S.bundle && S.bundle.sprints) || [];
  function scopeLabel() {
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
    qfs().slice(0, 9).forEach((q, i) => kids.push(h('button.fchip' + (S.qf.has(q.ID) ? '.on' : ''), { dataset: { qf: q.ID }, title: q.JQL }, h('kbd', i + 1), q.Name)));
    kids.push(h('button.fchip' + (S.mine ? '.on' : ''), { dataset: { act: 'mine' }, title: 'Assigned to me  (m)' }, h('kbd', 'm'), 'Mine'));
    kids.push(h('button.fchip' + (S.who != null ? '.on' : ''), { dataset: { act: 'who' }, title: 'Assignee  (A)' }, h('kbd', 'A'), S.who == null ? 'Assignee' : whoName(S.who)));
    if (anyFilter()) kids.push(h('button.fchip.clear', { dataset: { act: 'clear' }, title: 'Clear filters  (0)' }, '✕ clear'));
    clear(chips).append(...kids);
  }
  const whoName = id => {
    if (id === '-') return 'Unassigned';
    const c = S.cards.find(c => who(c) === id);
    return c ? c.Assignee : id;
  };
  delegate(chips, 'click', 'button', (e, b) => {
    if (b.dataset.qf) toggleQF(Number(b.dataset.qf));
    else if (b.dataset.act === 'mine') toggleMine();
    else if (b.dataset.act === 'who') pickWho();
    else clearFilters();
  });
  const onText = debounce(() => { S.text = filterIn.value; S.textFns = compileText(S.text, me); layout(); renderBar(); }, 70);
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
    if (jql) p.set('jql', jql);
    const s = p.toString();
    return '/boards/' + S.board.ID + '/cards' + (s ? '?' + s : '');
  }
  function setBusy(d) { S.busy += d; refreshBtn.classList.toggle('spin', S.busy > 0); }

  // Show cached data at once, then the fresh answer.
  function loadCards() {
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
    for (const f of S.textFns) if (!f(c)) return false;
    return true;
  }
  const CMP = {
    key: (a, b) => a.Key.localeCompare(b.Key, undefined, { numeric: true }),
    summary: (a, b) => a.Summary.localeCompare(b.Summary),
    status: (a, b) => colIndexOf(a) - colIndexOf(b),
    priority: (a, b) => prioOrd(a) - prioOrd(b),
    points: (a, b) => num(b) - num(a),
    assignee: (a, b) => (!a.Assignee) - (!b.Assignee) || a.Assignee.localeCompare(b.Assignee),
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

  function layout() {
    if (!S.bundle || !S.loaded || S.dead) return;
    sc = statusCol();
    const cols = columns(), vis = S.cards.filter(passes);
    S.visible = vis;
    const lanes = S.mode === 'lanes';
    const other = lanes && S.cards.some(c => !sc.has(String(c.StatusID)));
    const sig = S.mode + '|' + cols.map(c => c.Name).join(',') + (other ? '|other' : '');
    if (sig !== S.built) build(sig, lanes, cols, other);
    if (lanes) {
      for (const p of S.panes) { p.cards = []; p.total = 0; }
      for (const c of S.cards) S.panes[Math.min(colIndexOf(c), S.panes.length - 1)].total++;
      for (const c of vis) S.panes[Math.min(colIndexOf(c), S.panes.length - 1)].cards.push(c);
    } else {
      let l = vis;
      if (S.sort !== 'rank') { const f = CMP[S.sort]; l = vis.slice().sort((a, b) => f(a, b) * S.dir); }
      else if (S.dir < 0) l = vis.slice().reverse();
      S.panes[0].cards = l; S.panes[0].total = S.cards.length;
    }
    S.where.clear();
    for (const p of S.panes) p.cards.forEach((c, i) => S.where.set(c.Key, { p, i }));
    for (const k of [...S.marks]) if (!S.where.has(k)) S.marks.delete(k);
    if (S.sel && !S.where.has(S.sel)) S.sel = null;
    const rh = probeHeight(lanes);
    for (const p of S.panes) {
      if (rh !== p.rh) { p.rh = rh; p.vl.setRowHeight(rh); }
      p.vl.setCount(p.cards.length);
      paintHead(p);
    }
    renderStats();
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

  // ---- building the lanes or list
  function build(sig, lanes, cols, other) {
    for (const p of S.panes) p.vl.destroy();
    S.panes = []; S.built = sig; clear(main);
    if (lanes) {
      const wrap = h('div.bd-lanes');
      const names = cols.map((c, i) => ({ name: c.Name, max: c.Max || 0, col: i })).concat(other ? [{ name: 'Other', max: 0, col: cols.length }] : []);
      names.forEach((n, i) => {
        const pane = { col: n.col, name: n.name, max: n.max, cards: [], total: 0, rh: 0 };
        pane.count = h('span.bd-count');
        pane.wip = h('span.bd-wip');
        pane.head = h('div.bd-lane-head.k' + (i === 0 ? 'new' : i === names.length - 1 && !other ? 'done' : 'prog'), h('span.bd-lane-name', n.name), pane.count, pane.wip);
        pane.body = h('div.bd-lane-body');
        pane.drop = h('div.bd-drop', { hidden: true });
        pane.body.append(pane.drop);
        pane.el = h('section.bd-lane', { dataset: { pane: i } }, pane.head, pane.body);
        pane.vl = vlist(pane.body, { rowHeight: 96, create: buildCard, bind: (w, j) => fillCard(w, pane.cards[j]) });
        S.panes.push(pane);
        wrap.append(pane.el);
      });
      main.append(wrap);
    } else {
      const pane = { col: -1, name: 'list', cards: [], total: 0, rh: 0 };
      pane.head = h('div.bd-lhead', LIST_COLS.map(([k, label]) => h('span', { class: 'lh-' + (k || 'mark'), dataset: { sort: k } }, label, S.sort === k ? (S.dir > 0 ? ' ▲' : ' ▼') : '')));
      pane.body = h('div.bd-lbody');
      pane.vl = vlist(pane.body, { rowHeight: 32, create: buildRow, bind: (w, j) => fillRow(w, pane.cards[j]) });
      S.panes.push(pane);
      main.append(h('div.bd-list', pane.head, pane.body));
    }
  }
  function paintHead(p) {
    if (S.mode !== 'lanes') {
      for (const s of p.head.children) s.textContent = (LIST_COLS.find(c => c[0] === s.dataset.sort) || ['', ''])[1] + (S.sort === s.dataset.sort ? (S.dir > 0 ? ' ▲' : ' ▼') : '');
      return;
    }
    const filtered = p.cards.length !== p.total;
    p.count.textContent = filtered ? p.cards.length + '/' + p.total : p.total;
    const over = p.max && p.total > p.max;
    p.wip.textContent = p.max ? 'max ' + p.max : '';
    p.head.classList.toggle('over', !!over);
    p.wip.title = over ? 'Over the WIP limit of ' + p.max : 'WIP limit';
    p.body.classList.toggle('empty', !p.cards.length);
  }

  // ---- cards
  function buildCard() {
    const r = {};
    const w = h('div.bcw', h('div.card', { draggable: true },
      h('div.c1', r.type = h('span.ctype'), r.key = h('span.ckey'), r.flag = h('span.cflag', { title: 'Flagged' }, '⚑'), h('span.sp'), r.prio = h('span.cprio'), r.pts = h('span.cpts')),
      r.sum = h('div.csum'),
      h('div.c3', r.parent = h('span.cparent'), r.sub = h('span.csub'), r.due = h('span.cdue'), r.pr = h('span.cpr'), r.dep = h('span.cdep'), r.labels = h('span.clabels'), h('span.sp'), r.age = h('span.cage'), r.av = h('span.cav'))));
    w._r = r;
    return w;
  }
  const hourTick = () => Math.floor(Date.now() / 36e5);
  const sigOf = c => [c.Key, c.Summary, c.Type, c.Status, c.Priority, c.AssigneeID, c.AvatarURL, c.Points, c.ParentKey, c.Subtasks, c.SubtasksDone, c.Due, c.Done, c.Flagged, c.InProgress, c.PR, c.Deploy, c.Labels, c.Updated, S.sel === c.Key, S.marks.has(c.Key), hourTick()].join('|');
  function ageText(c) {
    if (c.Done) return '';
    const t = !isZero(c.Since) ? c.Since : c.Created;
    if (isZero(t)) return '';
    const d = Math.floor((Date.now() - Date.parse(t)) / DAY);
    return d > 0 ? d + 'd' : Math.max(1, Math.floor((Date.now() - Date.parse(t)) / 36e5)) + 'h';
  }
  function setAvatar(slot, c) {
    const k = c.Assignee + '|' + c.AvatarURL;
    if (slot._k === k) return;
    slot._k = k;
    slot.replaceChildren(ui.avatar(c.Assignee, avatarURL(c.AvatarURL), 20));
  }
  function fillCard(w, c) {
    if (!c) return;
    const sig = sigOf(c);
    if (w._sig === sig) return;
    w._sig = sig;
    const r = w._r, card = w.firstChild;
    w.dataset.key = c.Key;
    card.className = 'card ' + catClass(c) + (c.Flagged ? ' flagged' : '') + (S.sel === c.Key ? ' sel' : '') + (S.marks.has(c.Key) ? ' mark' : '');
    r.type.className = 'ctype ' + (TYPE_CLS[(c.Type || '').toLowerCase()] || 't-other');
    r.type.textContent = (c.Type || '?')[0].toUpperCase(); r.type.title = c.Type;
    r.key.textContent = c.Key;
    r.flag.hidden = !c.Flagged;
    const po = prioOrd(c);
    r.prio.hidden = !c.Priority;
    r.prio.className = 'cprio p' + po; r.prio.textContent = po < 5 ? PRIO_GLYPH[po] : (c.Priority || '').slice(0, 3); r.prio.title = c.Priority;
    r.pts.hidden = c.Points === '' || c.Points == null; r.pts.textContent = c.Points;
    r.sum.textContent = c.Summary; r.sum.title = c.Summary;
    r.parent.hidden = !c.ParentKey; r.parent.textContent = c.ParentKey ? c.ParentSummary || c.ParentKey : '';
    r.parent.dataset.open = c.ParentKey || ''; r.parent.title = c.ParentKey ? c.ParentKey + ' ' + c.ParentSummary : '';
    r.sub.hidden = !c.Subtasks;
    if (c.Subtasks) { r.sub.textContent = c.SubtasksDone + '/' + c.Subtasks; r.sub.style.setProperty('--p', Math.round(100 * c.SubtasksDone / c.Subtasks) + '%'); r.sub.title = 'Subtasks done'; }
    const due = date(c.Due);
    r.due.hidden = !due;
    if (due) { r.due.textContent = shortDate(c.Due); r.due.className = 'cdue' + (!c.Done && due.getTime() < startOfToday() ? ' overdue' : ''); r.due.title = 'Due ' + due.toLocaleDateString(); }
    r.pr.hidden = !c.PR; r.pr.textContent = PR_TEXT[c.PR] || c.PR; r.pr.className = 'cpr pr-' + (c.PR || '').toLowerCase();
    r.dep.hidden = !c.Deploy; r.dep.textContent = '↑ ' + c.Deploy;
    const ls = c.Labels ? c.Labels.split(' ') : [];
    r.labels.hidden = !ls.length; r.labels.textContent = ls.slice(0, 2).map(l => '#' + l).join(' ') + (ls.length > 2 ? ' +' + (ls.length - 2) : ''); r.labels.title = c.Labels;
    const a = ageText(c);
    r.age.textContent = a; r.age.title = a ? 'In status since ' + ago(!isZero(c.Since) ? c.Since : c.Created) : '';
    setAvatar(r.av, c);
  }

  function buildRow() {
    const r = {};
    const w = h('div.lrow', { draggable: false },
      r.mark = h('span.l-mark'), r.key = h('span.l-key'), r.sum = h('span.l-sum'), r.status = h('span.l-status'), r.prio = h('span.cprio'),
      r.pts = h('span.l-pts'), r.who = h('span.l-who'), r.due = h('span.l-due'), r.upd = h('span.l-upd'));
    w._r = r;
    return w;
  }
  function fillRow(w, c) {
    if (!c) return;
    const sig = sigOf(c);
    if (w._sig === sig) return;
    w._sig = sig;
    const r = w._r;
    w.dataset.key = c.Key;
    w.className = 'vl-row lrow ' + catClass(c) + (S.sel === c.Key ? ' sel' : '') + (S.marks.has(c.Key) ? ' mark' : '') + (c.Flagged ? ' flagged' : '');
    r.mark.textContent = S.marks.has(c.Key) ? '☑' : c.Flagged ? '⚑' : '';
    r.key.textContent = c.Key;
    r.sum.textContent = c.Summary; r.sum.title = c.Summary;
    r.status.textContent = c.Status; r.status.className = 'l-status pill cat-' + (c.Done ? 'done' : c.InProgress ? 'indeterminate' : 'new');
    const po = prioOrd(c);
    r.prio.className = 'cprio p' + po; r.prio.textContent = po < 5 ? PRIO_GLYPH[po] : ''; r.prio.title = c.Priority;
    r.pts.textContent = c.Points;
    setAvatar(r.who, c);
    const due = date(c.Due);
    r.due.textContent = due ? shortDate(c.Due) : ''; r.due.className = 'l-due' + (due && !c.Done && due.getTime() < startOfToday() ? ' overdue' : '');
    r.upd.textContent = ago(c.Updated);
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
  function move(dx, dy) {
    const P = S.panes, w = cur();
    if (!w) {
      const p = P.find(p => p.cards.length);
      if (p) { S.rowMem = 0; select(p.cards[0].Key); }
      return;
    }
    if (dy) {
      const i = Math.max(0, Math.min(w.p.cards.length - 1, w.i + dy));
      return select(w.p.cards[i].Key);
    }
    if (dx && S.mode === 'lanes') {
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
  delegate(main, 'click', '[data-sort]', (e, t) => { if (t.dataset.sort) setSort(t.dataset.sort, S.sort === t.dataset.sort ? -S.dir : 1); });
  delegate(main, 'dblclick', '[data-key]', (e, t) => window.open(app.session.baseURL + '/browse/' + t.dataset.key, '_blank', 'noopener'));

  // ---- drag and drop
  const paneOfEl = t => { const s = t.closest && t.closest('.bd-lane'); return s ? S.panes[Number(s.dataset.pane)] : null; };
  main.addEventListener('dragstart', e => {
    const w = e.target.closest && e.target.closest('[data-key]');
    if (!w || S.mode !== 'lanes') return;
    S.drag = w.dataset.key;
    e.dataTransfer.effectAllowed = 'move';
    e.dataTransfer.setData('text/plain', S.drag);
    w.firstChild.classList.add('dragging');
  });
  function dropIndex(p, e) {
    const r = p.body.getBoundingClientRect();
    const y = e.clientY - r.top + p.body.scrollTop;
    return Math.max(0, Math.min(p.cards.length, Math.round(y / (p.rh || 96))));
  }
  function clearDrop() { for (const p of S.panes) { if (p.drop) p.drop.hidden = true; if (p.el) p.el.classList.remove('drop'); } }
  main.addEventListener('dragover', e => {
    const p = S.drag && paneOfEl(e.target);
    if (!p) return;
    e.preventDefault(); e.dataTransfer.dropEffect = 'move';
    clearDrop();
    p.el.classList.add('drop');
    p.drop.hidden = false;
    p.drop.style.top = (dropIndex(p, e) * p.rh - 3) + 'px';
  });
  main.addEventListener('dragleave', e => { if (!main.contains(e.relatedTarget)) clearDrop(); });
  main.addEventListener('dragend', () => { S.drag = null; clearDrop(); for (const d of main.querySelectorAll('.dragging')) d.classList.remove('dragging'); });
  main.addEventListener('drop', e => {
    const p = S.drag && paneOfEl(e.target);
    if (!p) return;
    e.preventDefault();
    const key = S.drag, idx = dropIndex(p, e);
    S.drag = null; clearDrop();
    dropCard(key, p, idx);
  });

  function dropCard(key, pane, idx) {
    const card = S.cards.find(c => c.Key === key); if (!card) return;
    const from = S.where.get(key);
    const same = from && from.p === pane;
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
  async function rankTo(card, at) {
    if (!at) return;
    const before = S.cards;
    reorder(card, at); layout();
    try {
      await api.post('/issues/' + card.Key + '/rank', { Other: at.other, After: at.after });
      bus.emit('issue:changed', { key: card.Key });
    } catch (e) { S.cards = before; layout(); ui.errToast(e); }
  }
  async function moveCol(card, to, at) {
    const col = columns()[to];
    if (!col) return;
    let trs;
    try { trs = await api.get('/issues/' + card.Key + '/transitions', { fresh: true }); } catch (e) { return ui.errToast(e); }
    const names = (col.StatusIDs || []).map(id => (S.bundle.statusNames || {})[id]);
    let opts = trs.filter(t => (col.StatusIDs || []).includes(String(t.StatusID)) || names.includes(t.Name));
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
      S.cards = before; layout(); ui.toast('Could not move ' + card.Key + ': ' + e.message, { kind: 'err' });
    }
  }
  function stepCol(d) {
    const c = curCard(); if (!c) return;
    const to = colIndexOf(c) + d;
    if (to < 0 || to >= columns().length) return;
    moveCol(c, to, null);
  }
  function stepRank(d) {
    const w = cur(); if (!w || S.mode === 'list' && S.sort !== 'rank') return;
    const other = w.p.cards[w.i + d];
    if (!other) return;
    rankTo(w.p.cards[w.i], { other: other.Key, after: d > 0 });
  }

  // ---- filters
  function toggleQF(id) { S.qf.has(id) ? S.qf.delete(id) : S.qf.add(id); renderBar(); S.path === cardsPath() || loadCards(); }
  function toggleMine() { S.mine = !S.mine; layout(); renderBar(); }
  function clearFilters() {
    const reload = S.qf.size > 0;
    S.mine = false; S.who = null; S.qf.clear(); S.text = ''; S.textFns = []; filterIn.value = '';
    layout(); renderBar();
    if (reload) loadCards();
  }
  async function pickWho() {
    const seen = new Map();
    for (const c of S.cards) if (who(c)) seen.set(who(c), c.Assignee);
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

  // ---- pickers
  async function pickBoard() {
    let projects = [];
    try { projects = await api.get('/projects'); } catch (e) { projects = []; }
    const mine = app.session.projects || [];
    const items = [...projects].sort((a, b) => (mine.includes(b.Key) - mine.includes(a.Key)));
    if (!items.length) mine.forEach(k => items.push({ Key: k, Name: k }));
    const p = await ui.pick({ title: 'Project', items, label: x => x.Key + ' ' + x.Name, detail: x => (mine.includes(x.Key) ? '★' : '') });
    if (!p) return;
    let boards;
    try { boards = await api.get('/projects/' + p.Key + '/boards'); } catch (e) { return ui.errToast(e); }
    if (!boards.length) return ui.toast('No boards in ' + p.Key, { kind: 'err' });
    const b = boards.length === 1 ? boards[0] : await ui.pick({ title: p.Key + ' board', items: boards, label: x => x.Name, detail: x => x.Type });
    if (b) app.go('/board/' + p.Key + '/' + b.ID);
  }
  async function pickSprint() {
    if (!isScrum()) return;
    const items = [...sprints().map(s => ({ id: String(s.ID), name: s.Name, info: s.State })), { id: 'backlog', name: 'Backlog', info: '' }, { id: 'all', name: 'Whole board', info: '' }];
    const r = await ui.pick({ title: 'Show', items, label: i => i.name, detail: i => i.info });
    if (r) setScope(r.id);
  }
  function setScope(id) {
    S.scope = id;
    app.setQuery({ sprint: id === 'active' ? null : id });
    renderToolbar();
    loadCards();
  }
  function cycleScope(d) {
    if (!isScrum()) return;
    const ids = [...sprints().map(s => String(s.ID)), 'backlog', 'all'];
    const r = resolveScope();
    const now = S.scope === 'active' ? String(r.sprint || 'all') : S.scope;
    const i = ids.indexOf(now);
    setScope(ids[(i + d + ids.length) % ids.length]);
  }

  // ---- actions
  const need = fn => () => { const c = curCard(); if (c) fn(c); else ui.toast('Select a card first'); };
  const edit = field => need(c => app.actions.edit(c.Key, field, cardEl(c.Key)));
  const bulkKeys = () => (S.marks.size ? [...S.marks] : S.sel ? [S.sel] : []);

  function bindKeys() {
    const k = scope, G = 'Board';
    k.bind(['j', 'ArrowDown'], () => move(0, 1), 'next card', { group: G });
    k.bind(['k', 'ArrowUp'], () => move(0, -1), 'previous card', { group: G });
    k.bind(['h', 'ArrowLeft'], () => move(-1, 0), 'previous lane', { group: G });
    k.bind(['l', 'ArrowRight'], () => move(1, 0), 'next lane', { group: G });
    k.bind('Home', () => edge(false), 'first card in lane', { group: G });
    k.bind('End', () => edge(true), 'last card in lane', { group: G });
    k.bind('PageDown', () => page(1), 'page down', { group: G, hidden: true });
    k.bind('PageUp', () => page(-1), 'page up', { group: G, hidden: true });
    k.bind('Enter', need(c => app.panel.open(c.Key)), 'open issue', { group: G });
    k.bind('Escape', () => {
      if (app.panel.key) app.panel.close();
      else if (S.marks.size) { const ks = [...S.marks]; S.marks.clear(); ks.forEach(rebind); }
      else if (S.sel) { const o = S.sel; S.sel = null; rebind(o); }
    }, 'close panel / clear selection', { group: G });
    k.bind('s', need(c => app.actions.transition(c.Key)), 'change status', { group: 'Board: edit' });
    k.bind('e', edit('summary'), 'edit summary', { group: 'Board: edit' });
    k.bind('a', edit('assignee'), 'assign', { group: 'Board: edit' });
    k.bind('p', edit('priority'), 'set priority', { group: 'Board: edit' });
    k.bind('P', edit('points'), 'set points', { group: 'Board: edit' });
    k.bind('H', () => stepCol(-1), 'move card to previous column', { group: 'Board: edit' });
    k.bind('L', () => stepCol(1), 'move card to next column', { group: 'Board: edit' });
    k.bind('J', () => stepRank(1), 'rank card down', { group: 'Board: edit' });
    k.bind('K', () => stepRank(-1), 'rank card up', { group: 'Board: edit' });
    k.bind('x', () => { toggleMark(S.sel); move(0, 1); }, 'mark card (multi-select)', { group: 'Board: edit' });
    k.bind('X', () => { const ks = bulkKeys(); if (ks.length) app.actions.bulk(ks); }, 'bulk edit marked cards', { group: 'Board: edit' });
    k.bind('n', () => app.actions.create({ project: S.project }), 'new issue', { group: 'Board: edit' });
    k.bind('o', need(c => window.open(app.session.baseURL + '/browse/' + c.Key, '_blank', 'noopener')), 'open in Jira', { group: G });
    k.bind('y', need(c => navigator.clipboard && navigator.clipboard.writeText(c.Key).then(() => ui.toast('Copied ' + c.Key))), 'copy key', { group: G });
    k.bind('r', () => refresh(true), 'refresh', { group: G });
    k.bind('t', () => setMode(S.mode === 'lanes' ? 'list' : 'lanes'), 'lanes / list', { group: G });
    k.bind('S', () => setSort(SORTS[(SORTS.indexOf(S.sort) + 1) % SORTS.length], 1), 'next sort (list)', { group: G });
    k.bind('B', () => pickBoard(), 'switch project / board', { group: G });
    k.bind('v', () => pickSprint(), 'sprint / backlog / whole board', { group: G });
    k.bind('[', () => cycleScope(-1), 'previous sprint', { group: G });
    k.bind(']', () => cycleScope(1), 'next sprint', { group: G });
    k.bind('f', () => { filterIn.focus(); filterIn.select(); }, 'filter cards', { group: 'Board: filter' });
    k.bind('m', () => toggleMine(), 'only my cards', { group: 'Board: filter' });
    k.bind('A', () => pickWho(), 'filter by assignee', { group: 'Board: filter' });
    k.bind('0', () => clearFilters(), 'clear filters', { group: 'Board: filter' });
    for (let i = 1; i <= 9; i++) k.bind(String(i), () => { const q = qfs()[i - 1]; if (q) toggleQF(q.ID); }, i === 1 ? 'toggle quick filter 1-9' : '', { group: 'Board: filter', hidden: i > 1 });
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
    C('switch', 'switch project / board', pickBoard);
    C('sprint', 'pick sprint / backlog / whole board', pickSprint);
    C('new', 'new issue', () => app.actions.create({ project: S.project }));
    for (const s of SORTS) C('sort:' + s, 'sort list by ' + s, () => setSort(s, 1));
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

  async function start() {
    bdMsg('Loading…');
    let project = params.project, bid = params.board ? Number(params.board) : 0;
    try {
      if (!project) {
        const [p, b] = String(app.prefs.get('board.last', '')).split('/');
        project = p || ''; if (!bid && b) bid = Number(b);
      }
      if (!project) project = (app.session.projects || [])[0];
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
    if (app.prefs.get('board.last', '') !== S.project + '/' + S.board.ID) app.prefs.set('board.last', S.project + '/' + S.board.ID);
    const dm = app.session.ui && app.session.ui.DefaultMode;
    S.mode = app.prefs.get('board.mode.' + S.board.ID, app.prefs.get('board.mode', dm === 'list' ? 'list' : 'lanes')) === 'list' ? 'list' : 'lanes';
    const [sort, dir] = String(app.prefs.get('board.sort.' + S.board.ID, 'rank:1')).split(':');
    S.sort = SORTS.includes(sort) ? sort : 'rank'; S.dir = dir === '-1' ? -1 : 1;
    renderToolbar(); renderBar();
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
