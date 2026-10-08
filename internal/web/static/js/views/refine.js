// Refinement (TUI: ctrl+e): the view's open issues one at a time in a wide panel, unestimated first.
// J next, K back, esc ends it and copies what changed.
import { h } from '../lib/dom.js';
import { T, Tn } from '../lib/i18n.js';

let active = null;

export async function startRefine(app) {
  if (active) return active.end();
  const { api, ui, bus } = app;
  let cards = app.listed ? app.listed() : null;
  if (!cards) {
    // A view that does not list its cards: the keys it draws.
    const keys = [...new Set([...document.querySelectorAll('#view [data-key]')].map(e => e.dataset.key).filter(k => /^[A-Z][A-Z0-9]*-\d+$/.test(k)))];
    if (!keys.length) return ui.toast(T('No issues in this view to refine'), { kind: 'err' });
    const order = new Map(keys.map((k, i) => [k, i]));
    try { cards = (await api.get('/search?jql=' + encodeURIComponent('key in (' + keys.join(',') + ')'))).cards; } catch (e) { return ui.errToast(e); }
    cards.sort((a, b) => order.get(a.Key) - order.get(b.Key));
  }
  const open = cards.filter(c => !c.Done);
  const queue = [...open.filter(c => !c.Points), ...open.filter(c => c.Points)].map(c => c.Key);
  if (!queue.length) return ui.toast(T('No issues in this view to refine'), { kind: 'err' });

  const changes = [];
  let i = 0, ending = false;
  const bar = h('div.refine-bar');
  document.body.append(bar);
  const w0 = getComputedStyle(document.documentElement).getPropertyValue('--panel-w');
  document.documentElement.style.setProperty('--panel-w', Math.max(parseInt(w0) || 0, 640) + 'px');
  const scope = app.keys.scope('refine');
  const show = () => {
    bar.textContent = T('Refining %d of %d · J next · K back · esc done', i + 1, queue.length);
    app.panel.open(queue[i]);
  };
  const step = d => {
    const n = i + d;
    if (n >= queue.length) return ui.toast(T('That was the last · esc ends refining'));
    if (n < 0) return ui.toast(T('This is the first'));
    i = n; show();
  };
  scope.bind('J', () => step(1), T('refine: next issue'), { group: T('Refine'), input: false });
  scope.bind('K', () => step(-1), T('refine: previous issue'), { group: T('Refine') });
  const offChange = bus.on('issue:changed', e => { if (e && e.what) changes.push(e.what); });
  const offPanel = bus.on('panel', e => { if (!e.key) end(); });
  const offRoute = bus.on('route', () => end());
  async function end() {
    if (ending) return; ending = true;
    scope.dispose(); offChange(); offPanel(); offRoute(); bar.remove();
    if (w0) document.documentElement.style.setProperty('--panel-w', w0); else document.documentElement.style.removeProperty('--panel-w');
    active = null;
    if (!changes.length) return ui.toast(Tn(i + 1, 'Refined %d issue, nothing changed', 'Refined %d issues, nothing changed', i + 1));
    try { await navigator.clipboard.writeText('- ' + changes.join('\n- ')); } catch (e) { /* no clipboard */ }
    ui.toast(Tn(changes.length, 'Refined %d of %d issues, %d change · copied as a list', 'Refined %d of %d issues, %d changes · copied as a list', i + 1, queue.length, changes.length), { kind: 'ok', ms: 6000 });
  }
  active = { end };
  show();
}
