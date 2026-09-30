// The filter builder (F): field, comparison and value side by side, each narrowed by typing
// while it has the cursor. enter adds the term to the board's query (more values of the same
// field join with a comma); the builder stays open for the next term. esc closes.
import { h, clear } from '../lib/dom.js';
import * as cq from '../lib/cardquery.js';

export function openFilterBuilder({ app, cards, env, query, apply }) {
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
      const s = el.querySelector('.sel'); if (s && s.scrollIntoView) s.scrollIntoView({ block: 'nearest' });
    });
    query_.textContent = q ? '/' + q : 'no filter yet';
    const t = term();
    adds.textContent = t ? 'adds  ' + t : 'pick a field, a comparison and a value';
  };
  const body = h('div.fb', query_, input, h('div.fb-cols', colEls), adds, h('div.fb-foot', 'tab / ←→ column · ↑↓ pick · enter adds · esc closes'));
  const m = app.ui.modal(body, { title: 'Filter', wide: true });
  const go = c => { col = Math.max(0, Math.min(2, c)); typed = ''; input.value = ''; draw(); };
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
  k.bind('ArrowRight', () => { if (!input.value) go(col + 1); }, '', { input: true, hidden: true });
  k.bind('ArrowLeft', () => { if (!input.value) go(col - 1); }, '', { input: true, hidden: true });
  k.bind('Enter', () => {
    const t = term();
    if (t && (col === 2 || noValue(pick(1).id))) {
      q = cq.addTerm(q, t); apply(q);
      idx[0] = idx[1] = idx[2] = 0; go(0);
    } else if (col < 2 && rows(col).length) go(col + 1);
  }, '', { input: true, hidden: true });
  draw();
  return m;
}
