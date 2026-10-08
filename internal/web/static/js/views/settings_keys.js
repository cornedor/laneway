// Keyboard remapping: any web binding can get another key. Each group of keys is folded behind one row
// (host.folds holds the open ones); a filter shows the matching keys whatever is folded. Remaps go to the config file:
// ui.keys for a binding with a TUI action (the terminal follows), ui.web_keys by bind id for the rest.
import { h } from '../lib/dom.js';
import { icon } from '../lib/icons.js';
import { keyOf, kbd, NO_KEY } from '../lib/keys.js';
import { specToTUI } from '../lib/keymap.js';
import { put } from './settings_config.js';
import { T, Tn } from '../lib/i18n.js';

const maps = app => ({ keys: { ...(app.session.ui.Keys || {}) }, web_keys: { ...(app.session.ui.WebKeys || {}) } });
// save writes one of the two maps; the keys follow once the file has it.
async function save(app, name, map) {
  try { await put(app, name, { Value: Object.keys(map).length ? map : null }); } catch (e) { app.ui.errToast(e); return false; }
  app.session.ui[name === 'keys' ? 'Keys' : 'WebKeys'] = map;
  app.keys.configure({ conf: app.session.ui.Keys || {}, web: app.session.ui.WebKeys || {} });
  return true;
}
// Where a binding's remap lives: [option, entry name].
const slot = r => (r.action ? ['keys', r.action] : ['web_keys', r.id]);

const kbds = specs => specs.map((s, i) => h('span.st-spec', i ? h('span.faint', T(' or ')) : null, kbd(s).map(k => h('kbd', k))));

// Waits for a key (or keys in sequence, 1s of quiet ends it); resolves the spec, NO_KEY to unbind, or null.
function capture(app, r) {
  return new Promise(resolve => {
    let seq = [], timer = 0, note = '';
    const shown = h('div.cap-keys'), msg = h('div.cap-msg');
    const pad = h('div.cap', { tabindex: 0, role: 'textbox', 'aria-label': T('Press the new key') }, shown, msg);
    const paint = () => { shown.replaceChildren(...(seq.length ? kbd(seq.join(' ')).map(k => h('kbd', k)) : [h('span.faint', T('press a key…'))])); msg.textContent = note; msg.classList.toggle('bad', !!note); };
    let m;
    const finish = v => { clearTimeout(timer); resolve(v); m.close(); };
    pad.addEventListener('keydown', e => {
      const k = keyOf(e); if (!k) return;
      if (k === 'Escape' || k === 'Tab' || k === 'shift+Tab') return; // tab reaches the No key button
      e.preventDefault(); e.stopPropagation();
      clearTimeout(timer);
      if (k === 'Backspace' && seq.length) seq.pop(); else seq.push(k);
      const spec = seq.join(' '), c = seq.length ? app.keys.conflict(r.id, spec) : null;
      note = c ? T('Already used by “%s” (%s)', c.desc, c.group) : '';
      paint();
      if (seq.length && !c) timer = setTimeout(() => finish(spec), 1000);
    });
    m = app.ui.modal(h('div', h('p', r.desc, h('span.faint', '  ' + r.group)), pad, h('div.faint.cap-hint', T('One key, or a sequence like “g x”. It is taken after a second. Backspace removes the last key, tab reaches No key, esc cancels.')),
      h('div.cap-none', h('button.btn', { onclick: () => finish(NO_KEY) }, T('No key')))), { title: T('New key'), onClose: () => resolve(null) });
    paint();
    pad.focus();
  });
}

export function keyOptions(app, host) {
  const rows = app.keys.registry().filter(r => r.scope !== 'settings');
  rows.sort((a, b) => a.group.localeCompare(b.group) || a.desc.localeCompare(b.desc));
  const out = [];
  out.push({ name: T('Remap keys'), section: 'Keyboard', static: true, wide: true, desc: T('Open a group, then enter on a key to give it another. Saved in the config file: ui.keys when the terminal has the action (it follows), else ui.web_keys. Views not opened yet list their keys after the first visit.'), render: () => h('span') });
  const m0 = maps(app);
  if (Object.keys(m0.keys).length || Object.keys(m0.web_keys).length) out.push({ name: T('Reset all remaps'), section: 'Keyboard', desc: T('empties ui.keys and ui.web_keys of the config file, for the terminal too'), render: () => h('span.st-val', h('button.btn', { tabindex: -1, onclick: () => resetAll() }, T('Reset'))), change: () => resetAll() });
  const resetAll = async () => {
    if (!(await save(app, 'keys', {})) || !(await save(app, 'web_keys', {}))) return;
    app.ui.toast(T('Keys reset'), { kind: 'ok' }); host.reload();
  };
  const keyRow = r0 => {
    const o = { name: r0.desc, section: 'Keyboard', group: r0.group, key: true, meta: '', desc: '' };
    const cur = () => app.keys.registry().find(x => x.id === r0.id) || r0;
    const rebind = async () => {
      const spec = await capture(app, cur());
      if (!spec) return;
      const [name, entry] = slot(r0), t = spec === NO_KEY ? NO_KEY : specToTUI(spec);
      if (!t || (r0.action && spec.includes(' '))) return app.ui.toast(t ? T('%s is a sequence; the terminal takes one key for %s', kbd(spec).join(' '), r0.action || r0.desc) : T('%s cannot go in the config for %s', kbd(spec).join(' '), r0.action || r0.desc), { kind: 'err' });
      const map = maps(app)[name];
      if (spec === r0.def) delete map[entry]; else map[entry] = [t];
      if (await save(app, name, map)) host.reload(); // a ui.keys entry can move other rows too
    };
    const reset = async () => {
      const [name, entry] = slot(r0), map = maps(app)[name];
      if (!(entry in map)) return;
      delete map[entry];
      if (await save(app, name, map)) host.reload();
    };
    o.render = () => {
      const r = cur();
      return h('span.st-val', r.specs.length ? kbds(r.specs) : h('span.faint', T('no key')), r.changed && h('span.faint.st-was', T('default %s', kbd(r.def).join(' '))), r.from && h('span.chip', r.from),
        r.from && h('button.btn.ghost.st-x', { tabindex: -1, title: T('Back to the default (del)'), 'aria-label': T('Reset %s', r0.desc), onclick: e => { e.stopPropagation(); reset(); } }, '×'));
    };
    o.activate = rebind; o.change = () => rebind(); o.reset = reset;
    return o;
  };
  for (const g of [...new Set(rows.map(r => r.group))]) {
    const keys = rows.filter(r => r.group === g);
    const fold = { name: g, section: 'Keyboard', fold: g };
    Object.defineProperty(fold, 'desc', { get: () => { const n = app.keys.registry().filter(r => r.group === g && r.from).length; return Tn(keys.length, '%d key', '%d keys', keys.length) + (n ? T(' · %d remapped', n) : ''); } });
    fold.render = () => h('span.st-val', icon(host.folds.has(g) ? 'chevron-up' : 'chevron-down'));
    fold.activate = fold.change = () => { host.folds.has(g) ? host.folds.delete(g) : host.folds.add(g); host.reload(); };
    out.push(fold, ...keys.map(keyRow)); // each group's keys right under its row
  }
  return out;
}
