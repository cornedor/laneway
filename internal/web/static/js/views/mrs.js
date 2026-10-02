// Merge requests waiting on you, across GitLab instances and projects, Jira key or not (TUI: alt+m): review
// asked, assigned, and yours with comments since you last opened them (GET /api/gitlab/inbox, ui.MRInbox).
// Enter unfolds one as the development section does (lib/mr.js), o opens it in GitLab, r reads them again.
import { h, clear, delegate } from '../lib/dom.js';
import { css } from '../lib/css.js';
import { ago, isZero } from '../lib/fmt.js';
import { mrBody, glyph, diffHref } from '../lib/mr.js';

export default function mount(el, { app, scope }) {
  css('agents'); css('dev');
  const { api, ui } = app;
  let rows = [], sel = 0, dead = false;
  const open = new Set(), mrs = new Map(); // unfolded links, and each one's read: {data} | {err} | {}
  const list = h('div.review-list'), note = h('div.empty');
  el.append(h('div.review', note, list));

  function detail(u) {
    const st = mrs.get(u) || {};
    if (st.err) return h('div.dv-mr.dv-err', st.err.message || String(st.err));
    return st.data ? mrBody(st.data) : h('div.dv-mr.dv-dim', 'Loading the merge request…');
  }
  function paint() {
    const kids = [];
    rows.forEach((r, i) => {
      const m = r.MR;
      if (!i || r.Group !== rows[i - 1].Group) kids.push(h('div.mr-group', r.Group));
      kids.push(h('div.rv-row.mr-row' + (i === sel ? '.sel' : ''), { dataset: { i }, 'aria-expanded': String(open.has(m.WebURL)) },
        h('div.mr-line', m.Checks ? glyph(m.Checks.Status) : h('span.dv-check'), h('span.mono', m.Repo + '!' + m.Number), h('span.sum', m.Title),
          h('span.dim', [m.Draft && 'draft', m.Author, m.Notes && m.Notes + ' comments', !isZero(m.UpdatedAt) && ago(m.UpdatedAt)].filter(Boolean).join(' · '))),
        open.has(m.WebURL) ? detail(m.WebURL) : null));
    });
    list.replaceChildren(...kids);
    const s = list.querySelector('.sel'); if (s) s.scrollIntoView({ block: 'nearest' });
  }
  async function load() {
    note.hidden = false; note.textContent = 'Reading the merge requests waiting on you…'; clear(list);
    try {
      const r = await api.get('/gitlab/inbox', { fresh: true });
      if (dead) return;
      rows = r.Rows || []; sel = Math.min(sel, Math.max(rows.length - 1, 0));
      note.hidden = rows.length > 0 && !(r.Errs || []).length;
      note.textContent = !r.Configured ? 'No GitLab: add one under gitlab: in the config, or glab auth login.'
        : [rows.length ? '' : 'Nothing waits on you.', ...(r.Errs || [])].filter(Boolean).join('\n');
      paint();
    } catch (e) { if (!dead) { note.hidden = false; note.textContent = e.message; } }
  }
  function toggle(i) {
    const m = rows[i] && rows[i].MR; if (!m) return;
    if (open.has(m.WebURL)) { open.delete(m.WebURL); paint(); return; }
    open.add(m.WebURL);
    if (!mrs.has(m.WebURL)) {
      mrs.set(m.WebURL, {});
      api.get('/gitlab/mr?url=' + encodeURIComponent(m.WebURL), { fresh: true })
        .then(data => { mrs.set(m.WebURL, { data }); if (!dead) paint(); }, err => { mrs.set(m.WebURL, { err }); if (!dead) paint(); });
    }
    paint();
  }
  const move = d => { if (!rows.length) return; sel = Math.max(0, Math.min(rows.length - 1, sel + d)); paint(); };
  const G = 'Merge requests';
  scope.bind(['j', 'ArrowDown'], () => move(1), 'next', { group: G });
  scope.bind(['k', 'ArrowUp'], () => move(-1), 'previous', { group: G });
  scope.bind('Home', () => move(-rows.length), 'first', { group: G });
  scope.bind('End', () => move(rows.length), 'last', { group: G });
  scope.bind('PageDown', () => move(10), 'page down', { group: G });
  scope.bind('PageUp', () => move(-10), 'page up', { group: G });
  scope.bind('Enter', () => toggle(sel), 'unfold: pipeline, approvals, description', { group: G });
  scope.bind('o', () => rows[sel] && window.open(rows[sel].MR.WebURL, '_blank', 'noopener'), 'open in GitLab', { group: G });
  scope.bind('d', () => { if (rows[sel]) location.hash = diffHref(rows[sel].MR.WebURL); }, 'its diff', { group: G });
  scope.bind('r', () => { mrs.clear(); load(); }, 'refresh', { group: G });
  delegate(list, 'click', '.mr-row', (e, t) => { if (e.target.closest('a')) return; sel = +t.dataset.i; toggle(sel); });
  load();
  return () => { dead = true; };
}
