// Keyboard manager. Scopes stack up (global, the view's, a modal's); the newest
// scope's binding wins, and a `modal` scope hides all below it.
//
//   const k = keys.scope('board');            // k.dispose() when the view goes
//   k.bind('j', fn, 'next card');             // single key, typed character ("J" = shift+j, "?" as is)
//   k.bind('g b', fn, 'go to board');         // chord: keys in sequence
//   k.bind('ctrl+Enter', fn, 'save', {input: true});   // also fires inside inputs
//   k.bind(['j','ArrowDown'], fn, 'down');    // aliases
//
// Named keys: Enter Escape Tab ArrowUp ArrowDown ArrowLeft ArrowRight Backspace Delete Home End PageUp PageDown Space.
// Modifiers: ctrl+ alt+ meta+ (shift is implied by the character).
// In inputs/textareas only `input: true` bindings fire (and Escape always reaches modal scopes).
import { actionFor, fromTUI } from './keymap.js';

const scopes = [];
let pending = [], timer = 0;
// Remaps: `user` (Keyboard settings, id -> spec or specs) beats `conf` (ui.keys of the config file, action -> keys).
let user = {}, conf = {};
const registry = new Map(); // id -> every bind seen, even of views not open now (kept in localStorage for settings)
try { for (const r of JSON.parse(localStorage.getItem('lw:keyreg') || '[]')) registry.set(r.id, r); } catch (e) { /* ignore */ }
let regTimer = 0;
const listeners = new Set();
const CHORD_MS = 1200;

function norm(e) {
  let k = e.key;
  if (k === ' ') k = 'Space';
  if (k.length === 1 && e.shiftKey && /[a-z]/i.test(k)) k = k.toUpperCase();
  const mods = (e.ctrlKey ? 'ctrl+' : '') + (e.altKey ? 'alt+' : '') + (e.metaKey ? 'meta+' : '');
  if (['Shift', 'Control', 'Alt', 'Meta'].includes(k)) return null;
  return mods + k;
}
export const keyOf = norm;

// The specs a bind answers to: the user's remap, else ui.keys for its action, else its own.
function effective(id, def, action) {
  const u = user[id];
  if (u && u.length) return [].concat(u);
  const c = action && conf[action];
  if (c && c.length) { const l = c.map(fromTUI).filter(Boolean); if (l.length) return l; }
  return [def];
}
const seqs = specs => specs.map(sp => sp.split(' '));
function resolve(b) { b.specs = effective(b.id, b.def, b.action); b.spec = b.specs[0]; b.seqs = seqs(b.specs); }
function remember(b) {
  if (b.hidden || !b.desc || b.scope === 'modal') return;
  const r = registry.get(b.id), row = { id: b.id, scope: b.scope, group: b.group, def: b.def, desc: b.desc, action: b.action };
  if (r && r.desc === row.desc && r.group === row.group && r.action === row.action) return;
  registry.set(b.id, row);
  clearTimeout(regTimer);
  regTimer = setTimeout(() => { try { localStorage.setItem('lw:keyreg', JSON.stringify([...registry.values()])); } catch (e) { /* ignore */ } }, 500);
}
const inField = t => t && (t.tagName === 'INPUT' || t.tagName === 'TEXTAREA' || t.tagName === 'SELECT' || t.isContentEditable);

export function scope(name, { modal = false } = {}) {
  const s = { name, modal, binds: [], disposed: false };
  s.bind = (spec, fn, desc = '', opts = {}) => {
    for (const sp of [].concat(spec)) {
      const b = { fn, desc, input: !!opts.input, hidden: !!opts.hidden, help: !!opts.help, group: opts.group || name, def: sp, id: name + ':' + sp, action: actionFor(name, sp), when: opts.when, scope: name };
      resolve(b); s.binds.push(b); remember(b);
    }
    return s;
  };
  s.dispose = () => { const i = scopes.indexOf(s); if (i >= 0) scopes.splice(i, 1); s.disposed = true; notify(); };
  scopes.push(s); notify();
  return s;
}

function visible() {
  const out = [];
  for (let i = scopes.length - 1; i >= 0; i--) { out.push(scopes[i]); if (scopes[i].modal) break; }
  return out;
}
const match = (seq, buf) => buf.length <= seq.length && buf.every((k, i) => seq[i] === k);
// How a bind's sequences match buf: 2 whole, 1 a prefix, 0 not.
function matches(b, buf) {
  let r = 0;
  for (const seq of b.seqs) if (match(seq, buf)) { if (seq.length === buf.length) return 2; r = 1; }
  return r;
}

function onKey(e) {
  if (e.isComposing) return;
  const k = norm(e); if (!k) return;
  const field = inField(e.target);
  const buf = pending.concat(k);
  const vis = visible();
  let exact = null, prefix = false;
  for (const s of vis) {
    for (const b of s.binds) {
      if (field && !b.input) continue;
      if (b.when && !b.when()) continue;
      const m = matches(b, buf);
      if (m === 2) { if (!exact) exact = b; } else if (m === 1) prefix = true;
    }
    if (exact) break;
  }
  if (exact) {
    clearPending(); e.preventDefault(); e.stopPropagation();
    exact.fn(e); return;
  }
  if (prefix) { e.preventDefault(); pending = buf; clearTimeout(timer); timer = setTimeout(clearPending, CHORD_MS); notify(); return; }
  if (pending.length) { clearPending(); if (!field) e.preventDefault(); }
}
function clearPending() { pending = []; clearTimeout(timer); notify(); }
function notify() { for (const fn of listeners) fn(); }

document.addEventListener('keydown', onKey, true);

export const keys = {
  scope,
  pending: () => pending.join(' '),
  onChange: fn => { listeners.add(fn); return () => listeners.delete(fn); },
  // Remapping. configure({user, conf}) re-resolves every bind; user = {id: spec|[specs]}, conf = ui.keys of the config.
  configure(o) {
    if (o.user) user = o.user;
    if (o.conf) conf = o.conf;
    for (const s of scopes) for (const b of s.binds) resolve(b);
    notify();
  },
  // Every bind seen in this browser: [{id, scope, group, desc, def, specs, changed}].
  registry() {
    return [...registry.values()].map(r => { const specs = effective(r.id, r.def, r.action); return { ...r, specs, changed: specs.length !== 1 || specs[0] !== r.def, fromConfig: !user[r.id] && specs[0] !== r.def }; });
  },
  // The bind already answering to spec in the scope of `id` (or a global one), or null.
  conflict(id, spec) {
    const me = registry.get(id); if (!me) return null;
    for (const r of keys.registry()) {
      if (r.id === id) continue;
      const shared = r.scope === me.scope || GLOBAL.has(r.scope) || GLOBAL.has(me.scope);
      if (shared && r.specs.some(x => x === spec || (x.startsWith(spec + ' ')) || spec.startsWith(x + ' '))) return r;
    }
    return null;
  },
  // Bindings a user can press now, newest scope first: [{group, spec, desc, rank}].
  // rank 0 view, 1 a panel waiting for focus, 2 global. `help: true` binds are listed while their `when` is false.
  active() {
    const out = [], seen = new Set();
    for (const s of visible()) for (const b of s.binds) {
      if (b.hidden || !b.desc) continue;
      const on = !b.when || b.when();
      if (!on && !b.help) continue;
      const id = b.spec + (on ? '' : '~'); if (seen.has(id)) continue; seen.add(id);
      out.push({ group: on ? b.group : b.group + ' (Tab to focus)', spec: b.spec, specs: b.specs, desc: b.desc, rank: !on ? 1 : GLOBAL.has(s.name) ? 2 : 0 });
    }
    return out;
  },
};
const GLOBAL = new Set(['global', 'timer', 'undo']);
export const kbd = spec => spec.split(' ').map(k => k.replace('ArrowUp', '↑').replace('ArrowDown', '↓').replace('ArrowLeft', '←').replace('ArrowRight', '→').replace('Escape', 'esc').replace('Enter', '⏎').replace('Space', '␣').replace('ctrl+', '⌃').replace('alt+', '⌥').replace('meta+', '⌘'));
export default keys;
