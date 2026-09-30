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
const scopes = [];
let pending = [], timer = 0;
const listeners = new Set();
const CHORD_MS = 1200;

function norm(e) {
  let k = e.key;
  if (k === ' ') k = 'Space';
  if (k.length === 1 && e.shiftKey && /[a-z]/i.test(k)) k = k.toUpperCase();
  const mods = (e.ctrlKey ? 'ctrl+' : '') + (e.altKey ? 'alt+' : '') + (e.metaKey ? 'meta+' : '');
  if (['Shift', 'Control', 'Alt', 'Meta'].includes(k)) return null;
  if (k.length === 1 && !mods && /[A-Z]/.test(k) === false && e.shiftKey) { /* symbols arrive shifted already */ }
  return mods + k;
}
const inField = t => t && (t.tagName === 'INPUT' || t.tagName === 'TEXTAREA' || t.tagName === 'SELECT' || t.isContentEditable);

export function scope(name, { modal = false } = {}) {
  const s = { name, modal, binds: [], disposed: false };
  s.bind = (spec, fn, desc = '', opts = {}) => {
    for (const sp of [].concat(spec)) s.binds.push({ seq: sp.split(' '), fn, desc, input: !!opts.input, hidden: !!opts.hidden, group: opts.group || name, spec: sp, when: opts.when });
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
      if (!match(b.seq, buf)) continue;
      if (b.seq.length === buf.length) { if (!exact) exact = b; } else prefix = true;
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
  // Bindings a user can press now, newest scope first: [{group, spec, desc}]
  active() {
    const out = [], seen = new Set();
    for (const s of visible()) for (const b of s.binds) {
      if (b.hidden || !b.desc) continue;
      const id = b.spec; if (seen.has(id)) continue; seen.add(id);
      out.push({ group: b.group, spec: b.spec, desc: b.desc });
    }
    return out;
  },
};
export const kbd = spec => spec.split(' ').map(k => k.replace('ArrowUp', '↑').replace('ArrowDown', '↓').replace('ArrowLeft', '←').replace('ArrowRight', '→').replace('Escape', 'esc').replace('Enter', '⏎').replace('Space', '␣').replace('ctrl+', '⌃').replace('alt+', '⌥').replace('meta+', '⌘'));
export default keys;
