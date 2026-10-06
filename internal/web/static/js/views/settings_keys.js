// Keyboard remapping: any web binding can get another key. Each group of keys is folded behind one row
// (host.folds holds the open ones); a filter shows the matching keys whatever is folded. Remaps live in the `keymap` pref
// ({bindId: [spec]}); `ui.keys` of the config file applies to the bindings that have a TUI action.
import { h } from '../lib/dom.js';
import { icon } from '../lib/icons.js';
import { keyOf, kbd } from '../lib/keys.js';

const userMap = app => { try { return JSON.parse(app.prefs.get('keymap', '{}')) || {}; } catch (e) { return {}; } };
const apply = (app, map) => { app.prefs.set('keymap', JSON.stringify(map)); app.keys.configure({ user: map }); };

const kbds = specs => specs.map((s, i) => h('span.st-spec', i ? h('span.faint', ' or ') : null, kbd(s).map(k => h('kbd', k))));

// Waits for a key (or keys in sequence, 1s of quiet ends it); resolves the spec or null.
function capture(app, r) {
  return new Promise(resolve => {
    let seq = [], timer = 0, note = '';
    const shown = h('div.cap-keys'), msg = h('div.cap-msg');
    const pad = h('div.cap', { tabindex: 0, role: 'textbox', 'aria-label': 'Press the new key' }, shown, msg);
    const paint = () => { shown.replaceChildren(...(seq.length ? kbd(seq.join(' ')).map(k => h('kbd', k)) : [h('span.faint', 'press a key…')])); msg.textContent = note; msg.classList.toggle('bad', !!note); };
    let m;
    const finish = v => { clearTimeout(timer); resolve(v); m.close(); };
    pad.addEventListener('keydown', e => {
      const k = keyOf(e); if (!k) return;
      if (k === 'Escape') return;
      e.preventDefault(); e.stopPropagation();
      clearTimeout(timer);
      if (k === 'Backspace' && seq.length) seq.pop(); else seq.push(k);
      const spec = seq.join(' '), c = seq.length ? app.keys.conflict(r.id, spec) : null;
      note = c ? 'Already used by “' + c.desc + '” (' + c.group + ')' : '';
      paint();
      if (seq.length && !c) timer = setTimeout(() => finish(spec), 1000);
    });
    m = app.ui.modal(h('div', h('p', r.desc, h('span.faint', '  ' + r.group)), pad, h('div.faint.cap-hint', 'One key, or a sequence like “g x”. It is taken after a second. Backspace removes the last key, esc cancels.')), { title: 'New key', onClose: () => resolve(null) });
    paint();
    pad.focus();
  });
}

export function keyOptions(app, host) {
  const rows = app.keys.registry().filter(r => r.scope !== 'settings');
  rows.sort((a, b) => a.group.localeCompare(b.group) || a.desc.localeCompare(b.desc));
  const out = [];
  out.push({ name: 'Remap keys', section: 'Keyboard', static: true, wide: true, desc: 'Open a group, then enter on a key to give it another. Overrides ui.keys of the config file; views not opened yet list their keys after the first visit.', render: () => h('span') });
  if (Object.keys(userMap(app)).length) out.push({ name: 'Reset all remaps', section: 'Keyboard', desc: 'back to the defaults and ui.keys', render: () => h('span.st-val', h('button.btn', { tabindex: -1, onclick: () => resetAll() }, 'Reset')), change: () => resetAll() });
  const resetAll = () => { apply(app, {}); app.ui.toast('Keys reset', { kind: 'ok' }); host.reload(); };
  const keyRow = r0 => {
    const o = { name: r0.desc, section: 'Keyboard', group: r0.group, key: true, meta: '', desc: '' };
    const cur = () => app.keys.registry().find(x => x.id === r0.id) || r0;
    const rebind = async () => {
      const spec = await capture(app, cur());
      if (!spec) return;
      const map = userMap(app);
      if (spec === r0.def && !(cur().fromConfig)) delete map[r0.id]; else map[r0.id] = [spec];
      apply(app, map); host.redraw(o);
    };
    const reset = () => { const map = userMap(app); if (map[r0.id]) { delete map[r0.id]; apply(app, map); host.redraw(o); host.reload(); } };
    o.render = () => {
      const r = cur();
      return h('span.st-val', kbds(r.specs), r.changed && h('span.faint.st-was', 'default ' + kbd(r.def).join(' ')), r.fromConfig && h('span.chip', 'ui.keys'),
        userMap(app)[r0.id] && h('button.btn.ghost.st-x', { tabindex: -1, title: 'Back to the default (del)', 'aria-label': 'Reset ' + r0.desc, onclick: e => { e.stopPropagation(); reset(); } }, '×'));
    };
    o.activate = rebind; o.change = () => rebind(); o.reset = reset;
    return o;
  };
  for (const g of [...new Set(rows.map(r => r.group))]) {
    const keys = rows.filter(r => r.group === g);
    const fold = { name: g, section: 'Keyboard', fold: g };
    Object.defineProperty(fold, 'desc', { get: () => { const n = keys.filter(r => userMap(app)[r.id]).length; return keys.length + (keys.length === 1 ? ' key' : ' keys') + (n ? ' · ' + n + ' remapped' : ''); } });
    fold.render = () => h('span.st-val', icon(host.folds.has(g) ? 'chevron-up' : 'chevron-down'));
    fold.activate = fold.change = () => { host.folds.has(g) ? host.folds.delete(g) : host.folds.add(g); host.reload(); };
    out.push(fold, ...keys.map(keyRow)); // each group's keys right under its row
  }
  return out;
}
