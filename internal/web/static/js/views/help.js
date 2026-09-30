import { h } from '../lib/dom.js';
import { kbd } from '../lib/keys.js';

// Lists every binding pressable right now: view, panel, global. One row per action (aliases merged).
export function openHelp(app) {
  const groups = new Map();
  for (const b of app.keys.active().sort((a, b) => a.rank - b.rank)) {
    const g = groups.get(b.group) || groups.set(b.group, new Map()).get(b.group);
    const row = g.get(b.desc) || g.set(b.desc, []).get(b.desc);
    row.push(b.spec);
  }
  const body = h('div.help', [...groups].map(([g, rows]) => h('section', h('h3', g),
    h('dl', [...rows].map(([desc, specs]) => [
      h('dt', specs.map((s, i) => [i ? ' ' : '', kbd(s).map(k => h('kbd', k))])),
      h('dd', desc)])))));
  app.ui.modal(body, { title: 'Keyboard', wide: true });
}
