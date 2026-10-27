// A list under an input that searches as you type: the issue and person inputs' (issueinput.js,
// personinput.js). ↑↓ pick, enter or tab takes one, esc closes the list.
import { h, clear, debounce } from './dom.js';
import { scope } from './keys.js';
import { T } from './i18n.js';

// combo(input, o) → {el (the box: input and list), suggest(), update(q, items), close()}.
// o.find(q) → Promise<items>; o.row(item) → nodes; o.accept(item); o.key(item) → id (keeps the cursor on an
// update); o.open (lists with nothing typed: on focus and when emptied); o.none(q) → what an empty list says.
export function combo(input, o) {
  input.setAttribute('role', 'combobox');
  input.setAttribute('aria-autocomplete', 'list');
  input.setAttribute('aria-expanded', 'false');
  const pop = h('div.mention-pop.iin-pop', { role: 'listbox', hidden: true });
  let items = [], sel = 0, seq = 0, keys = null;
  const key = o.key || (x => x);

  const close = () => {
    seq++; pop.hidden = true; items = []; input.setAttribute('aria-expanded', 'false');
    removeEventListener('scroll', place, true); removeEventListener('resize', place);
  };
  const paint = () => {
    if (document.activeElement !== input) return close();
    const q = input.value.trim();
    clear(pop).append(...(items.length ? items.map((it, i) => h('div.mp.iin-row' + (i === sel ? '.sel' : ''), { role: 'option', 'aria-selected': String(i === sel),
      onmousedown: e => { e.preventDefault(); accept(i); } }, ...o.row(it)))
      : [h('div.iin-none.faint', o.none ? o.none(q) : T('No matches'))]));
    const was = pop.hidden;
    pop.hidden = false; input.setAttribute('aria-expanded', 'true');
    place();
    if (was) { addEventListener('scroll', place, true); addEventListener('resize', place); }
    const s = pop.querySelector('.sel'); if (s) s.scrollIntoView({ block: 'nearest' });
  };
  // Fixed to the viewport, so the dialog's own scrolling box doesn't cut it off: under the input, or over it
  // when there is more room there.
  const GAP = 2, EDGE = 8;
  function place() {
    if (pop.hidden) return;
    const r = input.getBoundingClientRect(), below = innerHeight - r.bottom - GAP - EDGE, above = r.top - GAP - EDGE;
    const up = below < Math.min(pop.scrollHeight, 240) && above > below;
    Object.assign(pop.style, { left: r.left + 'px', width: r.width + 'px', maxHeight: Math.max(up ? above : below, 80) + 'px',
      top: up ? 'auto' : r.bottom + GAP + 'px', bottom: up ? innerHeight - r.top + GAP + 'px' : 'auto' });
  }
  const search = debounce(n => {
    const q = input.value.trim();
    if (!q && !o.open) return close();
    o.find(q).then(r => { if (n === seq) { items = r; sel = 0; paint(); } }, () => { if (n === seq) close(); });
  }, 200);
  const suggest = () => { const n = ++seq; if (!input.value.trim() && !o.open) return close(); search(n); };
  // A later, better list for q (slower suggestions): replaces the open one, the cursor kept on its row.
  const update = (q, r) => {
    if (pop.hidden || input.value.trim() !== q) return;
    const at = items[sel] && key(items[sel]);
    items = r; sel = Math.max(0, items.findIndex(it => key(it) === at)); paint();
  };

  function accept(i) {
    const it = items[i]; if (it === undefined) return;
    close();
    o.accept(it);
  }

  input.addEventListener('input', suggest);
  input.addEventListener('focus', () => {
    // Over the dialog the input is in: while the list is open, its keys are the list's.
    keys = scope('combo', { layer: 3 });
    const open = () => !pop.hidden;
    const step = d => () => { if (items.length) { sel = (sel + d + items.length) % items.length; paint(); } };
    keys.bind('ArrowDown', step(1), '', { input: true, hidden: true, when: open });
    keys.bind('ArrowUp', step(-1), '', { input: true, hidden: true, when: open });
    keys.bind(['Enter', 'Tab'], () => accept(sel), '', { input: true, hidden: true, when: () => open() && items.length > 0 });
    keys.bind('Escape', close, '', { input: true, hidden: true, when: open });
    keys.bind('ArrowDown', suggest, '', { input: true, hidden: true, when: () => !open() });
    if (o.open && !input.value.trim()) suggest();
  });
  input.addEventListener('blur', () => { close(); if (keys) { keys.dispose(); keys = null; } });

  return { el: h('div.iin-box', input, pop), suggest, update, close };
}
