// Waiting on my review: the issues named by the pull and merge requests gh and glab list for you.
import { h, clear, delegate } from '../lib/dom.js';
import { css } from '../lib/css.js';

export default function mount(el, { app, scope, toolbar }) {
  css('agents');
  const { api, ui } = app;
  let cards = [], sel = 0, dead = false;
  const list = h('div.review-list'), note = h('div.empty');
  el.append(h('div.review', note, list));

  function paint() {
    const kids = cards.map((c, i) => h('div.rv-row' + (i === sel ? '.sel' : ''), { dataset: { key: c.Key, i } },
      h('span.mono', c.Key), h('span.sum', c.Summary), ui.statusPill(c.Status, c.Done ? 'done' : c.InProgress ? 'indeterminate' : 'new'), c.Assignee && h('span.dim', c.Assignee)));
    list.replaceChildren(...kids);
    const r = list.querySelector('.sel'); if (r) r.scrollIntoView({ block: 'nearest' });
  }
  async function load() {
    note.hidden = false; note.textContent = 'Asking gh and glab what waits on your review…'; clear(list);
    try {
      const r = await api.get('/review', { fresh: true });
      if (dead) return;
      cards = r.Cards || []; sel = Math.min(sel, Math.max(cards.length - 1, 0));
      note.hidden = cards.length > 0;
      note.textContent = r.Requests ? 'Nothing waits on your review (no issue keys in the requests\' titles or branches).' : 'Nothing waits on your review.';
      paint();
    } catch (e) { if (!dead) { note.hidden = false; note.textContent = e.message; } }
  }
  const move = d => { if (!cards.length) return; sel = Math.max(0, Math.min(cards.length - 1, sel + d)); paint(); };
  const G = 'Review';
  scope.bind(['j', 'ArrowDown'], () => move(1), 'next', { group: G });
  scope.bind(['k', 'ArrowUp'], () => move(-1), 'previous', { group: G });
  scope.bind('Enter', () => cards[sel] && app.panel.open(cards[sel].Key), 'open issue', { group: G });
  scope.bind('o', () => cards[sel] && window.open(app.session.baseURL + '/browse/' + cards[sel].Key, '_blank', 'noopener'), 'open in Jira', { group: G });
  scope.bind('y', () => cards[sel] && navigator.clipboard && navigator.clipboard.writeText(cards[sel].Key).then(() => ui.toast('Copied ' + cards[sel].Key)), 'copy key', { group: G });
  scope.bind('r', load, 'refresh', { group: G });
  delegate(list, 'click', '.rv-row', (e, t) => { sel = +t.dataset.i; paint(); app.panel.open(t.dataset.key); });
  const off = app.bus.on('issue:changed', () => {});
  load();
  return () => { dead = true; off(); };
}
