// The filter builder (F), as the TUI's (internal/ui/filter_builder.go): field, compare and value
// side by side, each narrowed by typing while it has the cursor. tab/→ and shift+tab/← change
// column (the cursor stays on its row), enter goes one column deeper and, on the value (or a
// compare that needs none), adds the term to the board's query (more values of the same field
// join with a comma); the builder stays open for the next. ctrl+x drops the last term, esc closes.
import { h, clear } from '../lib/dom.js';
import { icon } from '../lib/icons.js';
import * as cq from '../lib/cardquery.js';
import { css } from '../lib/css.js';

export function openFilterBuilder({ app, cards, env, query, apply }) {
  css('board');
  let col = 0, q = query || '', typed = '';
  const idx = [0, 0, 0];
  const noValue = op => op === 'empty' || op === '-empty';
  const base = c => {
    if (c === 0) return cq.BUILDER_FIELDS.map(([id, label]) => ({ id, label }));
    if (c === 1) return cq.builderOps(pick(0).id).map(([id, label]) => ({ id, label }));
    return noValue(pick(1).id) ? [] : cq.builderValues(cards, pick(0).id, env).map(v => ({ id: v.id, label: v.label + ' · ' + v.n }));
  };
  const rows = c => {
    const all = base(c);
    if (c !== col || !typed.trim()) return all;
    const terms = typed.toLowerCase().split(/\s+/).filter(Boolean);
    return all.filter(r => terms.every(t => r.label.toLowerCase().includes(t)));
  };
  const pick = c => rows(c)[idx[c]] || {};
  const term = () => cq.builderTerm(pick(0).id, pick(1).id, pick(2).id);

  const input = h('input.input.fb-input', { type: 'text', placeholder: 'type to narrow', spellcheck: false, autocomplete: 'off', autofocus: true });
  const colEls = [h('div.fb-col'), h('div.fb-col'), h('div.fb-col')];
  const titles = ['Field', 'Compare', 'Value'];
  const query_ = h('div.fb-query');
  const adds = h('div.fb-adds');
  const draw = () => {
    colEls.forEach((el, c) => {
      const rs = rows(c);
      if (idx[c] >= rs.length) idx[c] = Math.max(0, rs.length - 1);
      clear(el).append(h('div.fb-title', titles[c]), ...rs.slice(0, 300).map((r, i) => h('div.fb-row' + (i === idx[c] ? '.sel' : ''), { dataset: { c, i } }, r.label)));
      el.classList.toggle('on', c === col);
      el.classList.toggle('later', c > col);
      if (c === 2 && !rs.length && noValue(pick(1).id)) el.append(h('div.fb-none', 'no value needed'));
      const s = el.querySelector('.sel'); if (s && s.scrollIntoView) s.scrollIntoView({ block: 'nearest' });
    });
    const ws = cq.words(q);
    clear(query_).append(ws.length ? h('span.fb-slash', '/') : 'no filter yet', ...ws.map((w, i) => h('button.fchip.term', { dataset: { term: i }, title: 'Remove ' + w }, w, icon('x'))));
    const t = term();
    adds.textContent = t ? 'adds  ' + t : 'pick a value';
  };
  const body = h('div.fb', query_, input, h('div.fb-cols', colEls), adds, h('div.fb-foot', '↑↓ pick · ←→ tab column · ↵ add · ctrl+x drop last · esc close'));
  const m = app.ui.modal(body, { title: 'Filter', wide: true });
  const go = c => {
    // The cursor indexes the narrowed rows: find its row in the full list before the filter goes.
    const id = pick(col).id;
    typed = ''; input.value = '';
    const i = base(col).findIndex(r => r.id === id);
    if (i >= 0) idx[col] = i;
    col = Math.max(0, Math.min(2, c)); draw();
  };
  const drop = () => { const n = cq.words(q).length; if (n) { q = cq.removeTerm(q, n - 1); apply(q); draw(); } };
  query_.addEventListener('click', e => {
    const b = e.target.closest('[data-term]'); if (!b) return;
    q = cq.removeTerm(q, Number(b.dataset.term)); apply(q); draw(); input.focus();
  });
  const step = d => { const n = rows(col).length; if (n) { idx[col] = (idx[col] + d + n) % n; for (let c = col + 1; c < 3; c++) idx[c] = 0; draw(); } };
  input.addEventListener('input', () => { typed = input.value; idx[col] = 0; for (let c = col + 1; c < 3; c++) idx[c] = 0; draw(); });
  body.addEventListener('click', e => {
    const r = e.target.closest('.fb-row'); if (!r) return;
    col = Number(r.dataset.c); idx[col] = Number(r.dataset.i); for (let c = col + 1; c < 3; c++) idx[c] = 0;
    typed = ''; input.value = ''; draw(); input.focus();
  });
  const k = m.scope;
  k.bind('ArrowDown', () => step(1), '', { input: true, hidden: true });
  k.bind('ArrowUp', () => step(-1), '', { input: true, hidden: true });
  k.bind('Tab', e => go(col + (e.shiftKey ? -1 : 1)), '', { input: true, hidden: true });
  k.bind('ArrowRight', () => go(col + 1), '', { input: true, hidden: true });
  k.bind('ArrowLeft', () => go(col - 1), '', { input: true, hidden: true });
  k.bind('PageDown', () => step(10), '', { input: true, hidden: true });
  k.bind('PageUp', () => step(-10), '', { input: true, hidden: true });
  k.bind('ctrl+x', drop, '', { input: true, hidden: true });
  k.bind('Enter', () => {
    if (col === 0 || col === 1 && !noValue(pick(1).id)) { if (rows(col).length) go(col + 1); return; }
    const t = term();
    if (t) { q = cq.addTerm(q, t); apply(q); idx[2] = 0; typed = ''; input.value = ''; draw(); }
  }, '', { input: true, hidden: true });
  draw();
  return m;
}
