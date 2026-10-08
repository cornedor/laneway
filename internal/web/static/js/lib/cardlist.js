// The list of cards as rows of columns, with a header that sorts and orders (dragged) and a pick of the columns: the board's list
// view and planning draw it alike. Styles in css/board.css (.bd-lhead, .lrow, .l-*).
import { h } from './dom.js';
import { icon, setIcon } from './icons.js';
import { avatar } from './ui.js';
import { isZero, date, shortDate, ago } from './fmt.js';
import { prioOrd } from './cardsort.js';
import * as cq from './cardquery.js';
import { check, setCheck } from './selbar.js';
import { FIXED, fixCols, moveCol, pickOrder } from './listcols.js';
import { T } from './i18n.js';

export { fixCols };

// What O steps the list's order through.
export const SORTS = ['rank', 'priority', 'points', 'assignee', 'epic', 'key', 'status', 'updated', 'due', 'created'];
export const PRIO_ICON = ['chevrons-up', 'chevron-up', 'equal', 'chevron-down', 'chevrons-down'];
// List columns: id, header, width.
export const COLS = {
  mark: ['', '1.857rem'], key: [T('Key'), '6.143rem'], summary: [T('Summary'), 'minmax(8.571rem, 1fr)'], status: [T('Status'), '8.429rem'], priority: [T('Prio'), '2.429rem'],
  points: [T('Pts'), '2.857rem'], assignee: [T('Assignee'), '10.714rem'], epic: [T('Epic'), '9.286rem'], labels: [T('Labels'), '7.857rem'], reporter: [T('Reporter'), '7.857rem'],
  due: [T('Due'), '4.571rem'], updated: [T('Updated'), '6rem'], created: [T('Created'), '6rem'], age: [T('Age'), '3.143rem'],
};
export const DEFAULT_COLS = ['mark', 'key', 'summary', 'status', 'priority', 'points', 'assignee', 'due', 'updated'];

export const sortable = id => id !== 'mark';
export const colLabel = id => (id.startsWith('x:') ? id.slice(2) : (COLS[id] || ['', ''])[0]);
export const colWidth = id => (COLS[id] ? COLS[id][1] : '8.571rem');
// gridCols is the grid of cols, widths (id → px, dragged) over their defaults.
export const gridCols = (cols, widths = {}) => cols.map(id => (widths[id] ? widths[id] + 'px' : colWidth(id))).join(' ');
// resizable: the summary takes what the others leave, the mark is a checkbox.
const resizable = id => id !== 'mark' && id !== 'summary';
const MIN_W = 24;
// readWidths is a stored widths pref ('{"key":90}'), {} when none or broken.
export function readWidths(v) {
  try { const o = JSON.parse(v || '{}'); return o && typeof o === 'object' ? o : {}; } catch (e) { return {}; }
}


// A header click sorts by its column, again reverses it, a third time goes back to the rank.
export function nextSort(sort, dir, id) {
  if (sort !== id) return [id, 1];
  return dir > 0 ? [id, -1] : ['rank', 1];
}

const sortMark = (id, sort, dir) => (sort === id ? [' ', icon(dir > 0 ? 'arrow-up' : 'arrow-down')] : []);
const grip = id => (resizable(id) ? [h('span.lh-grip', { title: T('Drag to resize, double-click to reset') })] : []);
export function listHead(cols, sort, dir) {
  return h('div.bd-lhead', cols.map(id => h('span', { class: 'lh-' + id, draggable: id !== 'mark', dataset: { col: id, sort: sortable(id) ? id : '' } }, colLabel(id), sortMark(id, sort, dir), grip(id))));
}

// resizeCols lets head's cells be widened or narrowed by their right edge, a double-click resetting one;
// widths (id → px) changes in place, apply lays the grid out again, save keeps it.
export function resizeCols(head, { widths, apply, save }) {
  let drag = null;
  head.addEventListener('pointerdown', e => {
    const g = e.target.closest('.lh-grip');
    if (!g || e.button !== 0) return;
    e.preventDefault(); e.stopPropagation();
    const cell = g.parentElement;
    drag = { id: cell.dataset.col, x: e.clientX, w: cell.getBoundingClientRect().width, moved: false };
    head.classList.add('resizing');
    g.setPointerCapture(e.pointerId);
  });
  head.addEventListener('pointermove', e => {
    if (!drag) return;
    drag.moved = drag.moved || Math.abs(e.clientX - drag.x) > 2;
    if (!drag.moved) return;
    widths[drag.id] = Math.round(Math.max(MIN_W, drag.w + e.clientX - drag.x));
    apply();
  });
  const end = () => {
    if (!drag) return;
    if (drag.moved) save();
    drag = null;
    setTimeout(() => head.classList.remove('resizing'), 0); // past the click the release makes
  };
  head.addEventListener('pointerup', end);
  head.addEventListener('pointercancel', end);
  head.addEventListener('dblclick', e => {
    const g = e.target.closest('.lh-grip'); if (!g) return;
    e.stopPropagation();
    delete widths[g.parentElement.dataset.col];
    apply(); save();
  });
  // A press on an edge neither sorts nor drags the column.
  head.addEventListener('click', e => { if (e.target.closest('.lh-grip') || head.classList.contains('resizing')) e.stopPropagation(); }, true);
  head.addEventListener('dragstart', e => { if (head.classList.contains('resizing')) { e.preventDefault(); e.stopPropagation(); } }, true);
}

// dragCols lets head's cells be dragged onto one another; set gets the new order.
export function dragCols(head, cols, set) {
  let from = null;
  const clear = () => head.querySelectorAll('.drop-before, .drop-after').forEach(s => s.classList.remove('drop-before', 'drop-after'));
  // at is the column under the pointer across the whole header, gaps and all: the nearest shown one.
  const at = e => {
    if (!from) return null;
    let best = null, dist = Infinity;
    for (const t of head.querySelectorAll('[data-col]')) {
      const r = t.getBoundingClientRect();
      if (!r.width) continue; // hidden on a narrow screen
      const d = e.clientX < r.left ? r.left - e.clientX : e.clientX > r.right ? e.clientX - r.right : 0;
      if (d < dist) { best = { to: t.dataset.col, el: t, after: e.clientX > r.left + r.width / 2 }; dist = d; }
    }
    return best;
  };
  head.addEventListener('dragstart', e => {
    e.stopPropagation();
    from = e.target.dataset.col;
    e.dataTransfer.effectAllowed = 'move'; e.dataTransfer.setData('text/plain', from);
  });
  head.addEventListener('dragover', e => {
    const t = at(e); if (!t) return;
    e.preventDefault(); e.stopPropagation(); e.dataTransfer.dropEffect = 'move';
    clear(); if (t.to !== from) t.el.classList.add(t.after ? 'drop-after' : 'drop-before');
  });
  head.addEventListener('drop', e => {
    const t = at(e); if (!t) return;
    e.preventDefault(); e.stopPropagation(); clear();
    const next = moveCol(cols(), from, t.to, t.after);
    if (next.join() !== cols().join()) set(next);
  });
  head.addEventListener('dragend', e => { e.stopPropagation(); from = null; clear(); });
}
export function paintHead(head, sort, dir) {
  for (const s of head.children) s.replaceChildren(colLabel(s.dataset.col), ...sortMark(s.dataset.sort, sort, dir), ...grip(s.dataset.col));
}

// The columns to pick from: every one but those every list has; null when cancelled.
export async function pickCols(ui, all, cols) {
  const items = all.filter(c => !FIXED.includes(c));
  const r = await ui.pick({ title: T('List columns'), items, multi: true, selected: items.filter(c => cols.includes(c)), label: colLabel, placeholder: T('Columns…') });
  return r ? pickOrder(all, cols, r) : null;
}

const DAY = 864e5;
export function ageText(c) {
  if (c.Done) return '';
  const t = !isZero(c.Since) ? c.Since : c.Created;
  if (isZero(t)) return '';
  const d = Math.floor((Date.now() - Date.parse(t)) / DAY);
  return d > 0 ? d + 'd' : Math.max(1, Math.floor((Date.now() - Date.parse(t)) / 36e5)) + 'h';
}
export function setAvatar(slot, c, name) {
  const k = c.Assignee + '|' + c.AvatarURL + '|' + !!name;
  if (slot._k === k) return;
  slot._k = k;
  slot.replaceChildren(avatar(c.Assignee, c.AvatarURL, 20), name && c.Assignee ? h('span.l-name', ' ' + c.Assignee) : '');
}
const startOfToday = () => { const d = new Date(); d.setHours(0, 0, 0, 0); return d.getTime(); };

// buildRow is an empty row of cols; its cells are in w._r by column id.
export function buildRow(cols) {
  const r = {};
  const w = h('div.lrow', { draggable: true }, cols.map(id => (r[id] = cell(id))), r.ghead = h('div.l-ghead'));
  w._r = r;
  return w;
}
// cell is a column's span; the mark's holds a checkbox and the icon it gives way to (pin, flag…).
function cell(id) {
  const e = h('span', { class: 'l-' + id.replace(/\W+/g, '-') });
  if (id === 'mark') e.append(e._chk = check(), e._ico = h('span.l-ico'));
  return e;
}
// fillCells writes card c into row w's cells. o: marked, pinned (key → bool), hl (key → colour, '' the theme's,
// null none), tmark (key → running timer text), fdate (time, fallback → text), stamp (cell, key) for an agent's mark.
export function fillCells(w, cols, c, o) {
  const r = w._r;
  for (const id of cols) {
    if (FILL[id]) FILL[id](r[id], c, o);
    else if (id.startsWith('x:')) r[id].textContent = cq.extraOf(c)[id.slice(2).toLowerCase()] || '';
  }
}
const FILL = {
  mark: (e, c, o) => { const hl = o.hl(c.Key); setCheck(e._chk, o.marked(c.Key)); setIcon(e._ico, hl !== null ? 'circle' : o.pinned(c.Key) ? 'pin' : c.Flagged ? 'flag' : '', '', hl !== null || c.Flagged); },
  key: (e, c, o) => { e.textContent = c.Key; if (o.stamp) o.stamp(e, c.Key); const tm = o.tmark(c.Key); if (tm) e.append(h('span.ctimer', ' ', icon('timer'), ' ' + tm)); },
  summary: (e, c) => { e.textContent = c.Summary; e.title = c.Summary; },
  status: (e, c) => { e.textContent = c.Status; e.className = 'l-status pill cat-' + (c.Done ? 'done' : c.InProgress ? 'indeterminate' : 'new'); },
  priority: (e, c) => { const po = prioOrd(c); e.className = 'l-priority cprio p' + po; setIcon(e, po < 5 ? PRIO_ICON[po] : ''); e.title = c.Priority; },
  points: (e, c) => { e.textContent = c.Points; },
  assignee: (e, c) => setAvatar(e, c, true),
  epic: (e, c) => { e.textContent = c.ParentSummary || ''; e.title = c.ParentKey ? c.ParentKey + ' ' + c.ParentSummary : ''; },
  labels: (e, c) => { e.textContent = (c.Labels || '').split(' ').filter(Boolean).map(l => '#' + l).join(' '); },
  reporter: (e, c) => { e.textContent = c.Reporter || ''; },
  due: (e, c, o) => { const d = date(c.Due); e.textContent = d ? o.fdate(c.Due, shortDate(c.Due)) : ''; e.className = 'l-due' + (d && !c.Done && d.getTime() < startOfToday() ? ' overdue' : ''); },
  updated: (e, c, o) => { e.textContent = ago(c.Updated); e.title = o.fdate(c.Updated, ''); },
  created: (e, c, o) => { e.textContent = ago(c.Created); e.title = o.fdate(c.Created, ''); },
  age: (e, c) => { e.textContent = ageText(c); },
};
