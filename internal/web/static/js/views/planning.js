// Planning: the backlog and the open sprints as stacked, collapsible sections.
// One scroller, rows of known height, only the visible ones in the DOM.
import { h, clear, frame } from '../lib/dom.js';
import { css } from '../lib/css.js';
import { isZero, shortDate } from '../lib/fmt.js';
import { resolve, remember, pickScope, noBoard } from './plan_ctx.js';

const pts = c => Number(c.Points) || 0;
const fmtP = n => String(Math.round(n * 10) / 10);
const isoDay = t => { const d = new Date(t); return d.getFullYear() + '-' + String(d.getMonth() + 1).padStart(2, '0') + '-' + String(d.getDate()).padStart(2, '0'); };
const plusDays = n => { const d = new Date(); d.setDate(d.getDate() + n); return isoDay(d); };
const cat = c => (c.Done ? 'done' : c.InProgress ? 'indeterminate' : 'new');
const typeClass = t => { t = (t || '').toLowerCase(); return t.includes('sub') ? 'sub' : ['bug', 'story', 'task', 'epic'].find(x => t.includes(x)) || ''; };

function nextSprintName(name) {
  const m = (name || '').match(/^(.*?)(\d+)(\D*)$/);
  return m ? m[1] + (Number(m[2]) + 1) + m[3] : '';
}

export default async function mount(el, { app, params, scope, context, toolbar }) {
  css('planning');
  const sc = await resolve(app, params);
  let { project, board } = sc;
  remember(app, project, board);
  if (!board) { el.append(noBoard('Planning', project)); return; }

  const caps = (app.session.ui && app.session.ui.Capacity) || {};
  let data = null, sections = [], cur = '', filter = '', velAvg = 0, columns = [];
  const sel = new Set(), folded = new Set();
  let rows = [], tops = [], total = 0, ROW = 32, drag = null, token = 0, writing = 0, lastWrite = 0;

  const scroller = h('div.pl-scroll', { tabindex: -1 });
  const space = h('div.pl-space');
  const live = new Map();
  const dropEl = h('div.pl-drop', { hidden: true });
  space.append(dropEl);
  scroller.append(space);
  el.append(h('div.pl', scroller));

  const boardBtn = app.chrome.label(app.chrome.crumb('Board (b)', () => pickBoard()), project, board.Name);
  const filterIn = h('input.input.pl-filter', { type: 'search', placeholder: 'Filter  f', 'aria-label': 'Filter issues', oninput: () => { filter = filterIn.value.trim().toLowerCase(); relayout(); } });
  context.append(boardBtn);
  toolbar.append(filterIn, h('span.spacer'), h('button.btn.nw', { title: 'New sprint (N)', onclick: () => newSprint() }, '+ Sprint'));

  // ---- data
  const secOf = id => sections.find(s => s.id === id);
  function build(d) {
    data = d; columns = d.Columns || [];
    sections = (d.Sprints || []).map(s => ({ id: s.ID, sprint: s, name: s.Name, cards: s.Cards || [] }));
    sections.push({ id: 0, sprint: null, name: 'Backlog', cards: (d.Backlog && d.Backlog.Cards) || [] });
    for (const k of [...sel]) if (!sections.some(s => s.cards.some(c => c.Key === k))) sel.delete(k);
  }
  async function load(fresh) {
    const my = ++token;
    try {
      const d = await app.api.get('/plan/' + board.ID, { fresh });
      if (my !== token) return;
      build(d); relayout(true);
    } catch (e) {
      if (my !== token) return;
      if (!data) clear(scroller).append(h('div.empty', h('h2', 'Could not load'), h('p', e.message), h('button.btn', { onclick: () => load(true) }, 'Retry')));
      else app.ui.errToast(e);
    }
  }
  function loadVelocity() {
    app.api.get('/reports/velocity/' + board.ID + '?n=3').then(v => {
      if (!v || !v.length) return;
      velAvg = v.reduce((a, x) => a + x.Done, 0) / v.length; relayout();
    }).catch(() => {});
  }

  // ---- layout
  const match = c => !filter || [c.Key, c.Summary, c.Assignee, c.Labels, c.Status, c.ParentSummary].some(f => f && String(f).toLowerCase().includes(filter));
  const shown = s => (filter ? s.cards.filter(match) : s.cards);
  const headH = s => ROW + (s.sprint ? 34 : 6);

  function relayout(keepScroll) {
    ROW = parseInt(getComputedStyle(document.documentElement).getPropertyValue('--row')) || 32;
    rows = [];
    for (const s of sections) {
      rows.push({ k: 'h', key: 'h:' + s.id, s, h: headH(s) });
      if (folded.has(s.id)) continue;
      const cs = shown(s);
      if (!cs.length) rows.push({ k: 'e', key: 'e:' + s.id, s, h: ROW });
      for (const c of cs) rows.push({ k: 'c', key: 'c:' + c.Key, s, c, h: ROW });
    }
    tops = []; total = 0;
    for (const r of rows) { tops.push(total); total += r.h; }
    space.style.height = total + 'px';
    if (!stops().includes(cur)) cur = stops()[0] || '';
    for (const [k, n] of live) { n.remove(); live.delete(k); }
    paint();
  }
  const stops = () => rows.filter(r => r.k !== 'e').map(r => (r.k === 'h' ? 'h:' + r.s.id : r.c.Key));
  const rowAtY = y => {
    let lo = 0, hi = rows.length - 1;
    while (lo < hi) { const mid = (lo + hi + 1) >> 1; if (tops[mid] <= y) lo = mid; else hi = mid - 1; }
    return lo;
  };

  // ---- paint the visible window
  const paint = frame(() => {
    if (!rows.length) return;
    const vt = scroller.scrollTop, vb = vt + scroller.clientHeight;
    const a = Math.max(rowAtY(vt) - 6, 0), b = Math.min(rowAtY(vb) + 6, rows.length - 1);
    const want = new Set();
    for (let i = a; i <= b; i++) {
      const r = rows[i]; want.add(r.key);
      let n = live.get(r.key);
      const sig = sigOf(r);
      if (!n) { n = h('div.pl-abs'); live.set(r.key, n); space.append(n); }
      if (n._sig !== sig) { n._sig = sig; n.replaceChildren(rowNode(r)); }
      n.style.transform = `translateY(${tops[i]}px)`; n.style.height = r.h + 'px';
      const inner = n.firstChild;
      if (inner) {
        const id = r.k === 'h' ? 'h:' + r.s.id : r.k === 'c' ? r.c.Key : '';
        inner.classList.toggle('cur', id === cur);
        if (r.k === 'c') inner.classList.toggle('picked', sel.has(r.c.Key));
      }
    }
    for (const [k, n] of live) if (!want.has(k) && !(drag && drag.keys.some(x => k === 'c:' + x))) { n.remove(); live.delete(k); }
  });
  scroller.addEventListener('scroll', () => paint(), { passive: true });
  new ResizeObserver(() => paint()).observe(scroller);

  function sigOf(r) {
    if (r.k === 'c') { const c = r.c; return [c.Key, c.Summary, c.Status, c.Assignee, c.Points, c.Flagged, c.Type, c.ParentSummary, c.Done, sel.has(c.Key)].join('|'); }
    if (r.k === 'e') return 'e' + (filter ? 'f' : '');
    const s = r.s;
    return ['h', s.id, s.name, s.sprint && s.sprint.State, s.sprint && s.sprint.Start, s.sprint && s.sprint.End, s.sprint && s.sprint.Goal, folded.has(s.id), velAvg, filter,
      s.cards.map(c => c.Key + c.Points + c.Assignee + c.Done).join(',')].join('|');
  }

  function rowNode(r) {
    if (r.k === 'e') return h('div.pl-row.pl-empty', { style: { height: r.h + 'px' } }, filter ? 'No matches' : r.s.sprint ? 'Drop issues here to plan them' : 'The backlog is empty');
    if (r.k === 'h') return headNode(r.s, r.h);
    const c = r.c, t = typeClass(c.Type);
    return h('div.pl-row' + (c.Done ? '.done' : ''), { draggable: true, dataset: { key: c.Key }, style: { height: r.h + 'px' } },
      h('span.pl-sel', sel.has(c.Key) ? '☑' : '☐'),
      h('span.pl-type.' + (t || 'x'), { title: c.Type }, (c.Type || '?')[0]),
      h('span.pl-key', c.Key), h('span.pl-sum', { title: c.Summary }, c.Summary),
      c.Flagged && h('span.pl-flag', { title: 'Flagged' }, '⚑'),
      c.ParentSummary && h('span.chip.pl-epic', { title: c.ParentKey + ' ' + c.ParentSummary }, c.ParentSummary),
      app.ui.statusPill(c.Status, cat(c)),
      h('button.pl-pts' + (c.Points ? '' : '.none'), { dataset: { act: 'points' }, title: 'Story points (P)', tabindex: -1 }, c.Points || '–'),
      app.ui.avatar(c.Assignee, c.AvatarURL, 20));
  }

  function headNode(s, height) {
    const sp = s.sprint, open = !folded.has(s.id), cs = s.cards;
    const sum = cs.reduce((a, c) => a + pts(c), 0), done = cs.reduce((a, c) => a + (c.Done ? pts(c) : 0), 0);
    const over = sp && velAvg > 0 && sum > velAvg * 1.001;
    const dates = sp && !isZero(sp.Start) ? shortDate(sp.Start) + ' – ' + shortDate(sp.End) : '';
    const h1 = h('div.pl-h1',
      h('button.pl-fold', { tabindex: -1, 'aria-label': open ? 'Fold ' + s.name : 'Unfold ' + s.name, 'aria-expanded': open, dataset: { act: 'fold' } }, open ? '▾' : '▸'),
      h('span.pl-name', s.name),
      sp && h('span.pill.cat-' + (sp.State === 'active' ? 'indeterminate' : 'new'), sp.State === 'active' ? 'active' : 'planned'),
      dates && h('span.pl-dates', dates),
      sp && h('span.pl-goal', { title: sp.Goal || 'No goal' }, sp.Goal || ''),
      !sp || !sp.Goal ? h('span.spacer') : null,
      h('span.pl-tot' + (over ? '.over' : ''), cs.length + (cs.length === 1 ? ' issue' : ' issues'), ' · ', h('b', fmtP(sum) + 'p'), sp && velAvg > 0 && ` of ~${fmtP(velAvg)}p`),
      sp && sp.State === 'future' && h('button.btn.pl-act', { dataset: { act: 'start' }, tabindex: -1 }, 'Start'),
      sp && sp.State === 'active' && h('button.btn.pl-act', { dataset: { act: 'close' }, tabindex: -1 }, 'Complete'),
      sp && h('button.btn.ghost.pl-act', { dataset: { act: 'edit' }, tabindex: -1, title: 'Edit sprint (E)' }, 'Edit'));
    const out = h('div.pl-row.pl-head', { style: { height: height + 'px' } }, h1);
    if (sp) out.append(h('div.pl-h2', h('span.pl-prog', { title: fmtP(done) + 'p done', role: 'progressbar', 'aria-valuenow': sum ? Math.round(done / sum * 100) : 0, 'aria-valuemin': 0, 'aria-valuemax': 100 }, h('i', { style: { width: (sum ? done / sum * 100 : 0) + '%' } })), capsNode(cs)));
    return out;
  }

  function capFor(name) {
    if (name in caps) return caps[name];
    return name !== 'unassigned' && 'default' in caps ? caps.default : null;
  }
  function capsNode(cs) {
    const by = new Map();
    for (const c of cs) by.set(c.Assignee || 'unassigned', (by.get(c.Assignee || 'unassigned') || 0) + pts(c));
    const list = [...by].sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0]));
    return h('div.pl-caps', list.map(([name, p]) => {
      const cap = capFor(name), over = cap != null && p > cap;
      return h('span.pl-cap' + (over ? '.over' : ''), { title: cap != null ? `${name}: ${fmtP(p)} of ${fmtP(cap)}p` : `${name}: ${fmtP(p)}p` },
        h('span', name), cap != null && h('span.bar', h('i', { style: { width: Math.min(p / (cap || 1), 1) * 100 + '%' } })), cap != null ? `${fmtP(p)}/${fmtP(cap)}${over ? '!' : ''}` : fmtP(p));
    }));
  }

  // ---- cursor and selection
  function setCur(id, scrollTo = true) {
    cur = id; paint();
    if (!scrollTo) return;
    const i = rows.findIndex(r => (r.k === 'h' ? 'h:' + r.s.id : r.k === 'c' ? r.c.Key : '') === id);
    if (i < 0) return;
    const top = tops[i], bot = top + rows[i].h, vt = scroller.scrollTop, vh = scroller.clientHeight;
    if (top < vt) scroller.scrollTop = top; else if (bot > vt + vh) scroller.scrollTop = bot - vh;
  }
  const move = d => { const st = stops(); if (!st.length) return; setCur(st[Math.min(Math.max(st.indexOf(cur) + d, 0), st.length - 1)]); };
  const curCard = () => { for (const s of sections) { const c = s.cards.find(x => x.Key === cur); if (c) return { c, s }; } return null; };
  const curSection = () => { const cc = curCard(); if (cc) return cc.s; return cur.startsWith('h:') ? secOf(Number(cur.slice(2))) : sections[0]; };
  // the cards an action applies to, in display order
  function targets() {
    if (sel.size) return rows.filter(r => r.k === 'c' && sel.has(r.c.Key)).map(r => r.c.Key);
    const cc = curCard(); return cc ? [cc.c.Key] : [];
  }
  function toggleSel(key) { sel.has(key) ? sel.delete(key) : sel.add(key); for (const k of ['c:' + key]) { const n = live.get(k); if (n) n._sig = ''; } paint(); }

  // ---- writes: optimistic, queued, reloaded from the server if one fails
  let queue = Promise.resolve(), settle = 0;
  function enqueue(fn, keys) {
    writing++; lastWrite = Date.now();
    queue = queue.then(fn).then(() => { clearTimeout(settle); settle = setTimeout(() => load(true), 500); for (const k of keys || []) app.bus.emit('issue:changed', { key: k }); })
      .catch(e => { app.ui.errToast(e); load(true); })
      .finally(() => { writing--; lastWrite = Date.now(); });
    return queue;
  }
  const rank = (key, other, after) => app.api.post('/issues/' + key + '/rank', { Other: other, After: after });

  // Move keys to section `to`, before/after anchor (or to its end).
  function moveCards(keyList, to, anchor, after) {
    if (!keyList.length || !to) return;
    const order = [], from = new Map();
    for (const s of sections) for (const c of s.cards) if (keyList.includes(c.Key)) { order.push(c); from.set(c.Key, s.id); }
    if (anchor && keyList.includes(anchor)) return;
    for (const s of sections) s.cards = s.cards.filter(c => !keyList.includes(c.Key));
    let idx = to.cards.length;
    if (anchor) { idx = to.cards.findIndex(c => c.Key === anchor); idx = idx < 0 ? to.cards.length : idx + (after ? 1 : 0); }
    const moved = order.map(c => ({ ...c, Sprint: to.sprint ? to.sprint.Name : '' }));
    to.cards.splice(idx, 0, ...moved);
    const crossing = order.filter(c => from.get(c.Key) !== to.id).map(c => c.Key);
    relayout();
    const names = order.map(c => c.Key);
    enqueue(async () => {
      if (crossing.length) await app.api.post('/plan/move', { Keys: crossing, Sprint: to.id });
      if (anchor) { let prev = anchor, aft = after; for (const k of names) { await rank(k, prev, aft); prev = k; aft = true; } }
    }, names);
  }

  // J/K: swap with the neighbour
  function rankStep(d) {
    const cc = curCard(); if (!cc || filter) return;
    const cs = cc.s.cards, i = cs.indexOf(cc.c), j = i + d;
    if (j < 0 || j >= cs.length) return;
    const other = cs[j].Key;
    [cs[i], cs[j]] = [cs[j], cs[i]];
    relayout(); setCur(cc.c.Key);
    enqueue(() => rank(cc.c.Key, other, d > 0), [cc.c.Key]);
  }

  async function moveTo() {
    const keysToMove = targets(); if (!keysToMove.length) return;
    const to = await app.ui.pick({ title: `Move ${keysToMove.length} to…`, items: sections, label: s => s.name, detail: s => s.cards.length + ' issues' });
    if (to) { moveCards(keysToMove, to, null, false); if (keysToMove.length === 1) cur = keysToMove[0]; sel.clear(); relayout(); setCur(cur); }
  }
  function shift(d) {
    const cc = curCard(); if (!cc) return;
    const to = sections[sections.indexOf(cc.s) + d];
    if (!to) return;
    const ks = targets(); moveCards(ks, to, null, false); sel.clear(); relayout(); setCur(cc.c.Key);
    app.ui.toast(`${ks.length > 1 ? ks.length + ' issues' : ks[0]} to ${to.name}`);
  }

  // ---- sprint actions
  const sprintOf = pred => { const s = curSection(); return s && s.sprint && (!pred || pred(s.sprint)) ? s : sections.find(x => x.sprint && (!pred || pred(x.sprint))); };
  async function newSprint() {
    const last = [...sections].reverse().find(s => s.sprint);
    const name = await app.ui.prompt({ title: 'New sprint', value: nextSprintName(last && last.name), placeholder: 'Sprint name', ok: 'Create' });
    if (!name || !name.trim()) return;
    try { await app.api.post('/plan/sprints', { Board: board.ID, Name: name.trim() }); app.ui.toast('Created ' + name.trim(), { kind: 'ok' }); load(true); } catch (e) { app.ui.errToast(e); }
  }
  function dialog(title, fields, ok, run) {
    const form = h('form.pl-form', { onsubmit: async e => {
      e.preventDefault();
      try { await run(form); m.close(); load(true); } catch (err) { app.ui.errToast(err); }
    } }, fields, h('div.row.end', h('button.btn', { type: 'button', onclick: () => m.close() }, 'Cancel'), h('button.btn.primary', { type: 'submit' }, ok)));
    const m = app.ui.modal(form, { title });
    return m;
  }
  function startSprint() {
    const s = sprintOf(sp => sp.State === 'future'); if (!s) return app.ui.toast('No planned sprint to start');
    dialog('Start ' + s.name, [
      h('label', 'Ends on', h('input.input', { type: 'date', name: 'end', value: plusDays(14), min: plusDays(1), required: true, autofocus: true })),
      h('p.pl-note', `Starts now with ${s.cards.length} issues, ${fmtP(s.cards.reduce((a, c) => a + pts(c), 0))}p.`)],
    'Start sprint', async f => { await app.api.post('/plan/sprints/' + s.id + '/start', { End: f.end.value }); app.ui.toast(s.name + ' started', { kind: 'ok' }); });
  }
  function closeSprint() {
    const s = sprintOf(sp => sp.State === 'active'); if (!s) return app.ui.toast('No active sprint');
    const lastCol = columns.length ? columns[columns.length - 1].StatusIDs || [] : [];
    const open = s.cards.filter(c => !c.Done && !lastCol.includes(c.StatusID));
    const next = sections.filter(x => x.sprint && x.sprint.State === 'future');
    const dest = h('select.input', { name: 'to' }, next.map(x => h('option', { value: x.id }, x.name)), h('option', { value: 0 }, 'Backlog'));
    dialog('Complete ' + s.name, [
      h('p', `${s.cards.length - open.length} done, ${open.length} unfinished.`),
      open.length ? h('label', 'Move unfinished issues to', dest) : null],
    'Complete sprint', async f => {
      const r = await app.api.post('/plan/sprints/' + s.id + '/close', { Board: board.ID, MoveTo: Number(dest.value) || 0 });
      app.ui.toast(`${s.name} completed` + (r && r.Moved ? `, ${r.Moved} moved` : ''), { kind: 'ok' });
    });
  }
  function editSprint() {
    const s = sprintOf(); if (!s) return;
    const sp = s.sprint, end = isZero(sp.End) ? '' : isoDay(sp.End);
    dialog('Edit ' + s.name, [
      h('label', 'Name', h('input.input', { name: 'name', value: sp.Name, required: true, autofocus: true })),
      h('label', 'Goal', h('textarea.input', { name: 'goal', rows: 3 }, sp.Goal || '')),
      h('label', 'Ends on', h('input.input', { type: 'date', name: 'end', value: end }))],
    'Save', async f => {
      const body = {};
      if (f.name.value.trim() !== sp.Name) body.Name = f.name.value.trim();
      if (f.goal.value.trim() !== (sp.Goal || '')) body.Goal = f.goal.value.trim();
      if (f.end.value && f.end.value !== end) body.End = f.end.value;
      if (Object.keys(body).length) await app.api.post('/plan/sprints/' + s.id + '/update', body);
    });
  }
  const act = { new: newSprint, start: startSprint, close: closeSprint, edit: editSprint };
  const onAction = e => { const f = act[e.detail]; if (f) f(); };
  document.addEventListener('plan:action', onAction);

  async function pickBoard() {
    const r = await pickScope(app); if (!r) return;
    app.go('/planning/' + r.project + '/' + r.board.ID);
  }

  // ---- mouse
  scroller.addEventListener('click', e => {
    const n = e.target.closest('.pl-row'); if (!n) return;
    const holder = n.parentElement, r = rows.find(x => 'pl-abs' && live.get(x.key) === holder);
    if (!r) return;
    const a = e.target.closest('[data-act]');
    if (r.k === 'h') {
      const f = a && a.dataset.act;
      setCur('h:' + r.s.id, false);
      if (f === 'start' || f === 'close' || f === 'edit') return act[f]();
      return toggleFold(r.s);
    }
    if (r.k !== 'c') return;
    setCur(r.c.Key, false);
    if (a && a.dataset.act === 'points') return app.actions.edit(r.c.Key, 'points', a);
    if (e.target.closest('.pl-sel') || e.ctrlKey || e.metaKey) return toggleSel(r.c.Key);
    app.panel.open(r.c.Key);
  });
  function toggleFold(s) {
    if (!s) return;
    folded.has(s.id) ? folded.delete(s.id) : folded.add(s.id);
    relayout(); setCur('h:' + s.id);
  }

  // ---- drag and drop
  scroller.addEventListener('dragstart', e => {
    const n = e.target.closest('.pl-row[data-key]'); if (!n) return;
    const key = n.dataset.key;
    const ks = sel.has(key) ? targets() : [key];
    drag = { keys: ks };
    e.dataTransfer.effectAllowed = 'move'; e.dataTransfer.setData('text/plain', ks.join(','));
    for (const k of ks) { const l = live.get('c:' + k); if (l && l.firstChild) l.firstChild.classList.add('dragging'); }
  });
  function dropAt(e) {
    const rect = scroller.getBoundingClientRect(), y = e.clientY - rect.top + scroller.scrollTop;
    if (y >= total) { const s = sections[sections.length - 1]; return { s, anchor: null, after: false, y: total - 1 }; }
    const i = rowAtY(Math.max(y, 0)), r = rows[i];
    if (r.k === 'c') { const after = y - tops[i] > r.h / 2; return { s: r.s, anchor: r.c.Key, after, y: tops[i] + (after ? r.h : 0) }; }
    if (r.k === 'h') { const first = shown(r.s)[0]; return { s: r.s, anchor: first ? first.Key : null, after: false, y: tops[i] + r.h }; }
    return { s: r.s, anchor: null, after: false, y: tops[i] + 2 };
  }
  scroller.addEventListener('dragover', e => {
    if (!drag) return;
    e.preventDefault(); e.dataTransfer.dropEffect = 'move';
    const rect = scroller.getBoundingClientRect();
    if (e.clientY < rect.top + 50) scroller.scrollTop -= 14; else if (e.clientY > rect.bottom - 50) scroller.scrollTop += 14;
    const t = dropAt(e);
    dropEl.hidden = false; dropEl.style.top = t.y - 1 + 'px';
  });
  scroller.addEventListener('dragleave', e => { if (!scroller.contains(e.relatedTarget)) dropEl.hidden = true; });
  scroller.addEventListener('drop', e => {
    if (!drag) return;
    e.preventDefault();
    const t = dropAt(e), ks = drag.keys;
    dropEl.hidden = true; drag = null;
    moveCards(ks, t.s, t.anchor, t.after);
  });
  scroller.addEventListener('dragend', () => { dropEl.hidden = true; drag = null; paint(); for (const n of live.values()) n.firstChild && n.firstChild.classList.remove('dragging'); });

  // ---- keys
  const G = { group: 'Planning' };
  scope.bind(['j', 'ArrowDown'], () => move(1), 'next', G);
  scope.bind(['k', 'ArrowUp'], () => move(-1), 'previous', G);
  scope.bind('Home', () => setCur(stops()[0]), 'first', { ...G, hidden: true });
  scope.bind('End', () => setCur(stops().at(-1)), 'last', { ...G, hidden: true });
  scope.bind('J', () => rankStep(1), 'rank down', G);
  scope.bind('K', () => rankStep(-1), 'rank up', G);
  scope.bind('m', moveTo, 'move to a sprint or the backlog', G);
  scope.bind(']', () => shift(1), 'move to the next sprint', G);
  scope.bind('[', () => shift(-1), 'move to the previous sprint', G);
  scope.bind(['x', 'Space'], () => { const cc = curCard(); if (cc) { toggleSel(cc.c.Key); move(1); } else if (cur.startsWith('h:')) toggleFold(curSection()); }, 'select', G);
  scope.bind('Enter', () => { const cc = curCard(); if (cc) app.panel.open(cc.c.Key); else toggleFold(curSection()); }, 'open', G);
  scope.bind('z', () => toggleFold(curSection()), 'fold or unfold the section', G);
  scope.bind('P', () => { const cc = curCard(); if (cc) app.actions.edit(cc.c.Key, 'points'); }, 'story points', G);
  scope.bind('a', () => { const cc = curCard(); if (cc) app.actions.edit(cc.c.Key, 'assignee'); }, 'assignee', G);
  scope.bind('N', newSprint, 'new sprint', G);
  scope.bind('S', startSprint, 'start the sprint', G);
  scope.bind('C', closeSprint, 'complete the active sprint', G);
  scope.bind('E', editSprint, 'edit sprint name, goal, end', G);
  scope.bind('b', pickBoard, 'pick board', G);
  scope.bind('R', () => load(true), 'reload', G);
  scope.bind('f', () => filterIn.focus(), 'filter', G);
  scope.bind('Escape', () => {
    if (document.activeElement === filterIn) { filterIn.value = ''; filter = ''; filterIn.blur(); relayout(); return; }
    if (filter) { filterIn.value = ''; filter = ''; relayout(); return; }
    sel.clear(); for (const n of live.values()) n._sig = ''; paint();
  }, 'clear filter and selection', { ...G, input: true, when: () => document.activeElement === filterIn || !!filter || sel.size > 0 });

  const off = app.bus.on('issue:changed', () => { if (writing || Date.now() - lastWrite < 1500) return; load(true); });

  await load();
  loadVelocity();
  return () => { token++; off(); clearTimeout(settle); document.removeEventListener('plan:action', onAction); };
}
