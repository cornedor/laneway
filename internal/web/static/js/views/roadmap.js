// Roadmap: the project's epics as bars on a time axis.
import { h, clear } from '../lib/dom.js';
import { css } from '../lib/css.js';
import { remPx, onChange as onMetrics } from '../lib/metrics.js';
import { hwheel } from '../lib/hscroll.js';
import { isZero, shortDate } from '../lib/fmt.js';
import { resolve, switcher, noBoard } from './plan_ctx.js';

const DAY = 86400000;
const ZOOMS = [2, 4, 8, 14, 24, 40, 64]; // px per day
const labelW = () => 20 * remPx(); // .rm-label / .rm-corner are 20rem
const ms = t => (isZero(t) ? null : +new Date(t));
const midnight = t => { const d = new Date(t); return new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime(); };
const addDays = (t, n) => { const d = new Date(t); return new Date(d.getFullYear(), d.getMonth(), d.getDate() + n).getTime(); };

export default async function mount(el, { app, params, scope, context, toolbar }) {
  css('roadmap');
  const sc = await resolve(app, params, { scrum: false });
  let { project } = sc;
  let epics = [], zoom = Number(app.prefs.get('roadmap.zoom', 3)), cur = 0, open = new Set(), rows = [], token = 0;
  if (!(zoom >= 0 && zoom < ZOOMS.length)) zoom = 3;
  let t0 = 0, t1 = 0;
  const scroller = h('div.rm', { tabindex: -1 }); hwheel(scroller);
  el.append(scroller);
  switcher(app, { scope, context, project, boards: false, group: 'Roadmap', onPick: r => { project = r.project; epics = []; history.replaceState(null, '', '#/roadmap/' + project); load(); } });
  toolbar.append(h('span.spacer'),
    h('button.btn', { title: 'Zoom out (-)', 'aria-label': 'Zoom out', onclick: () => setZoom(zoom - 1) }, '−'),
    h('button.btn', { title: 'Zoom in (+)', 'aria-label': 'Zoom in', onclick: () => setZoom(zoom + 1) }, '+'),
    h('button.btn', { title: 'Today (.)', onclick: () => today() }, 'Today'));

  const ppd = () => ZOOMS[zoom] * remPx() / 14; // px per day, scales with the font size
  const xOf = t => (t - t0) / DAY * ppd();

  async function load(fresh) {
    const my = ++token;
    if (!project) { clear(scroller).append(noBoard('Roadmap', '')); return; }
    if (!epics.length) clear(scroller).append(h('div.loading', 'Loading…'));
    try {
      const d = await app.api.get('/roadmap/' + encodeURIComponent(project), { fresh });
      if (my !== token) return;
      epics = d.Epics || [];
      cur = Math.min(cur, Math.max(epics.length - 1, 0));
      draw(true);
    } catch (e) {
      if (my !== token) return;
      clear(scroller).append(h('div.empty', h('h2', 'Could not load'), h('p', e.message), h('button.btn', { onclick: () => load(true) }, 'Retry')));
    }
  }

  const span = it => {
    let s = ms(it.Start), e = ms(it.End);
    if (s == null && e == null) return null;
    if (s == null) s = e - 7 * DAY;
    if (e == null) e = s + 7 * DAY;
    return [midnight(s), midnight(Math.max(e, s)) + DAY];
  };
  const cat = e => (e.Done ? 'done' : e.DoneChildren > 0 || /progress|review/i.test(e.Status) ? 'indeterminate' : 'new');

  function draw(reset) {
    const keepScroll = scroller.scrollLeft, keepTop = scroller.scrollTop;
    const now = Date.now();
    let lo = now, hi = now;
    for (const e of epics) { const s = span(e); if (s) { lo = Math.min(lo, s[0]); hi = Math.max(hi, s[1]); } }
    t0 = addDays(midnight(lo), -21); t1 = addDays(midnight(hi), 28);
    const w = xOf(t1);
    rows = [];
    for (const e of epics) { rows.push({ e }); if (open.has(e.Key)) for (const k of e.Kids || []) rows.push({ e: k, kid: true, parent: e }); }
    cur = Math.min(cur, Math.max(rows.length - 1, 0));

    const inner = h('div.rm-inner', { style: { width: labelW() + w + 'px', '--wk': 7 * ppd() + 'px', '--wko': xOf(mondayOnOrBefore(t0)) + 'px' } });
    inner.append(header(w));
    const list = h('div.rm-rows', { role: 'list' });
    rows.forEach((r, i) => list.append(rowEl(r, i, w)));
    if (!epics.length) list.append(h('div.empty', 'No epics in ' + project + '.'));
    inner.append(list);
    inner.append(h('div.rm-today', { style: { left: labelW() + xOf(midnight(now)) + ppd() / 2 + 'px' }, title: 'Today' }));
    clear(scroller).append(inner);
    if (reset) { scroller.scrollLeft = Math.max(xOf(now) - scroller.clientWidth / 3, 0); keepScrollApply(0, keepTop); } else keepScrollApply(keepScroll, keepTop);
    markCur();
  }
  const keepScrollApply = (l, t) => { scroller.scrollLeft = l; scroller.scrollTop = t; };
  const mondayOnOrBefore = t => { const d = new Date(t); return addDays(t, -((d.getDay() + 6) % 7)); };

  function header(w) {
    const months = h('div.rm-months'), ticks = h('div.rm-ticks');
    for (let d = new Date(t0); d.getTime() < t1;) {
      const from = d.getTime(); d = new Date(d.getFullYear(), d.getMonth() + 1, 1);
      const to = Math.min(d.getTime(), t1);
      months.append(h('span', { style: { left: xOf(from) + 'px', width: xOf(to) - xOf(from) + 'px' } }, new Date(from).toLocaleDateString(undefined, { month: 'long', year: 'numeric' })));
    }
    const dayMode = ZOOMS[zoom] >= 14, step = dayMode ? 1 : 7;
    for (let t = dayMode ? t0 : mondayOnOrBefore(t0); t < t1; t = addDays(t, step)) {
      const d = new Date(t);
      ticks.append(h('span', { class: dayMode && (d.getDay() === 0 || d.getDay() === 6) ? 'wk' : '', style: { left: xOf(t) + 'px', width: step * ppd() + 'px' } }, dayMode ? d.getDate() : ZOOMS[zoom] >= 4 ? d.getDate() : ''));
    }
    return h('div.rm-head', h('div.rm-corner', project), h('div.rm-axis', { style: { width: w + 'px' } }, months, ticks));
  }

  function rowEl(r, i, w) {
    const it = r.e, s = span(it), kid = r.kid, c = kid ? (it.Done ? 'done' : 'new') : cat(it);
    const isOpen = !kid && open.has(it.Key);
    const blocked = !kid && (it.BlockedBy || []).length ? it.BlockedBy : null;
    let bad = false;
    if (blocked && s) for (const k of blocked) { const b = epics.find(x => x.Key === k); const bs = b && span(b); if (bs && bs[1] > s[0]) bad = true; }
    const pct = kid ? (it.Done ? 100 : 0) : it.Children ? Math.round(it.DoneChildren / it.Children * 100) : it.Done ? 100 : 0;
    const label = h('div.rm-label', { onclick: () => select(i) },
      !kid ? h('button.rm-fold', { 'aria-label': isOpen ? 'Fold' : 'Unfold', 'aria-expanded': isOpen, tabindex: -1, onclick: ev => { ev.stopPropagation(); select(i); toggle(); } }, it.Kids && it.Kids.length ? (isOpen ? '▾' : '▸') : '') : h('span.rm-fold'),
      h('span.rm-key', it.Key), h('span.rm-sum', { title: it.Summary }, it.Summary),
      blocked && h('span.rm-block' + (bad ? '.bad' : ''), { title: 'Blocked by ' + blocked.join(', ') }, bad ? '⛔' : '⛓'),
      !kid && it.Children > 0 && h('span.rm-cnt', it.DoneChildren + '/' + it.Children));
    const track = h('div.rm-track', { style: { width: w + 'px' }, onclick: () => select(i) });
    if (s) {
      const when = shortDate(s[0]) + ' – ' + shortDate(s[1] - DAY) + (it.DatesFromSprints ? ' (from sprints)' : '');
      track.append(h('div.rm-bar.c-' + c + (it.DatesFromSprints ? '.soft' : ''), {
        style: { left: xOf(s[0]) + 'px', width: Math.max(xOf(s[1]) - xOf(s[0]), 6) + 'px', '--pct': pct + '%' }, title: `${it.Key} ${it.Summary}\n${when}\n${it.Status}` + (kid ? '' : `, ${pct}% of ${it.Children} issues`),
        onclick: ev => { ev.stopPropagation(); select(i); app.panel.open(it.Key); },
      }, h('span', it.Summary)));
    } else track.append(h('span.rm-nodate', 'no dates'));
    return h('div.rm-row' + (kid ? '.kid' : ''), { role: 'listitem', dataset: { i } }, label, track);
  }

  function select(i) { cur = i; markCur(); }
  function markCur() {
    scroller.querySelectorAll('.rm-row.cur').forEach(n => n.classList.remove('cur'));
    const n = scroller.querySelector(`.rm-row[data-i="${cur}"]`);
    if (n) { n.classList.add('cur'); n.scrollIntoView({ block: 'nearest' }); }
  }
  function toggle() {
    const r = rows[cur]; if (!r) return;
    const e = r.kid ? r.parent : r.e;
    if (!(e.Kids && e.Kids.length)) return;
    open.has(e.Key) ? open.delete(e.Key) : open.add(e.Key);
    cur = rows.findIndex(x => x.e === e); draw();
  }
  function setZoom(z) {
    z = Math.min(Math.max(z, 0), ZOOMS.length - 1); if (z === zoom) return;
    const centre = t0 + (scroller.scrollLeft + (scroller.clientWidth - labelW()) / 2) / ppd() * DAY;
    zoom = z; app.prefs.set('roadmap.zoom', z);
    draw();
    scroller.scrollLeft = Math.max(xOf(centre) - (scroller.clientWidth - labelW()) / 2, 0);
  }
  function today() { scroller.scrollTo({ left: Math.max(xOf(Date.now()) - (scroller.clientWidth - labelW()) / 2, 0), behavior: 'smooth' }); }
  const move = d => { if (rows.length) { cur = Math.min(Math.max(cur + d, 0), rows.length - 1); markCur(); } };
  const pan = d => scroller.scrollBy({ left: d * 120, behavior: 'smooth' });

  scope.bind(['j', 'ArrowDown'], () => move(1), 'next row', { group: 'Roadmap' });
  scope.bind(['k', 'ArrowUp'], () => move(-1), 'previous row', { group: 'Roadmap' });
  scope.bind(['h', 'ArrowLeft'], () => pan(-1), 'scroll left', { group: 'Roadmap' });
  scope.bind(['l', 'ArrowRight'], () => pan(1), 'scroll right', { group: 'Roadmap' });
  scope.bind(['+', '='], () => setZoom(zoom + 1), 'zoom in', { group: 'Roadmap' });
  scope.bind(['-', '_'], () => setZoom(zoom - 1), 'zoom out', { group: 'Roadmap' });
  scope.bind('.', today, 'scroll to today', { group: 'Roadmap' });
  scope.bind('Space', toggle, 'fold epic issues', { group: 'Roadmap' });
  scope.bind('Enter', () => { const r = rows[cur]; if (r) app.panel.open(r.e.Key); }, 'open', { group: 'Roadmap' });
  scope.bind('R', () => load(true), 'reload', { group: 'Roadmap' });
  scope.bind('n', () => app.actions.create({ project, type: app.session.ui.RoadmapEpicType || 'Epic' }), 'new epic', { group: 'Roadmap' });
  const offBus = app.bus.on('issue:changed', () => load(true));
  const offM = onMetrics(() => rows.length && draw());
  const off = () => { offBus(); offM(); };

  await load();
  return () => { token++; off(); };
}
