// Standup: what I did since the previous workday (with my commits), or the team's walking the board
// right to left or by person. One card at a time (space), park a card for after (P, kept per sprint).
// Rows come from /standup/lines, built like the TUI's.
import { h, clear, delegate } from '../lib/dom.js';
import { css } from '../lib/css.js';
import { projectOf, boardsOf, lastBoard, setCtx, switcher } from './plan_ctx.js';
import { ymd, addDays, workdays } from '../lib/worktime.js';

const PARK = 'Parking lot';

export default function mount(el, { app, scope, context, toolbar }) {
  css('work'); css('standup');
  const { api, ui, prefs } = app;
  const wd = workdays(app);
  const prevWorkday = d => { let x = addDays(d, -1); while (!wd.includes(x.getDay())) x = addDays(x, -1); return x; };
  const midnight = new Date(); midnight.setHours(0, 0, 0, 0);
  let since = prevWorkday(midnight), mode = 'mine', data = null, err = '', dead = false, single = false, seq = 0;
  let project = projectOf(app, {});
  let sw = null, board = null, sprint = 0, lines = [], folded = [], sel = 0, parkedKeys = [];

  const root = h('div.standup');
  el.append(root);

  const team = () => mode !== 'mine';
  const picks = l => !!(l.Key || l.Unfold);
  const parkId = () => 'standup_park.' + (board ? board.ID : 0) + '.' + sprint;
  const loadParked = () => { parkedKeys = (prefs.get(parkId(), '') || '').split(/\s+/).filter(Boolean); };

  // The project's board and its active sprint, for the team walk and the parking lot.
  async function resolveBoard() {
    if (board || !project) return;
    try {
      const bs = await boardsOf(app, project), last = lastBoard(app, project);
      board = bs.find(b => b.ID === last) || bs.find(b => /scrum/i.test(b.Type)) || bs[0] || null;
      if (board && !last) setCtx(app, project, board);
      if (board) {
        const b = await api.get('/boards/' + board.ID);
        const a = (b.sprints || []).find(s => s.State === 'active');
        sprint = a ? a.ID : 0;
      }
    } catch (e) { /* no board: mine still works */ }
  }

  function build() {
    const L = (data.Lines || []).filter(l => !l.Head || !l.Head.startsWith(PARK));
    const all = [...L, ...(data.Folded || [])];
    const byKey = new Map(all.filter(l => l.Key).map(l => [l.Key, l]));
    const lot = parkedKeys.map(k => ({ ...(byKey.get(k) || { Key: k, Title: k }), Marks: '', parked: true }));
    lines = L.map(l => (l.Key && parkedKeys.includes(l.Key) ? { ...l, Marks: [l.Marks, 'parked'].filter(Boolean).join(' · ') } : l));
    folded = (data.Folded || []).map(l => (l.Key && parkedKeys.includes(l.Key) ? { ...l, Marks: [l.Marks, 'parked'].filter(Boolean).join(' · ') } : l));
    if (lot.length) lines.push({ Head: PARK + ' (' + lot.length + ')' }, ...lot);
  }

  const cells = l => [l.Who, l.Age, l.Marks].filter(Boolean);
  const copyText = () => {
    let t = data.Text || '';
    const i = lines.findIndex(l => l.Head && l.Head.startsWith(PARK));
    if (i >= 0) t += '\n\n' + PARK + '\n' + lines.slice(i + 1).map(l => '- ' + [l.Title, l.Who, l.Age, l.Marks, l.What].filter(Boolean).join(' · ')).join('\n');
    return t.trim();
  };

  function paint() {
    clear(root);
    const label = since.toLocaleDateString(undefined, { weekday: 'long', day: 'numeric', month: 'short' });
    const title = mode === 'mine' ? 'Standup' : mode === 'team' ? 'Team standup' : 'Team standup by person';
    root.append(h('div.sthead', h('h2', title), h('span.dim', 'since ' + label), data && data.Head && h('span.dim', data.Head), h('span.spacer'),
      h('button.btn.ghost', { onclick: () => step(-1), title: '[' }, '‹ earlier'), h('button.btn.ghost', { onclick: () => step(1), title: ']' }, 'later ›'),
      h('button.btn', { onclick: () => setMode(team() ? 'mine' : 'team'), title: 'Tab' }, team() ? 'Mine' : 'Team'),
      team() && h('button.btn', { onclick: () => setMode(mode === 'team' ? 'person' : 'team'), title: 'p' }, mode === 'team' ? 'By person' : 'Walk the board'),
      h('button.btn', { onclick: () => { single = !single; paint(); }, title: 'space' }, single ? 'The list' : 'One by one'),
      h('button.btn', { onclick: copy, title: 'y' }, 'Copy')));
    if (err) return root.append(h('div.empty', err));
    if (!data) return root.append(h('div.loading', 'Loading…'));
    if (!lines.length) return root.append(h('div.empty', 'Nothing on record since ' + label));
    sel = Math.max(0, Math.min(sel, lines.length - 1));
    if (!picks(lines[sel])) stepSel(1);
    if (single) return paintSingle();
    let sec = null;
    lines.forEach((l, i) => {
      if (l.Head) {
        sec = h('section.stsec', h('h3', l.Head, l.Unfold && h('button.btn.ghost.sm', { onclick: () => { sel = i; unfold(); } }, ' show (z)')));
        if (l.Unfold) sec.dataset.i = i;
        return root.append(sec);
      }
      (sec || root).append(rowEl(l, i));
    });
    const r = root.querySelector('.strow.sel'); if (r) r.scrollIntoView({ block: 'nearest' });
  }

  function rowEl(l, i) {
    return h('div.strow' + (i === sel ? '.sel' : '') + (l.parked ? '.parked' : ''), { dataset: l.Key ? { key: l.Key, i } : { i } },
      h('span.wkey.mono', l.Key), h('div.main', h('span.wsum', l.Key ? l.Title.slice(l.Key.length + 1) : l.Title),
        h('span.what' + (l.What === 'no activity' ? '.quiet' : ''), l.What || '')),
      h('span.stwho' + (/stale/.test(l.Age || '') ? '.stale' : ''), cells(l).join(' · ')));
  }

  function paintSingle() {
    const l = lines[sel];
    let section = '', n = 0, at = 0;
    lines.forEach((x, i) => { if (x.Head && i < sel) section = x.Head; if (x.Key) { n++; if (i === sel) at = n; } });
    root.append(h('div.stcard',
      h('div.dim', section),
      l.Unfold ? h('h2', 'Off the board') : [h('h2', h('span.mono', l.Key), ' ', l.Key ? l.Title.slice(l.Key.length + 1) : l.Title),
        h('div.dim', cells(l).join(' · ')), h('p.what' + (l.What === 'no activity' ? '.quiet' : ''), l.What || '')],
      h('div.dim', 'card ' + at + ' of ' + n + ' · j k next / previous · space the list · P park')));
    if (l.Key && app.panel.key) app.panel.open(l.Key);
  }

  function stepSel(d) {
    for (let i = sel + d; i >= 0 && i < lines.length; i += d) if (picks(lines[i])) { sel = i; return true; }
    if (!picks(lines[sel]) && d > 0) return stepSel(-1);
    return false;
  }
  function move(d) { if (stepSel(d)) { paint(); if (!single && app.panel.key && lines[sel].Key) app.panel.open(lines[sel].Key); } }

  function unfold() {
    const i = lines.findIndex(l => l.Unfold);
    if (i < 0) return ui.toast('Nothing folded');
    lines[i] = { ...lines[i], Unfold: false };
    lines.splice(i + 1, 0, ...folded);
    folded = []; data.Folded = [];
    sel = i; stepSel(1); paint();
  }

  function park() {
    const l = lines[sel];
    if (!l || !l.Key) return ui.toast('No card to park');
    const i = parkedKeys.indexOf(l.Key);
    if (i >= 0) parkedKeys.splice(i, 1); else parkedKeys.push(l.Key);
    prefs.set(parkId(), parkedKeys.join(' '));
    const was = l.Key;
    build();
    sel = Math.max(0, lines.findIndex(x => x.Key === was && !x.parked));
    ui.toast(was + (i >= 0 ? ' out of the parking lot' : ' parked for after the standup'));
    paint();
  }

  async function load() {
    const my = ++seq;
    data = null; err = ''; paint();
    try {
      await resolveBoard();
      if (sw && !dead) sw.label(project, board);
      if (team() && !board) throw new Error('No board found for ' + (project || 'this site') + '. B picks a project.');
      loadParked();
      const q = 'since=' + ymd(since) + '&mode=' + mode + (board ? '&board=' + board.ID + '&sprint=' + sprint : '');
      const d = await api.get('/standup/lines?' + q, { fresh: true });
      if (dead || my !== seq) return;
      data = d; build(); sel = 0; if (lines.length && !picks(lines[0])) stepSel(1);
    } catch (e) { if (dead || my !== seq) return; err = e.message; }
    paint();
  }

  function setMode(m) { mode = m; sel = 0; load(); }
  function step(n) {
    if (n < 0) since = prevWorkday(since);
    else {
      let x = addDays(since, 1);
      while (!wd.includes(x.getDay())) x = addDays(x, 1);
      if (x < midnight) since = x; else return ui.toast('The previous workday is the latest to start from');
    }
    sel = 0; load();
  }
  function copy() {
    if (!data) return;
    navigator.clipboard.writeText(copyText()).then(() => ui.toast('Copied the standup'), e => ui.errToast(e));
  }
  const open = () => { const l = lines[sel]; if (!l) return; if (l.Unfold) unfold(); else if (l.Key) app.panel.open(l.Key); };

  delegate(root, 'click', '.strow', (e, row) => { sel = +row.dataset.i; paint(); if (row.dataset.key) app.panel.open(row.dataset.key); });

  const G = { group: 'Standup' };
  scope.bind(['j', 'ArrowDown'], () => move(1), 'next', G);
  scope.bind(['k', 'ArrowUp'], () => move(-1), 'previous', G);
  scope.bind('Enter', open, 'open issue (or show Off the board)', G);
  scope.bind('z', unfold, 'show Off the board', G);
  scope.bind('Space', () => { single = !single; paint(); }, 'one card at a time / the list', G);
  scope.bind('P', park, 'park the card for after the standup', G);
  scope.bind('[', () => step(-1), 'a workday further back', G);
  scope.bind(']', () => step(1), 'a workday forward', G);
  scope.bind('Tab', () => setMode(team() ? 'mine' : 'team'), 'mine / team', { ...G, when: () => !app.panel.key });
  scope.bind('p', () => setMode(!team() ? 'team' : mode === 'team' ? 'person' : 'team'), 'team: by person / walk the board', G);
  sw = switcher(app, { scope, context, project, board: null, scrum: false, group: 'Standup', onPick: r => { project = r.project; board = r.board; sprint = 0; load(); } });
  scope.bind('y', copy, 'copy as text, parking lot included', G);
  scope.bind('r', load, 'refresh', G);

  clear(toolbar);
  load();
  return () => { dead = true; };
}
