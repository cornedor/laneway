// Standup: what I did since the previous workday, what's next, blockers; or the team's, per person.
import { h, clear, delegate } from '../lib/dom.js';
import { css } from '../lib/css.js';
import { duration } from '../lib/fmt.js';
import { ymd, addDays, workdays } from '../lib/worktime.js';

const clip = (s, n) => (s.length > n ? s.slice(0, n - 1) + '…' : s);
const catOf = c => (c.Done ? 'done' : c.InProgress ? 'indeterminate' : 'new');

// What happened on an issue, in one line: changes and comments, the logged work summed.
function what(events) {
  const parts = [];
  let logged = 0;
  for (const e of events) {
    if (e.Logged) { logged += e.Logged; continue; }
    const w = clip(e.What.replace(/\s+/g, ' '), 90);
    if (!parts.includes(w)) parts.push(w);
  }
  if (logged) parts.push('logged ' + duration(logged));
  return parts.join('; ');
}

export default function mount(el, { app, scope, toolbar }) {
  css('work');
  const { api, ui, prefs } = app;
  const wd = workdays(app);
  const prevWorkday = d => { let x = addDays(d, -1); while (!wd.includes(x.getDay())) x = addDays(x, -1); return x; };
  const midnight = new Date(); midnight.setHours(0, 0, 0, 0);
  let since = prevWorkday(midnight), team = false, data = null, err = '', dead = false;
  let project = prefs.get('standup_project', (app.session.projects || [])[0] || '');
  let items = [], sel = 0, text = '';
  const me = (app.session.me && app.session.me.AccountID) || '';

  const root = h('div.standup');
  el.append(root);

  function sections() {
    const byKey = new Map(), cardOf = new Map((data.cards || []).map((c, i) => [c.Key, { c, i }]));
    const loose = [];
    for (const e of data.entries) {
      if (!e.Key) { loose.push(e); continue; }
      const s = byKey.get(e.Key) || byKey.set(e.Key, { key: e.Key, summary: e.Summary, card: cardOf.get(e.Key)?.c, rank: cardOf.get(e.Key)?.i ?? 1e6, events: [] }).get(e.Key);
      s.events.push(e);
    }
    const sorted = [...byKey.values()].sort((a, b) => a.rank - b.rank);
    if (team) {
      const by = new Map();
      for (const e of data.entries) (by.get(e.Who) || by.set(e.Who, new Map()).get(e.Who));
      const out = [];
      for (const who of by.keys()) {
        const m = new Map();
        for (const e of data.entries) if (e.Who === who && e.Key) (m.get(e.Key) || m.set(e.Key, { key: e.Key, summary: e.Summary, card: cardOf.get(e.Key)?.c, events: [] }).get(e.Key)).events.push(e);
        out.push({ name: who, rows: [...m.values()] });
      }
      return out;
    }
    const mine = (data.cards || []).filter(c => c.AssigneeID === me && !c.Done);
    const done = sorted.filter(s => s.card?.Done), doing = sorted.filter(s => s.card && !s.card.Done && s.card.InProgress), touched = sorted.filter(s => !s.card?.Done && !(s.card && s.card.InProgress));
    for (const c of mine) if (c.InProgress && !byKey.has(c.Key)) doing.push({ key: c.Key, summary: c.Summary, card: c, events: [], quiet: true });
    const next = mine.filter(c => !c.InProgress && !c.Flagged && !byKey.has(c.Key)).slice(0, 3).map(c => ({ key: c.Key, summary: c.Summary, card: c, events: [] }));
    const blocked = mine.filter(c => c.Flagged).map(c => ({ key: c.Key, summary: c.Summary, card: c, events: [] }));
    const out = [
      { name: 'Done since ' + since.toLocaleDateString(undefined, { weekday: 'long' }), rows: done },
      { name: 'In progress', rows: doing }, { name: 'Also touched', rows: touched }, { name: 'Next', rows: next }, { name: 'Blockers', rows: blocked },
    ].filter(s => s.rows.length);
    if (loose.length) out.push({ name: 'No ticket', rows: loose.map(e => ({ key: '', summary: e.What, events: [] })) });
    out.done = done; out.doing = doing; out.touched = touched; out.next = next; out.blocked = blocked;
    return out;
  }

  function copyText(secs) {
    const line = s => '- ' + (s.key ? s.key + ' ' + s.summary : s.summary);
    if (team) return secs.map(s => s.name + '\n' + s.rows.map(r => line(r) + ': ' + what(r.events)).join('\n')).join('\n\n');
    const y = [...secs.done, ...secs.doing, ...secs.touched].filter(s => s.events.length);
    const t = [...secs.doing, ...secs.next].map(s => line(s));
    return ['Yesterday', ...(y.length ? y.map(s => line(s) + ': ' + what(s.events)) : ['- nothing on record']), '', 'Today', ...t, '', 'Blockers', ...(secs.blocked.length ? secs.blocked.map(line) : ['- none'])].join('\n');
  }

  function paint() {
    clear(root);
    const label = since.toLocaleDateString(undefined, { weekday: 'long', day: 'numeric', month: 'short' });
    root.append(h('div.sthead', h('h2', team ? 'Team standup' : 'Standup'), h('span.dim', 'since ' + label), h('span.spacer'),
      h('button.btn.ghost', { onclick: () => step(-1), title: '[' }, '‹ earlier'), h('button.btn.ghost', { onclick: () => step(1), title: ']' }, 'later ›'),
      h('button.btn', { onclick: () => setTeam(!team), title: 'Tab' }, team ? 'Mine' : 'Team'), h('button.btn', { onclick: copy, title: 'y' }, 'Copy')));
    if (err) return root.append(h('div.empty', err));
    if (!data) return root.append(h('div.loading', 'Loading…'));
    const secs = sections();
    items = []; text = copyText(secs);
    if (!secs.length) root.append(h('div.empty', 'Nothing on record since ' + label));
    for (const s of secs) {
      root.append(h('section.stsec', h('h3', s.name), s.rows.map(r => {
        const i = items.push(r.key) - 1;
        return h('div.strow' + (i === sel ? '.sel' : ''), { dataset: r.key ? { key: r.key, i } : { i } },
          h('span.wkey.mono', r.key), h('div.main', h('span.wsum', r.summary), r.events.length ? h('span.what', what(r.events)) : r.quiet && h('span.what', 'no activity')),
          r.card && ui.statusPill(r.card.Status, catOf(r.card)));
      })));
    }
    sel = Math.min(sel, Math.max(items.length - 1, 0));
  }

  function load() {
    const path = '/standup?since=' + ymd(since) + (team ? '&ids=' + teamIds.join(',') : '');
    if (team && !teamIds.length) { data = { entries: [], cards: [] }; return paint(); }
    data = null; err = ''; paint();
    return api.swr(path, d => { if (dead || path !== '/standup?since=' + ymd(since) + (team ? '&ids=' + teamIds.join(',') : '')) return; data = d; paint(); })
      .catch(e => { if (!dead) { err = e.message; paint(); } });
  }
  let teamIds = [];
  async function setTeam(on) {
    team = on; sel = 0;
    if (on) {
      data = null; paint();
      try {
        const people = await api.get('/standup/people' + (project ? '?project=' + encodeURIComponent(project) : ''));
        teamIds = people.map(p => p.ID);
      } catch (e) { ui.errToast(e); team = false; }
    }
    load();
  }
  function step(n) {
    if (n < 0) since = prevWorkday(since);
    else {
      let x = addDays(since, 1);
      while (!wd.includes(x.getDay())) x = addDays(x, 1);
      if (x < midnight) since = x;
    }
    sel = 0; load();
  }
  function copy() {
    if (!text) return;
    navigator.clipboard.writeText(text).then(() => ui.toast('Copied the standup'), e => ui.errToast(e));
  }
  async function setProject() {
    const ps = app.session.projects || [];
    if (!ps.length) return ui.toast('No projects configured');
    const p = await ui.pick({ title: 'Team of project', items: ps, label: x => x });
    if (p) { project = p; prefs.set('standup_project', p); if (team) setTeam(true); }
  }
  function move(d) {
    const n = Math.min(Math.max(sel + d, 0), items.length - 1);
    if (n === sel) return;
    sel = n; paint();
    const r = root.querySelector('.strow.sel'); if (r) r.scrollIntoView({ block: 'nearest' });
    if (app.panel.key && items[sel]) app.panel.open(items[sel]);
  }

  delegate(root, 'click', '.strow', (e, row) => { sel = +row.dataset.i; paint(); if (row.dataset.key) app.panel.open(row.dataset.key); });

  const G = { group: 'Standup' };
  scope.bind(['j', 'ArrowDown'], () => move(1), 'next', G);
  scope.bind(['k', 'ArrowUp'], () => move(-1), 'previous', G);
  scope.bind('Enter', () => { if (items[sel]) app.panel.open(items[sel]); }, 'open issue', G);
  scope.bind('[', () => step(-1), 'a workday further back', G);
  scope.bind(']', () => step(1), 'a workday forward', G);
  scope.bind('Tab', () => setTeam(!team), 'mine / team', G);
  scope.bind('P', setProject, 'team of another project', G);
  scope.bind('y', copy, 'copy as text', G);
  scope.bind('r', () => { api.forget(); load(); }, 'refresh', G);

  clear(toolbar);
  load();
  return () => { dead = true; };
}
