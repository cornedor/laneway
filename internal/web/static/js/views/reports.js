// Reports: sprint charts (burndown, burnup, flow), velocity, cycle time, retro, releases.
import { h, clear } from '../lib/dom.js';
import { css } from '../lib/css.js';
import { chart, niceTicks, timeTicks, localDay, num, percentile } from '../lib/charts.js';
import { isZero, shortDate } from '../lib/fmt.js';
import { resolve, remember, pickScope, noBoard } from './plan_ctx.js';
import { KINDS } from './report_kinds.js';

const SPRINT_KINDS = ['burndown', 'burnup', 'cfd'];
const DAY = 86400000;
const ms = t => (isZero(t) ? null : +new Date(t));
const nextDay = t => { const d = new Date(t); return new Date(d.getFullYear(), d.getMonth(), d.getDate() + 1).getTime(); };
const dayName = t => new Date(t).toLocaleDateString(undefined, { weekday: 'short', day: 'numeric', month: 'short' });
const days = v => num(v / (DAY * 1e6)); // Go durations arrive as nanoseconds
const toDays = ns => ns / (DAY * 1e6);

// The sprint's issues as the charts count them: points, or one each when none is pointed.
function burnRows(issues) {
  const pointed = issues.some(i => i.Points > 0);
  return { unit: pointed ? 'p' : ' issues', rows: issues.map(i => ({ p: pointed ? i.Points : 1, added: ms(i.Added), res: ms(i.Resolved), i })) };
}
// Sample times: the start, then each day's end up to now (or the sprint's end).
function sampleTimes(start, end, now) {
  const out = [start], last = Math.min(end, now);
  for (let d = localDay(start); d <= last; d = nextDay(d)) out.push(Math.min(nextDay(d), now, end + DAY));
  return out.filter((t, i, a) => i === 0 || t > a[i - 1]);
}

export default async function mount(el, { app, params, query, scope, context, toolbar }) {
  css('reports');
  let kind = KINDS.some(k => k[0] === params.kind) ? params.kind : 'burndown';
  const sc = await resolve(app, params);
  let { project, board } = sc;
  let token = 0, charts = [], cur = 0, curItems = [], weeks = 8, sprintId = Number(query.sprint) || 0;
  const body = h('div.rp');
  el.append(body);
  const cleanupCharts = () => { charts.forEach(c => c.destroy()); charts = []; };
  const open = key => app.panel.open(key);

  const tabs = h('div.rp-tabs', { role: 'tablist' }, KINDS.map(([id, label], i) =>
    h('button', { role: 'tab', dataset: { kind: id }, title: label + ' (' + (i + 1) + ')', onclick: () => go(id) }, label)));
  const sprintBtn = app.chrome.crumb('Sprint (s)', () => pickSprint());
  sprintBtn.textContent = 'Sprint';
  const boardBtn = app.chrome.crumb('Board (b)', () => pickBoard());
  context.append(boardBtn, sprintBtn);
  toolbar.append(tabs);

  function go(k) {
    kind = k;
    history.replaceState(null, '', '#/reports/' + k + (project ? '/' + project + (board ? '/' + board.ID : '') : '') + (sprintId ? '?sprint=' + sprintId : ''));
    document.title = KINDS.find(x => x[0] === k)[1] + ' · laneway';
    load();
  }
  const step = d => { const i = KINDS.findIndex(k => k[0] === kind); go(KINDS[(i + d + KINDS.length) % KINDS.length][0]); };

  async function pickBoard() {
    const r = await pickScope(app, { scrum: kind !== 'cycle' && kind !== 'releases' }); if (!r) return;
    project = r.project; board = r.board; sprintId = 0;
    remember(app, project, board); go(kind);
  }
  let lastSprints = [];
  async function pickSprint() {
    if (!lastSprints.length) return;
    const s = await app.ui.pick({ title: 'Sprint', items: lastSprints, label: x => x.Name, detail: x => x.State, selected: [] });
    if (s) { sprintId = s.ID; go(kind); }
  }

  async function load(fresh) {
    const my = ++token;
    cleanupCharts(); cur = 0;
    tabs.querySelectorAll('button').forEach(b => { const on = b.dataset.kind === kind; b.classList.toggle('on', on); b.setAttribute('aria-selected', on); });
    const sprintKind = SPRINT_KINDS.includes(kind);
    sprintBtn.hidden = !sprintKind;
    app.chrome.label(boardBtn, project || '—', board && board.Name);
    const needsBoard = kind !== 'cycle' && kind !== 'releases';
    if (needsBoard && !board) { clear(body).append(noBoard('Charts', project)); return; }
    if (!project) { clear(body).append(noBoard('Reports', '')); return; }
    clear(body).append(h('div.loading', 'Loading…'));
    try {
      const view = await render(kind, fresh);
      if (my !== token) return;
      clear(body).append(view);
    } catch (e) {
      if (my !== token) return;
      clear(body).append(h('div.empty', h('h2', 'Could not load'), h('p', e.message), h('button.btn', { onclick: () => load(true) }, 'Retry')));
    }
  }
  const get = (path, fresh) => app.api.get(path, { fresh });

  async function render(k, fresh) {
    const b = board && board.ID;
    switch (k) {
      case 'burndown': case 'burnup': case 'cfd': {
        const d = await get('/reports/sprint/' + b + (sprintId ? '?sprint=' + sprintId : ''), fresh);
        lastSprints = (d.Sprints || []).slice().sort((x, y) => (x.State === 'active' ? -1 : y.State === 'active' ? 1 : 0) || (+new Date(y.Start) - +new Date(x.Start)));
        if (!d.Sprint) return h('div.empty', h('h2', 'No sprint'), h('p', 'This board has no active or closed sprint yet.'));
        sprintBtn.textContent = d.Sprint.Name;
        return k === 'burndown' ? burndown(d) : k === 'burnup' ? burnup(d) : flow(d);
      }
      case 'velocity': return velocity(await get('/reports/velocity/' + b, fresh));
      case 'cycle': return cycleView(await get('/reports/cycle/' + encodeURIComponent(project) + '?weeks=' + weeks, fresh));
      case 'retro': return retro(await get('/reports/retro/' + b + '?n=2', fresh));
      case 'releases': return releases(await get('/reports/versions/' + encodeURIComponent(project), fresh));
    }
  }

  const card = (title, ...kids) => h('section.rp-card', title && h('h3', title), ...kids);
  const stat = (v, label, cls = '') => h('div.rp-stat' + (cls ? '.' + cls : ''), h('b', v), h('span', label));
  const head = (title, sub) => h('div.rp-head', h('h2', title), h('span.rp-sub', sub));
  const noDates = sp => h('div.empty', h('h2', sp.Name), h('p', 'This sprint has no dates, so there is nothing to plot.'));

  function sprintHead(sp) {
    const s = ms(sp.Start), e = ms(sp.End);
    return head(sp.Name, (s && e ? shortDate(sp.Start) + ' – ' + shortDate(sp.End) : '') + (sp.State === 'closed' ? ' · closed' : ''));
  }

  function burndown({ Sprint: sp, Issues: issues = [] }) {
    const start = ms(sp.Start), end = ms(sp.End), now = Date.now();
    if (!start || !end) return noDates(sp);
    const { unit, rows } = burnRows(issues);
    const total = rows.reduce((a, r) => a + r.p, 0);
    if (total === 0) return card('', sprintHead(sp), h('p.dim', 'No issues in this sprint.'));
    const left = t => rows.reduce((a, r) => a + ((r.added == null || r.added < t) && !(r.res != null && r.res < t) ? r.p : 0), 0);
    const added = rows.reduce((a, r) => a + (r.added != null && r.added > start ? r.p : 0), 0);
    const span = Math.max(end - start, DAY), ideal = t => total * (1 - Math.min(Math.max((t - start) / span, 0), 1));
    const ts = sampleTimes(start, end, now);
    const pts = ts.map((t, i) => [t, i === 0 ? left(start) : left(t)]);
    const curLeft = pts[pts.length - 1][1], idealNow = ideal(Math.min(now, end)), d = curLeft - idealNow;
    const pace = total === 0 ? '' : d >= 0.5 ? num(d) + unit + ' behind' : d <= -0.5 ? num(-d) + unit + ' ahead' : 'on track';
    const max = niceTicks(Math.max(total, ...pts.map(p => p[1]), 1));
    const wrap = h('div');
    const spec = {
      title: 'Burndown of ' + sp.Name, desc: `${num(curLeft)} of ${num(total)}${unit} left, ideal ${num(idealNow)}. ${pace}`,
      x: { min: start, max: Math.max(end, now), ticks: timeTicks(start, Math.max(end, now), 7) }, y: { max: max.max, ticks: max.ticks },
      layers: [
        { type: 'line', pts: [[start, total], [end, 0]], color: 'var(--fg-3)', dash: true, width: 1.5 },
        now > start && now < end && { type: 'vline', x: now, color: 'var(--warn)', label: 'today' },
        { type: 'line', pts, color: 'var(--accent)', area: true, step: true, width: 2.5 },
        { type: 'dots', pts: pts.slice(1), color: 'var(--accent)', r: 3, opacity: 1 },
      ].filter(Boolean),
      targets: pts.map(([t, v]) => ({ x: t, y: v, head: dayName(t), rows: [
        { color: 'var(--accent)', label: 'Left', value: num(v) + unit, y: v }, { color: 'var(--fg-3)', label: 'Ideal', value: num(ideal(t)) + unit, y: ideal(t) }] })),
      legend: [{ name: 'Left', color: 'var(--accent)' }, { name: 'Ideal', color: 'var(--fg-3)', dash: true }],
    };
    const view = h('div.rp-in', sprintHead(sp),
      h('div.rp-stats', stat(num(curLeft) + unit, 'left'), stat(num(total) + unit, 'in sprint'), stat(num(idealNow) + unit, 'ideal today'),
        pace && stat(pace, 'pace', d >= 0.5 ? 'bad' : d <= -0.5 ? 'ok' : ''), added > 0 && stat('+' + num(added) + unit, 'added after start', 'warn')),
      card('', wrap), h('p.rp-note', 'Counted by resolution date. Left/right arrows step through the days.'));
    charts.push(chart(wrap, spec));
    return view;
  }

  function burnup({ Sprint: sp, Issues: issues = [] }) {
    const start = ms(sp.Start), end = ms(sp.End), now = Date.now();
    if (!start || !end) return noDates(sp);
    const { unit, rows } = burnRows(issues);
    const at = t => {
      let s = 0, dn = 0;
      for (const r of rows) { if (r.added != null && r.added >= t) continue; s += r.p; if (r.res != null && r.res < t) dn += r.p; }
      return [s, dn];
    };
    const ts = sampleTimes(start, end, now), data = ts.map(t => [t, ...at(t)]);
    const top = niceTicks(Math.max(...data.map(d => d[1]), 1)), [, s, dn] = data[data.length - 1];
    const wrap = h('div');
    charts.push(chart(wrap, {
      title: 'Burnup of ' + sp.Name, desc: `${num(dn)} of ${num(s)}${unit} done`,
      x: { min: start, max: Math.max(end, now), ticks: timeTicks(start, Math.max(end, now), 7) }, y: { max: top.max, ticks: top.ticks },
      layers: [
        now > start && now < end && { type: 'vline', x: now, color: 'var(--warn)', label: 'today' },
        { type: 'line', pts: data.map(d => [d[0], d[1]]), color: 'var(--fg-3)', dash: true, step: true },
        { type: 'line', pts: data.map(d => [d[0], d[2]]), color: 'var(--ok)', area: true, step: true, width: 2.5 },
      ].filter(Boolean),
      targets: data.map(d => ({ x: d[0], y: d[2], head: dayName(d[0]), rows: [{ color: 'var(--ok)', label: 'Done', value: num(d[2]) + unit, y: d[2] }, { color: 'var(--fg-3)', label: 'Scope', value: num(d[1]) + unit, y: d[1] }] })),
      legend: [{ name: 'Done', color: 'var(--ok)' }, { name: 'Scope', color: 'var(--fg-3)', dash: true }],
    }));
    return h('div.rp-in', sprintHead(sp), h('div.rp-stats', stat(num(dn) + unit, 'done'), stat(num(s) + unit, 'scope')), card('', wrap));
  }

  const FLOW = ['var(--cat-new)', 'var(--info)', 'var(--warn)', 'var(--cat-indeterminate)', 'var(--accent)'];
  function flow({ Sprint: sp, Issues: issues = [], Columns: cols = [] }) {
    const start = ms(sp.Start), end = ms(sp.End), now = Date.now();
    if (!start || !end) return noDates(sp);
    const colOf = {};
    cols.forEach((c, i) => (c.StatusIDs || []).forEach(id => { colOf[id] = i; }));
    const statusAt = (is, t) => {
      const mv = is.Moves || [];
      for (let i = mv.length - 1; i >= 0; i--) if (+new Date(mv[i].When) <= t) return mv[i].To;
      return mv.length ? mv[0].From : is.Status;
    };
    const color = i => (i === cols.length - 1 ? 'var(--cat-done)' : i === 0 ? FLOW[0] : FLOW[1 + ((i - 1) % 3)]);
    const data = sampleTimes(start, end, now).map(t => {
      const counts = cols.map(() => 0);
      for (const is of issues) {
        const a = ms(is.Added); if (a != null && a >= t) continue;
        const c = colOf[statusAt(is, t - 1000)]; if (c != null) counts[c]++;
      }
      return [t, counts];
    });
    const top = niceTicks(Math.max(...data.map(d => d[1].reduce((a, b) => a + b, 0)), 1));
    const layers = [];
    let lows = data.map(() => 0);
    for (let k = cols.length - 1; k >= 0; k--) {
      const his = data.map((d, i) => lows[i] + d[1][k]);
      layers.push({ type: 'area', pts: data.map((d, i) => [d[0], lows[i], his[i]]), color: color(k), opacity: 0.85 });
      lows = his;
    }
    const wrap = h('div');
    charts.push(chart(wrap, {
      title: 'Cumulative flow of ' + sp.Name, desc: 'Issues per board column, day by day.',
      x: { min: start, max: Math.max(end, now), ticks: timeTicks(start, Math.max(end, now), 7) }, y: { max: top.max, ticks: top.ticks },
      layers,
      targets: data.map(d => ({ x: d[0], head: dayName(d[0]), rows: cols.map((c, k) => ({ color: color(k), label: c.Name, value: String(d[1][k]) })).reverse() })),
      legend: cols.map((c, k) => ({ name: c.Name, color: color(k) })).reverse(),
    }));
    return h('div.rp-in', sprintHead(sp), card('Issues per column', wrap));
  }

  function velocity(vel) {
    if (!vel.length) return h('div.empty', h('h2', 'Velocity'), h('p', 'No closed sprints yet.'));
    const avg = vel.reduce((a, v) => a + v.Done, 0) / vel.length;
    const top = niceTicks(Math.max(...vel.map(v => Math.max(v.Done, v.Committed)), 1));
    const abbr = n => { const m = n.match(/(\d+)\D*$/); return m ? '#' + m[1] : n.slice(0, 8); };
    const every = Math.ceil(vel.length / Math.max(Math.floor(((el.clientWidth || 800) - 100) / 48), 1));
    const items = [];
    vel.forEach((v, i) => {
      items.push({ x: i - 0.21, y: v.Committed, color: 'var(--cat-new)', opacity: 0.5 }, { x: i + 0.21, y: v.Done, color: 'var(--ok)' });
    });
    const wrap = h('div');
    charts.push(chart(wrap, {
      title: 'Velocity', desc: `Committed and completed points of the last ${vel.length} sprints, average ${num(avg)} completed.`,
      x: { min: -0.6, max: vel.length - 0.4, ticks: vel.map((v, i) => ({ v: i, label: abbr(v.Name) })).filter((_, i) => i % every === 0) }, y: { max: top.max, ticks: top.ticks },
      layers: [{ type: 'bars', items, bw: 0.4 }, { type: 'hline', y: avg, color: 'var(--accent)', label: 'avg ' + num(avg) + 'p' }],
      targets: vel.map((v, i) => ({ x: i, head: v.Name, rows: [{ color: 'var(--ok)', label: 'Completed', value: num(v.Done) + 'p' }, { color: 'var(--cat-new)', label: 'Committed', value: num(v.Committed) + 'p' }, { label: 'Closed', value: shortDate(v.End) }] })),
      legend: [{ name: 'Completed', color: 'var(--ok)' }, { name: 'Committed', color: 'var(--cat-new)' }, { name: 'Average', color: 'var(--accent)', dash: true }],
    }));
    const last = vel[vel.length - 1];
    return h('div.rp-in', head('Velocity', 'last ' + vel.length + ' sprints'),
      h('div.rp-stats', stat(num(avg) + 'p', 'average completed'), stat(num(last.Done) + 'p', 'last sprint: ' + last.Name),
        stat(Math.round(vel.reduce((a, v) => a + v.Done, 0) / Math.max(vel.reduce((a, v) => a + v.Committed, 0), 1) * 100) + '%', 'of committed, completed')),
      card('', wrap));
  }

  function cycleView(list) {
    const now = Date.now(), from = now - weeks * 7 * DAY;
    const cyc = list.filter(i => i.Cycle > 0), cycD = cyc.map(i => toDays(i.Cycle)), leadD = list.map(i => toDays(i.Lead));
    const weeksBtn = h('button.btn', { title: 'Weeks (W)', onclick: nextWeeks }, weeks + ' weeks');
    if (!list.length) return h('div.rp-in', head('Cycle time', ''), h('div.empty', 'Nothing resolved in the last ' + weeks + ' weeks.'), weeksBtn);
    const p50 = percentile(cycD, 50), p85 = percentile(cycD, 85);
    const top = niceTicks(Math.max(...cycD, p85 * 1.2, 1));
    const wrap = h('div');
    charts.push(chart(wrap, {
      title: 'Cycle time', desc: `${cyc.length} resolved issues; half within ${num(p50)} days, 85% within ${num(p85)}.`, nearest: 'xy',
      x: { min: from, max: now, ticks: timeTicks(from, now, 7) }, y: { max: top.max, ticks: top.ticks, fmt: v => num(v) + 'd' },
      layers: [
        { type: 'hline', y: p50, color: 'var(--ok)', label: '50% ' + num(p50) + 'd' }, { type: 'hline', y: p85, color: 'var(--warn)', label: '85% ' + num(p85) + 'd' },
        { type: 'dots', pts: cyc.map(i => [+new Date(i.Resolved), toDays(i.Cycle)]), color: 'var(--accent)', r: 4 },
      ],
      targets: cyc.map(i => ({ x: +new Date(i.Resolved), y: toDays(i.Cycle), head: i.Key + ' ' + i.Summary.slice(0, 50), onclick: () => open(i.Key),
        rows: [{ label: 'Cycle', value: days(i.Cycle) + ' days', color: 'var(--accent)', y: toDays(i.Cycle) }, { label: 'Lead', value: days(i.Lead) + ' days' }, { label: 'Done', value: dayName(+new Date(i.Resolved)) }] })),
    }));
    const slow = cyc.slice().sort((a, b) => b.Cycle - a.Cycle).slice(0, 8);
    curItems = slow.map(i => i.Key);
    return h('div.rp-in', head('Cycle time', 'in progress to done'),
      h('div.rp-stats', stat(num(p50) + 'd', 'cycle, 50%'), stat(num(p85) + 'd', 'cycle, 85%'), stat(num(percentile(leadD, 50)) + 'd', 'lead, 50%'), stat(num(percentile(leadD, 85)) + 'd', 'lead, 85%'), stat(list.length, 'resolved'), weeksBtn),
      card('', wrap),
      card('Slowest', h('ul.rp-list', slow.map((i, n) => h('li', { class: n === 0 ? 'cur' : '', dataset: { key: i.Key }, onclick: () => open(i.Key) }, h('span.key', i.Key), h('span.sum', i.Summary), h('span.dim', days(i.Cycle) + ' days'))))),
      h('p.rp-note', 'Lead time runs from created to done; cycle time from first in progress.'));
  }
  function nextWeeks() { const w = [4, 8, 12, 26]; weeks = w[(w.indexOf(weeks) + 1) % w.length]; load(); }

  function retro({ Sprints: rs = [], Cards: cards = {} }) {
    if (!rs.length) return h('div.empty', h('h2', 'Retro'), h('p', 'No closed sprints yet.'));
    const last = rs[rs.length - 1], n = k => (k || []).length;
    const metric = [['Committed', r => n(r.Committed)], ['Added during', r => n(r.Added)], ['Done', r => n(r.Done)], ['Carried over', r => n(r.Carried)], ['Moved backwards', r => n(r.Back)], ['Points done', r => num(r.DonePoints) + ' of ' + num(r.Points)]];
    const list = (title, keys, cls) => keys && keys.length ? card(title + ' (' + keys.length + ')', h('ul.rp-list', keys.map(k => {
      const c = cards[k] || {};
      return h('li', { dataset: { key: k }, onclick: () => open(k) }, h('span.key', k), h('span.sum', c.Summary || ''), c.Status && h('span.dim', c.Status));
    }))) : null;
    curItems = [...(last.Carried || []), ...(last.Done || [])];
    return h('div.rp-in', head('Retro: ' + last.Name, shortDate(last.Start) + ' – ' + shortDate(last.End)),
      card('', h('table', h('thead', h('tr', h('th', ''), rs.map(r => h('th.n', r.Name)))),
        h('tbody', metric.map(([label, f]) => h('tr', h('td', label), rs.map(r => h('td.n', f(r)))))))),
      h('div.rp-grid', list('Shipped', last.Done), list('Slipped', last.Carried), list('Added during the sprint', last.Added), list('Moved backwards', last.Back)));
  }

  function releases(vs) {
    const list = vs.filter(v => !v.Archived);
    if (!list.length) return h('div.empty', h('h2', 'Releases'), h('p', project + ' has no versions.'));
    const open_ = list.filter(v => !v.Released), shipped = list.filter(v => v.Released);
    curItems = [...open_, ...shipped];
    const row = v => {
      const pct = v.Total ? Math.round(v.Done / v.Total * 100) : 0;
      const today = new Date().toISOString().slice(0, 10), late = !v.Released && v.ReleaseDate && v.ReleaseDate < today;
      return h('div.rp-ver' + (v.Released ? '.released' : ''), { role: 'listitem', dataset: { id: v.ID }, tabindex: -1, onclick: () => { cur = curItems.indexOf(v); mark(); } },
        h('span.name', v.Name), h('span' + (late ? '.dim' : '.faint'), { style: late ? { color: 'var(--err)' } : null }, v.ReleaseDate || (v.Released ? 'released' : 'no date')),
        h('div.rp-bar', { role: 'progressbar', 'aria-valuenow': pct, 'aria-valuemin': 0, 'aria-valuemax': 100, 'aria-label': v.Name + ' ' + pct + '% done' }, h('i', { style: { width: pct + '%' } })),
        h('span.cnt', v.Done + '/' + v.Total,
          !v.Released && h('button.btn.link', { title: 'Release (r)', onclick: e => { e.stopPropagation(); release(v); } }, 'release')));
    };
    const view = h('div.rp-in', head('Releases', project),
      card('Unreleased', h('div', { role: 'list' }, open_.length ? open_.map(row) : h('p.dim', 'Everything is released.'))),
      shipped.length ? card('Released', h('div', { role: 'list' }, shipped.slice(0, 15).map(row))) : null);
    queueMicrotask(mark);
    return view;
  }
  function mark() {
    body.querySelectorAll('.rp-ver, .rp-list li').forEach(n => n.classList.remove('cur'));
    const k = curItems[cur];
    const n = k && (typeof k === 'string' ? body.querySelector(`.rp-list li[data-key="${k}"]`) : body.querySelector(`.rp-ver[data-id="${k.ID}"]`));
    if (n) { n.classList.add('cur'); n.scrollIntoView({ block: 'nearest' }); }
  }
  async function release(v) {
    if (!v || v.Released) return;
    if (!await app.ui.confirm({ title: 'Release ' + v.Name, text: v.Total - v.Done ? `${v.Total - v.Done} issues are not done yet. Release it today anyway?` : 'Release it today?', ok: 'Release' })) return;
    try { await app.api.post('/reports/versions/' + encodeURIComponent(v.ID) + '/release', {}); app.ui.toast(v.Name + ' released', { kind: 'ok' }); load(true); } catch (e) { app.ui.errToast(e); }
  }

  // ---- keys
  scope.bind(['l', ']', 'ArrowRight'], () => step(1), 'next report', { group: 'Reports' });
  scope.bind(['h', '[', 'ArrowLeft'], () => step(-1), 'previous report', { group: 'Reports' });
  KINDS.forEach(([id], i) => scope.bind(String(i + 1), () => go(id), 'report ' + (i + 1), { hidden: true }));
  scope.bind('s', () => { if (SPRINT_KINDS.includes(kind)) pickSprint(); }, 'pick sprint', { group: 'Reports' });
  scope.bind('b', () => pickBoard(), 'pick board', { group: 'Reports' });
  scope.bind('W', () => { if (kind === 'cycle') nextWeeks(); }, 'cycle time: weeks', { group: 'Reports' });
  scope.bind('R', () => load(true), 'reload', { group: 'Reports' });
  const move = d => { if (curItems.length) { cur = Math.min(Math.max(cur + d, 0), curItems.length - 1); mark(); } };
  scope.bind(['j', 'ArrowDown'], () => move(1), 'next item', { group: 'Reports' });
  scope.bind(['k', 'ArrowUp'], () => move(-1), 'previous item', { group: 'Reports' });
  scope.bind('Enter', () => { const k = curItems[cur]; if (typeof k === 'string') open(k); }, 'open issue', { group: 'Reports' });
  scope.bind('r', () => { if (kind === 'releases') release(curItems[cur]); }, 'release the version', { group: 'Reports' });

  remember(app, project, board);
  await load();
  return () => { token++; cleanupCharts(); };
}
