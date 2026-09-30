// Shared widgets: toast, modal, pick (fuzzy list picker), prompt, confirm, avatar, chip.
import { h, clear } from './dom.js';
import { keys } from './keys.js';
import { fuzzy } from './fuzzy.js';
import { initials, hue } from './fmt.js';


// toast('Moved', {kind:'ok'|'err'|'info', action:{label, run}, ms})
export function toast(msg, { kind = 'info', action, ms } = {}) {
  const host = document.getElementById('toasts');
  const t = h('div.toast.' + kind, { role: kind === 'err' ? 'alert' : 'status' }, h('span', msg),
    action && h('button.btn.link', { onclick: () => { action.run(); close(); } }, action.label));
  const close = () => { t.remove(); };
  host.append(t);
  setTimeout(close, ms || (kind === 'err' ? 6000 : action ? 6000 : 2800));
  return close;
}
export const errToast = e => toast(e && e.message ? e.message : String(e), { kind: 'err' });

// modal(node, {title, wide, onClose}) → {close, el}. Esc closes; focus returns.
export function modal(content, { title, wide = false, onClose, className = '' } = {}) {
  const returnTo = document.activeElement; // per modal: a picker opened inside a modal returns to it
  const back = h('div.overlay', { onmousedown: e => { if (e.target === back) close(); } });
  const box = h('div.modal' + (wide ? '.wide' : '') + (className ? '.' + className : ''), { role: 'dialog', 'aria-modal': 'true', 'aria-label': title || '' },
    title && h('div.modal-title', title), content);
  back.append(box);
  document.body.append(back);
  const k = keys.scope('modal', { modal: true });
  k.bind('Escape', () => close(), 'close', { input: true, hidden: true });
  let closed = false;
  function close(val) {
    if (closed) return; closed = true;
    k.dispose(); back.remove();
    if (returnTo && returnTo.isConnected && returnTo.focus) returnTo.focus();
    if (onClose) onClose(val);
  }
  // Keep Tab inside the dialog; a modal above this one leaves the trap to it.
  back.addEventListener('keydown', e => {
    if (e.key !== 'Tab' || e.defaultPrevented) return;
    const items = [...box.querySelectorAll('a[href],button,input,select,textarea,[tabindex]')].filter(x => !x.disabled && x.tabIndex >= 0 && x.offsetParent !== null);
    if (!items.length) { e.preventDefault(); return; }
    const a = document.activeElement, i = items.indexOf(a);
    if (e.shiftKey && (i <= 0)) { e.preventDefault(); items[items.length - 1].focus(); }
    else if (!e.shiftKey && (i < 0 || i === items.length - 1)) { e.preventDefault(); items[0].focus(); }
  });
  const first = box.querySelector('[autofocus],input,textarea,select,button');
  if (first) first.focus();
  return { close, el: box, scope: k };
}

// pick({title, items, label(item), detail?(item), render?(item, match)→node, multi, selected, placeholder, create?})
// → Promise<item | item[] | null>. Type to filter, ↑/↓ or ctrl+n/p, Enter picks, Esc cancels.
// With `create: q => item` and no match, Enter on typed text creates an item.
// With `search: async q => items` the list is also fed by the server (debounced).
export function pick(o) {
  return new Promise(resolve => {
    const label = o.label || (x => String(x));
    let items = o.items || [], shown = [], sel = 0, q = '';
    const chosen = new Set(o.selected || []);
    const input = h('input.pick-input', { type: 'text', placeholder: o.placeholder || 'Filter…', autofocus: true, spellcheck: false, autocomplete: 'off' });
    const list = h('div.pick-list', { role: 'listbox' });
    let done = false;
    const finish = v => { if (done) return; done = true; m.close(); resolve(v); };
    const render = () => {
      const rows = [];
      for (const it of items) {
        const m = q ? fuzzy(q, label(it) + (o.detail ? ' ' + o.detail(it) : '')) : { score: 0, idx: [] };
        if (m) rows.push({ it, s: m.score, m });
      }
      if (q) rows.sort((a, b) => b.s - a.s);
      shown = rows.map(r => r.it);
      if (sel >= shown.length) sel = Math.max(0, shown.length - 1);
      clear(list);
      const frag = document.createDocumentFragment();
      rows.slice(0, 200).forEach((r, i) => {
        const row = h('div.pick-row' + (i === sel ? '.sel' : ''), { role: 'option', dataset: { i }, onmousemove: () => { if (sel !== i) { sel = i; mark(); } }, onclick: () => choose(i) },
          o.multi && h('span.check', chosen.has(r.it) ? '☑' : '☐'),
          o.render ? o.render(r.it, r.m) : h('span.pick-label', label(r.it)),
          o.detail && !o.render && h('span.pick-detail', o.detail(r.it)));
        frag.append(row);
      });
      if (!rows.length) list.append(h('div.pick-empty', o.create && q ? `Enter creates “${q}”` : (o.empty || 'No matches')));
      list.append(frag);
      scrollSel();
    };
    const mark = () => { list.querySelectorAll('.pick-row').forEach((r, i) => r.classList.toggle('sel', i === sel)); scrollSel(); };
    const scrollSel = () => { const r = list.children[sel]; if (r && r.scrollIntoView) r.scrollIntoView({ block: 'nearest' }); };
    function choose(i) {
      const it = shown[i];
      if (it === undefined) { if (o.create && q) return finish(o.multi ? [...chosen, o.create(q)] : o.create(q)); return; }
      if (o.multi) { chosen.has(it) ? chosen.delete(it) : chosen.add(it); render(); return; }
      finish(it);
    }
    input.addEventListener('input', () => { q = input.value; sel = 0; render(); if (o.search) remote(q); });
    let rt = 0;
    const remote = q => { clearTimeout(rt); rt = setTimeout(async () => { try { const r = await o.search(q); if (input.value === q) { items = r; render(); } } catch (e) { /* keep list */ } }, 180); };
    const m = modal(h('div.pick', o.title && h('div.pick-title', o.title), input, list, o.multi && h('div.pick-foot', 'space toggles · enter confirms')), { className: 'pick-modal', onClose: () => finish(null) });
    const move = d => { if (!shown.length) return; sel = (sel + d + Math.min(shown.length, 200)) % Math.min(shown.length, 200); mark(); };
    m.scope.bind(['ArrowDown', 'ctrl+n', 'Tab'], () => move(1), '', { input: true, hidden: true });
    m.scope.bind(['ArrowUp', 'ctrl+p', 'shift+Tab'], () => move(-1), '', { input: true, hidden: true });
    m.scope.bind('Enter', () => { if (o.multi) finish(o.create && q && !shown.length ? [...chosen, o.create(q)] : [...chosen]); else choose(sel); }, '', { input: true, hidden: true });
    if (o.multi) m.scope.bind('ctrl+Space', () => choose(sel), '', { input: true, hidden: true });
    if (o.multi) input.addEventListener('keydown', e => { if (e.key === ' ' && !q) { e.preventDefault(); choose(sel); } });
    render();
    if (o.search) remote('');
  });
}

// prompt({title, value, placeholder, multiline}) → Promise<string|null>
export function prompt({ title, value = '', placeholder = '', multiline = false, ok = 'OK' } = {}) {
  return new Promise(resolve => {
    const f = multiline ? h('textarea.input', { rows: 6, value, placeholder, autofocus: true }) : h('input.input', { type: 'text', value, placeholder, autofocus: true });
    let v = null;
    const m = modal(h('form.prompt', { onsubmit: e => { e.preventDefault(); v = f.value; m.close(); } }, f,
      h('div.row.end', h('button.btn', { type: 'button', onclick: () => m.close() }, 'Cancel'), h('button.btn.primary', { type: 'submit' }, ok))), { title, onClose: () => resolve(v) });
    m.scope.bind('ctrl+Enter', () => { v = f.value; m.close(); }, '', { input: true, hidden: true });
    f.select && f.select();
  });
}
export function confirm({ title, text = '', ok = 'OK', danger = false } = {}) {
  return new Promise(resolve => {
    let v = false;
    const b = h('button.btn.' + (danger ? 'danger' : 'primary'), { autofocus: true, onclick: () => { v = true; m.close(); } }, ok);
    const m = modal(h('div', h('p', text), h('div.row.end', h('button.btn', { onclick: () => m.close() }, 'Cancel'), b)), { title, onClose: () => resolve(v) });
    m.scope.bind('y', () => { v = true; m.close(); }, '', { hidden: true });
    m.scope.bind('n', () => m.close(), '', { hidden: true });
  });
}

// Avatar: an image when the API gave a URL, else coloured initials.
export function avatar(name, url, size = 20) {
  const r = v => Math.round(v / 14 * 1000) / 1000 + 'rem';
  const s = { width: r(size), height: r(size), fontSize: r(Math.round(size * 0.42)) };
  if (!name) return h('span.avatar.none', { style: s, title: 'Unassigned' }, '·');
  return h('span.avatar', { style: { ...s, background: `hsl(${hue(name)} 45% 42%)` }, title: name }, initials(name),
    url && h('img', { src: url, alt: '', loading: 'lazy', onerror: e => e.target.remove() }));
}
export const chip = (text, cls = '') => h('span.chip' + (cls ? '.' + cls : ''), text);
// Status pill coloured by status category ('new'|'indeterminate'|'done' or the card's Done/InProgress).
export const statusPill = (name, cat) => h('span.pill.cat-' + (cat || 'new'), name);
