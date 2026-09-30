// Tiny DOM helpers. h('div.card#x', {class:'a', onclick, dataset:{k:1}}, child, ...)
export function h(tag, props, ...kids) {
  let id, cls = [];
  const m = tag.match(/^([a-z0-9-]*)((?:[.#][\w-]+)*)$/i);
  let name = 'div';
  if (m) {
    name = m[1] || 'div';
    for (const p of m[2].match(/[.#][\w-]+/g) || []) (p[0] === '#' ? (id = p.slice(1)) : cls.push(p.slice(1)));
  }
  const svg = name === 'svg' || name === 'path' || name === 'g' || name === 'circle' || name === 'line' || name === 'rect' || name === 'text' || name === 'polyline' || name === 'polygon';
  const el = svg ? document.createElementNS('http://www.w3.org/2000/svg', name) : document.createElement(name);
  if (id) el.id = id;
  if (cls.length) el.setAttribute('class', cls.join(' '));
  if (props && (typeof props !== 'object' || props.nodeType || Array.isArray(props))) { kids.unshift(props); props = null; }
  if (props) for (const k in props) {
    const v = props[k];
    if (v == null || v === false) continue;
    if (k === 'class') el.setAttribute('class', (el.getAttribute('class') ? el.getAttribute('class') + ' ' : '') + v);
    else if (k === 'style' && typeof v === 'object') Object.assign(el.style, v);
    else if (k === 'dataset') Object.assign(el.dataset, v);
    else if (k.startsWith('on') && typeof v === 'function') el.addEventListener(k.slice(2), v);
    else if (k === 'html') el.innerHTML = v;
    else if (k in el && !svg) el[k] = v;
    else el.setAttribute(k, v === true ? '' : v);
  }
  add(el, kids);
  return el;
}
function add(el, kids) {
  for (const k of kids) {
    if (k == null || k === false) continue;
    if (Array.isArray(k)) add(el, k);
    else el.append(k.nodeType ? k : document.createTextNode(String(k)));
  }
}
export const $ = (sel, root = document) => root.querySelector(sel);
export const $$ = (sel, root = document) => Array.from(root.querySelectorAll(sel));
export function clear(el) { while (el.firstChild) el.removeChild(el.firstChild); return el; }
export function esc(s) { return String(s).replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c])); }
// Delegated listener: on(root, 'click', '.card', (e, el) => …)
export function delegate(root, type, sel, fn) {
  root.addEventListener(type, e => { const t = e.target.closest && e.target.closest(sel); if (t && root.contains(t)) fn(e, t); });
}
// rAF-coalesced function: many calls in a frame, one run.
export function frame(fn) { let q = 0; return (...a) => { if (q) return; q = requestAnimationFrame(() => { q = 0; fn(...a); }); }; }
export function debounce(fn, ms) { let t; return (...a) => { clearTimeout(t); t = setTimeout(() => fn(...a), ms); }; }
