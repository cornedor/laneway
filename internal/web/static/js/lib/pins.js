// Pinned issues (★): kept in the `pins` pref as [[key, summary], …], each a
// palette command first in the list; they outlive the view. `*` on the board,
// the panel and the issue page toggles one; pins:changed tells the others.
import { T } from './i18n.js';
let cmds = [];

export function list(app) {
  let l = [];
  try { l = JSON.parse(app.prefs.get('pins', '[]')); } catch (e) { l = []; }
  return Array.isArray(l) ? l.filter(p => Array.isArray(p) && p[0]) : [];
}

export const has = (app, key) => list(app).some(p => p[0] === key);

export function register(app) {
  cmds.forEach(u => u()); cmds = [];
  for (const p of list(app)) cmds.push(app.commands.register({ id: 'pin:' + p[0], title: '★ ' + p[0] + '  ' + (p[1] || ''), group: 'Pinned', run: () => app.panel.open(p[0]) }));
}

export function toggle(app, key, summary) {
  if (!key) return;
  const l = list(app), on = l.some(p => p[0] === key);
  app.prefs.set('pins', JSON.stringify(on ? l.filter(p => p[0] !== key) : [...l, [key, summary || '']]));
  register(app);
  app.bus.emit('pins:changed', { key, on: !on });
  app.ui.toast(on ? T('Unpinned %s', key) : T('Pinned %s  ·  first in the palette', key));
}
