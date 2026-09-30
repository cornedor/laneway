import { h } from '../lib/dom.js';
import { kbd } from '../lib/keys.js';

// Lists every binding pressable right now, grouped.
export function openHelp(app) {
  const groups = new Map();
  for (const b of app.keys.active()) (groups.get(b.group) || groups.set(b.group, []).get(b.group)).push(b);
  const body = h('div.help', [...groups].map(([g, bs]) => h('section', h('h3', g),
    h('dl', bs.map(b => [h('dt', kbd(b.spec).map(k => h('kbd', k))), h('dd', b.desc)])))));
  app.ui.modal(body, { title: 'Keyboard', wide: true });
}
