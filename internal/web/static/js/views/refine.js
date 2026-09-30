// Refinement (TUI: ctrl+e): the view's open issues one at a time in a wide panel, unestimated first.
// J next, K back, esc ends it and copies what changed.
import { h } from '../lib/dom.js';

let active = null;

export async function startRefine(app) {
  if (active) return active.end();
  const { api, ui, bus } = app;
  const keys = [...new Set([...document.querySelectorAll('#view [data-key]')].map(e => e.dataset.key).filter(k => /^[A-Z][A-Z0-9]*-\d+$/.test(k)))];
  if (!keys.length) return ui.toast('No issues in this view to refine', { kind: 'err' });
  let cards;
  try { cards = (await api.get('/search?jql=' + encodeURIComponent('key in (' + keys.join(',') + ')'))).cards; } catch (e) { return ui.errToast(e); }
  const order = new Map(keys.map((k, i) => [k, i]));
  const queue = cards.filter(c => !c.Done).sort((a, b) => (!a.Points - !b.Points) * -1 || order.get(a.Key) - order.get(b.Key)).map(c => c.Key);
  if (!queue.length) return ui.toast('Nothing left to refine', { kind: 'ok' });

  const changed = new Set();
  let i = 0, ending = false;
  const bar = h('div.refine-bar');
  document.body.append(bar);
  const w0 = getComputedStyle(document.documentElement).getPropertyValue('--panel-w');
  document.documentElement.style.setProperty('--panel-w', Math.max(parseInt(w0) || 0, 640) + 'px');
  const scope = app.keys.scope('refine');
  const show = () => {
    bar.textContent = 'Refining ' + (i + 1) + ' of ' + queue.length + ' · J next · K back · esc done';
    app.panel.open(queue[i]);
  };
  const step = d => { const n = i + d; if (n < 0 || n >= queue.length) return; i = n; show(); };
  scope.bind('J', () => step(1), 'refine: next issue', { group: 'Refine', input: false });
  scope.bind('K', () => step(-1), 'refine: previous issue', { group: 'Refine' });
  const offChange = bus.on('issue:changed', e => { if (e && e.key) changed.add(e.key); });
  const offPanel = bus.on('panel', e => { if (!e.key) end(); });
  const offRoute = bus.on('route', () => end());
  async function end() {
    if (ending) return; ending = true;
    scope.dispose(); offChange(); offPanel(); offRoute(); bar.remove();
    if (w0) document.documentElement.style.setProperty('--panel-w', w0); else document.documentElement.style.removeProperty('--panel-w');
    active = null;
    const list = [...changed];
    if (list.length) {
      try { await navigator.clipboard.writeText(list.join('\n')); } catch (e) { /* no clipboard */ }
      ui.toast('Refined: ' + list.join(', ') + ' (copied)', { kind: 'ok', ms: 6000 });
    }
  }
  active = { end };
  show();
}
