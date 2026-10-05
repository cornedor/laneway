// Standup: a strip that goes round the people (← →), Everyone first with the board walked right to left, then each
// person's cards and what they did, those heard ticked; a timer beside it, each turn counting down (ui.standup_length
// split, or ui.standup_timebox) and the whole standup's time; below, the stop's rows, over the view the board showed last.
// A picks who takes part, kept per board. Park a card for after (P, kept per view): Everyone's parking lot comes last. Stops come from /standup/lines, built like the TUI's.
import { h, clear, delegate } from '../lib/dom.js';
import { icon } from '../lib/icons.js';
import { css } from '../lib/css.js';
import { avatar } from '../lib/ui.js';
import { projectOf, boardsOf, lastBoard, setCtx, switcher, recover } from './plan_ctx.js';
import { ymd, addDays, workdays } from '../lib/worktime.js';

const PARK = 'Parking lot';

export default function mount(el, { app, scope, context, toolbar }) {
  css('work'); css('standup');
  const { api, ui, prefs } = app;
  const wd = workdays(app);
  const prevWorkday = d => { let x = addDays(d, -1); while (!wd.includes(x.getDay())) x = addDays(x, -1); return x; };
  const midnight = new Date(); midnight.setHours(0, 0, 0, 0);
  let since = null, data = null, err = '', dead = false, seq = 0, opened = false, loading = false;
  let project = projectOf(app, {});
  let sw = null, board = null, sprint = 0, lines = [], folded = [], sel = 0, parkedKeys = [];
  // The round: the stops in the order gone round, the one shown, who was heard; the timer (ms, 0 unset).
  let stops = [], at = 0, shuffled = false, started = 0, turn = 0, paused = 0;
  const heard = new Set();

  const root = h('div.standup');
  el.append(root);

  const picks = l => !!(l.Key || l.Unfold);
  const person = () => (stops[at] && stops[at].Person) || {};
  // The board view the walk takes: the one the board showed last, else the active sprint; parked per view.
  let view = null;
  const parkId = () => 'standup_park.' + (board ? board.ID : 0) + '.' + (view ? view.park : sprint);
  const loadParked = () => { parkedKeys = (prefs.get(parkId(), '') || '').split(/\s+/).filter(Boolean); };
  // Who takes part, by account, kept per board; none: everyone.
  let inPeople = [];
  const peopleId = () => 'standup_people.' + (board ? board.ID : 0);
  const loadPeople = () => { inPeople = (prefs.get(peopleId(), '') || '').split(/\s+/).filter(Boolean); };
  // turnMs is each person's turn: the timebox, else the length split over who takes part.
  const turnMs = () => { const s = data.Settings, n = stops.length - 1; return 1000 * (s.Timebox || (n ? Math.floor(s.Length / n) : 0)); };

  // The project's board and its active sprint, for the walk and the parking lot.
  async function resolveBoard() {
    if (board || !project) return;
    try {
      const bs = await boardsOf(app, project), last = lastBoard(app, project);
      board = bs.find(b => b.ID === last) || bs.find(b => /scrum/i.test(b.Type)) || bs[0] || null;
      if (board && !last) setCtx(app, project, board);
      if (board && !(app.lastView && app.lastView.board === board.ID)) {
        const b = await api.get('/boards/' + board.ID);
        const a = (b.sprints || []).find(s => s.State === 'active');
        sprint = a ? a.ID : 0;
      }
    } catch (e) {
      // A project this site lacks (remembered on another): forget it, take the default.
      if (e.status === 404 && recover(app, project)) { project = projectOf(app, {}); return resolveBoard(); }
    }
  }

  // order lays the stops out, Everyone first: the board's order, or shuffled (again when reshuffle, else as gone round
  // so far, so a reload keeps it), and stays on who.
  function order(who, reshuffle) {
    const people = (data.Stops || []).slice(1).filter(s => !inPeople.length || inPeople.includes(s.Person.ID)), prev = stops.map(s => s.Person.ID);
    if (shuffled) for (let i = people.length - 1; i > 0; i--) { const j = Math.floor(Math.random() * (i + 1)); [people[i], people[j]] = [people[j], people[i]]; }
    const was = s => { const i = prev.indexOf(s.Person.ID); return i < 0 ? prev.length : i; }; // someone new goes last
    if (shuffled && !reshuffle && prev.length) people.sort((a, b) => was(a) - was(b));
    stops = [...(data.Stops || []).slice(0, 1), ...people];
    at = Math.max(0, stops.findIndex(s => (s.Person.ID || '') === (who || '')));
  }

  // build puts the shown stop's rows up, parked rows marked, and on Everyone's the parking lot last.
  function build() {
    const st = stops[at] || { Rows: [], Folded: [] };
    const mark = l => (l.Key && parkedKeys.includes(l.Key) ? { ...l, Marks: [l.Marks, 'parked'].filter(Boolean).join(' · ') } : l);
    lines = (st.Rows || []).map(mark);
    folded = (st.Folded || []).map(mark);
    if (at !== 0) return;
    const all = [...(st.Rows || []), ...(st.Folded || [])];
    const byKey = new Map(all.filter(l => l.Key).map(l => [l.Key, l]));
    const lot = parkedKeys.map(k => ({ ...(byKey.get(k) || { Key: k, Title: k }), parked: true }));
    if (lot.length) lines.push({ Head: PARK + ' (' + lot.length + ')' }, ...lot);
  }

  const cells = l => [l.Who, l.Age, l.Marks].filter(Boolean);
  const rowText = l => [l.Title, l.Who, l.Age, l.Marks, l.What].filter(Boolean).join(' · ');
  const copyText = () => {
    const st = stops[at];
    if (!st) return '';
    let t = st.Person.ID ? st.Person.Name + '\n\n' + st.Text : (data.Head ? data.Head + '\n\n' : '') + st.Text;
    const i = lines.findIndex(l => l.Head && l.Head.startsWith(PARK));
    if (i >= 0) t += '\n\n' + PARK + '\n' + lines.slice(i + 1).map(l => '- ' + rowText(l)).join('\n');
    return t.trim();
  };

  // ---- the timer

  const now = () => Date.now();
  const clock = ms => { const s = Math.max(0, Math.floor(ms / 1000)); return Math.floor(s / 60) + ':' + String(s % 60).padStart(2, '0'); };
  let tick = 0;
  function run() { clearInterval(tick); tick = setInterval(paintTimer, 1000); }
  function pause() {
    const t = now();
    if (!started) { started = t; run(); ui.toast("The standup's timer runs"); }
    else if (!paused) { paused = t; clearInterval(tick); ui.toast('The timer is paused'); }
    else { const away = t - paused; started += away; if (turn) turn += away; paused = 0; run(); ui.toast('The timer runs again'); }
    paintTimer();
  }
  const timerEl = h('div.sttimer');
  function paintTimer() {
    clear(timerEl);
    const s = data && data.Settings;
    if (!s) return;
    const length = s.Length * 1000;
    if (!started) {
      timerEl.append(h('span.dim', 'space or → starts the timer'), h('button.btn.ghost.sm', { onclick: pause, title: 'space' }, icon('play'), 'Start'));
      return;
    }
    const t = paused || now(), total = t - started;
    if (turn && turnMs()) {
      const left = turnMs() - (t - turn);
      timerEl.append(left < 0
        ? h('span.stleft.over' + (paused ? '' : '.flash'), '+' + clock(-left) + ' over')
        : h('span.stleft', clock(left + 999), h('span.dim', ' left')));
    }
    timerEl.append(...[h('span.sttotal' + (total > length ? '.over' : ''), clock(total) + ' of ' + clock(length)),
      paused && h('span.dim', 'paused'),
      h('button.btn.ghost.sm', { onclick: pause, title: 'space' }, icon(paused ? 'play' : 'pause'), paused ? 'Resume' : 'Pause')].filter(Boolean));
  }

  // go moves to stop i, marking the person left heard; a turn starts on a person's stop, the timer with the first.
  function go(i) {
    if (stops.length < 2) return ui.toast('No one is assigned a card on the board');
    if (person().ID) heard.add(person().ID);
    at = (i + stops.length) % stops.length;
    turn = 0;
    if (at !== 0) {
      turn = paused || now();
      if (!started) { started = turn; run(); }
    }
    build(); sel = 0; if (lines.length && !picks(lines[0])) stepSel(1);
    paint();
  }

  // ---- painting

  function strip() {
    const chips = stops.map((st, i) => {
      const p = st.Person, cur = i === at;
      const cls = 'button.stchip' + (cur ? '.cur' : '') + (p.ID && heard.has(p.ID) ? '.heard' : '') + (st.Quiet ? '.quiet' : '');
      return h(cls, { onclick: () => go(i), title: p.ID ? p.Name + (st.Quiet ? ' · no changes' : '') : 'Everyone: the board walked right to left' },
        p.ID ? avatar(p.Name, p.Avatar, 26) : h('span.stall', 'Everyone'),
        p.ID && cur && h('span.stname', p.Name),
        p.ID && cur && st.Quiet && h('span.dim', 'no changes'),
        p.ID && heard.has(p.ID) && h('span.sttick', '✓'));
    });
    // The arrows stay beside the chips, which wrap between them; the tools and the timer keep to one line each.
    return h('div.ststrip',
      h('div.stround', h('button.btn.ghost.sm', { onclick: () => go(at - 1), title: '←' }, icon('chevron-left')), h('div.stpeople', ...chips),
        h('button.btn.ghost.sm', { onclick: () => go(at + 1), title: '→' }, icon('chevron-right'))),
      h('div.sttools', h('button.btn.ghost.sm', { onclick: pickPeople, title: 'A' }, inPeople.length ? (stops.length - 1) + ' of ' + ((data.Stops || []).length - 1) : "Who's in"),
        h('button.btn.ghost.sm' + (shuffled ? '.on' : ''), { onclick: shuffle, title: 's' }, icon('shuffle'), shuffled ? 'Shuffled' : 'Board order')),
      timerEl);
  }

  function paint() {
    clear(root);
    const label = since ? since.toLocaleDateString(undefined, { weekday: 'long', day: 'numeric', month: 'short' }) : '…';
    root.append(h('div.sthead', h('div.sttitle', h('h2', 'Standup'), view && view.name && h('span.chip', { title: 'The view the board showed last' }, view.name), h('span.dim', 'since ' + label), data && data.Head && h('span.dim', data.Head), loading && data && h('span.dim', 'loading…')),
      h('div.stbtns', h('button.btn.ghost', { onclick: () => step(-1), title: '[' }, icon('chevron-left'), 'earlier'), h('button.btn.ghost', { onclick: () => step(1), title: ']' }, 'later', icon('chevron-right')),
        h('button.btn', { onclick: copy, title: 'y' }, 'Copy'))));
    if (err) return root.append(h('div.empty', err));
    if (!data) return root.append(h('div.loading', 'Loading…'));
    root.append(strip());
    paintTimer();
    if (!lines.length) return root.append(h('div.empty', 'No changes since ' + label));
    sel = Math.max(0, Math.min(sel, lines.length - 1));
    if (!picks(lines[sel])) stepSel(1);
    let sec = null;
    lines.forEach((l, i) => {
      if (l.Head) {
        sec = h('section.stsec', h('h3', l.Head, l.Unfold && h('button.btn.ghost.sm', { onclick: () => { sel = i; unfold(); } }, ' show (z)')));
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

  function stepSel(d) {
    for (let i = sel + d; i >= 0 && i < lines.length; i += d) if (picks(lines[i])) { sel = i; return true; }
    if (!picks(lines[sel]) && d > 0) return stepSel(-1);
    return false;
  }
  function move(d) { if (stepSel(d)) { paint(); if (app.panel.key && lines[sel].Key) app.panel.open(lines[sel].Key); } }

  function unfold() {
    const i = lines.findIndex(l => l.Unfold);
    if (i < 0) return ui.toast('Nothing folded');
    lines[i] = { ...lines[i], Unfold: false };
    lines.splice(i + 1, 0, ...folded);
    folded = [];
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

  async function pickPeople() {
    const all = (data && data.Stops || []).slice(1).map(s => s.Person);
    if (!all.length) return ui.toast('No one is assigned a card on the board');
    const everyone = { ID: '', Name: 'Everyone on the board' };
    const r = await ui.pick({ title: 'Who takes part', items: [everyone, ...all], multi: true, enterPicks: true, selected: all.filter(p => inPeople.includes(p.ID)), label: p => p.Name, placeholder: 'People…' });
    if (!r || dead) return;
    inPeople = r.includes(everyone) ? [] : r.map(p => p.ID);
    prefs.set(peopleId(), inPeople.join(' '));
    order(person().ID, false);
    build(); sel = 0; if (lines.length && !picks(lines[0])) stepSel(1);
    ui.toast(inPeople.length ? (stops.length - 1) + (stops.length === 2 ? ' person takes' : ' people take') + ' part' : 'Everyone on the board takes part');
    paint();
  }

  function shuffle() {
    shuffled = !shuffled;
    if (data) { order(person().ID, true); build(); }
    ui.toast(shuffled ? 'A random order' : "The board's order");
    paint();
  }

  async function load() {
    const my = ++seq;
    err = ''; loading = true; paint();
    try {
      await resolveBoard();
      if (sw && !dead) sw.label(project, board);
      if (!board) throw new Error('No board found for ' + (project || 'this site') + '. alt+p picks a project.');
      view = app.lastView && app.lastView.board === board.ID ? app.lastView : null;
      loadParked(); loadPeople();
      const q = (since ? 'since=' + ymd(since) + '&' : '') + 'board=' + board.ID + '&' + (view ? view.query : 'sprint=' + sprint);
      const d = await api.get('/standup/lines?' + q, { fresh: true });
      if (dead || my !== seq) return;
      const who = person().ID;
      data = d; since = new Date(d.Since + 'T00:00:00');
      if (!opened) shuffled = !!d.Settings.Shuffle;
      order(who, false);
      build(); sel = 0; if (lines.length && !picks(lines[0])) stepSel(1);
      if (!opened && d.Settings.First && stops.length > 1) { opened = true; return go(1); }
      opened = true;
    } catch (e) { if (dead || my !== seq) return; err = e.message; }
    loading = false;
    paint();
  }

  // reset starts the standup over, for another board: its people, unheard, the timer stopped.
  function reset() {
    data = null; stops = []; at = 0; lines = []; folded = []; sel = 0; heard.clear();
    started = turn = paused = 0; clearInterval(tick); opened = false; since = null;
  }

  function step(n) {
    if (!since) return;
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
  scope.bind(['l', 'ArrowRight'], () => go(at + 1), 'next person', { ...G, bar: 'person' });
  scope.bind(['h', 'ArrowLeft'], () => go(at - 1), 'previous person', G);
  scope.bind('Space', pause, 'start / pause the timer', { ...G, bar: 'timer' });
  scope.bind('A', pickPeople, 'who takes part', { ...G, bar: "who's in" });
  scope.bind('s', shuffle, 'a random order / the board\'s', G);
  scope.bind('Enter', open, 'open issue (or show Off the board)', { ...G, bar: 'open' });
  scope.bind('z', unfold, 'show Off the board', G);
  scope.bind('P', park, 'park the card for after the standup', { ...G, bar: 'park' });
  scope.bind('[', () => step(-1), 'a workday further back', { ...G, bar: 'day' });
  scope.bind(']', () => step(1), 'a workday forward', { ...G, bar: 'day' });
  sw = switcher(app, { scope, context, project, board: null, scrum: false, group: 'Standup', onPick: r => { project = r.project; board = r.board; sprint = 0; view = null; reset(); load(); } });
  scope.bind('y', copy, 'copy the stop as text, parking lot included', { ...G, bar: 'copy' });
  scope.bind('r', load, 'refresh', G);

  clear(toolbar);
  load();
  return () => { dead = true; clearInterval(tick); };
}
