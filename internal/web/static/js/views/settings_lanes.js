// The lane layout editor on the settings page: ui.lane_layouts drawn over a board's real columns. Drag a column onto
// a lane to stack it there, between lanes for a lane of its own, onto Hidden to take it off the board; drag a lane's
// head to reorder; type in its name to rename. The server arranges a layout over the board (internal/lanes, as the
// board does); statuses of columns on other boards stay with their lane. Each change writes the config file.
// Keys: enter on the row starts editing; h/l pick a column, H/L stack it on the lane before or after, n gives it a
// lane of its own, x hides or shows it, < > move its lane, r renames its lane, esc leaves.
import { h, debounce } from '../lib/dom.js';
import { icon } from '../lib/icons.js';
import { saver } from './settings_config.js';
import { projectOf, lastBoard, boardsOf } from './plan_ctx.js';

const DRAG_COL = 'text/x-laneway-col', DRAG_LANE = 'text/x-laneway-lane';

// designLanes gives the settings row of ui.lane_layouts its editor.
export function designLanes(app, host, options) {
  const O = options.find(o => o.name === 'lane_layouts');
  if (!O) return () => {};
  const ui = () => (app.session && app.session.ui) || {};
  // specs are ui.lane_layouts as written: lower-case keys, status ids as strings.
  const norm = l => ({ name: l.Name || '', boards: (l.Boards || []).map(Number), hidden: (l.Hidden || []).map(String),
    lanes: (l.Lanes || []).map(x => ({ name: x.Name || '', statuses: (x.Statuses || []).map(String) })) });
  const specs = (ui().LaneLayouts || []).map(norm);
  let cur = 0, pick = -1, seq = 0;
  // B is the board the layout shows over; view the layout arranged on it: lanes of column indexes, with the
  // statuses of columns elsewhere (foreign) kept, and the hidden columns.
  const B = { project: projectOf(app, null), board: 0, boards: [], cols: [], names: {}, fits: true, err: '' };
  let view = null;

  const out = l => {
    const o = { name: l.name, lanes: l.lanes.map(x => (x.name ? { name: x.name, statuses: x.statuses } : { statuses: x.statuses })) };
    if (l.boards.length) o.boards = l.boards;
    if (l.hidden.length) o.hidden = l.hidden;
    return o;
  };
  const save = debounce(saver(app, O, () => { const v = specs.filter(l => l.name.trim() && l.lanes.length).map(out); return v.length ? v : null; }), 400);
  const ids = ci => (B.cols[ci].StatusIDs || []).map(String);
  const colName = ci => B.cols[ci].Name;
  const laneName = l => l.name || (l.cols.length ? colName(l.cols[0]) : 'Lane');

  // ---- the layout over the board
  async function arrange() {
    const spec = specs[cur], n = ++seq;
    if (!spec || !B.board) { view = null; host.redraw(O); return; }
    try {
      const r = await app.api.post('/boards/' + B.board + '/arrange', { Layout: spec });
      if (n !== seq) return;
      B.cols = r.Columns || []; B.names = r.StatusNames || {}; B.fits = r.Fits; B.err = '';
      const here = new Set(B.cols.flatMap((c, i) => ids(i)));
      const foreign = list => list.filter(id => !here.has(id));
      const lanes = (r.Lanes || []).map(l => ({ name: l.Spec >= 0 ? spec.lanes[l.Spec].name : '', cols: l.Cols, foreign: l.Spec >= 0 ? foreign(spec.lanes[l.Spec].statuses) : [], spec: l.Spec }));
      // A lane of the layout with no column here stays, for the boards it is for, after the one before it.
      spec.lanes.forEach((sl, si) => {
        if (lanes.some(l => l.spec === si)) return;
        let at = 0;
        lanes.forEach((l, j) => { if (l.spec >= 0 && l.spec < si) at = j + 1; });
        lanes.splice(at, 0, { name: sl.name, cols: [], foreign: sl.statuses.slice(), spec: si });
      });
      view = { lanes, hidden: r.Hidden || [], foreignHidden: foreign(spec.hidden) };
    } catch (e) {
      if (n === seq) { view = null; B.err = e.message; }
    }
    host.redraw(O);
  }
  // commit writes the view back into the layout, saves it, and has the server arrange it again (whether it fits).
  function commit() { write(); host.redraw(O); arrange(); }
  function write() {
    const spec = specs[cur];
    view.lanes = view.lanes.filter(l => l.cols.length || l.foreign.length);
    spec.lanes = view.lanes.map(l => ({ name: l.cols.length && l.name === colName(l.cols[0]) ? '' : l.name, statuses: [...l.cols.flatMap(ids), ...l.foreign] }));
    spec.hidden = [...view.hidden.flatMap(ids), ...view.foreignHidden];
    save();
  }
  const laneOf = ci => view.lanes.find(l => l.cols.includes(ci));
  function take(ci) {
    const l = laneOf(ci);
    if (l) l.cols = l.cols.filter(x => x !== ci);
    view.hidden = view.hidden.filter(x => x !== ci);
  }
  // stack puts column ci in lane l; alone makes it a lane of its own before lane `before` (null: last).
  function stack(ci, l) { take(ci); l.cols.push(ci); pick = ci; commit(); }
  function alone(ci, before) {
    take(ci);
    const at = before ? view.lanes.indexOf(before) : view.lanes.length;
    view.lanes.splice(at < 0 ? view.lanes.length : at, 0, { name: '', cols: [ci], foreign: [], spec: -1 });
    pick = ci; commit();
  }
  function hide(ci) {
    if (view.hidden.includes(ci)) return alone(ci, null);
    take(ci); view.hidden.push(ci); pick = ci; commit();
  }
  function moveLane(l, before) {
    if (l === before) return;
    view.lanes.splice(view.lanes.indexOf(l), 1);
    const at = before ? view.lanes.indexOf(before) : view.lanes.length;
    view.lanes.splice(at, 0, l); commit();
  }

  // ---- layouts and boards
  // newLayout starts from the board's columns, a lane each (the server places none, so each keeps its own).
  async function newLayout() {
    if (!B.board) return app.ui.toast('Pick a board first', { kind: 'err' });
    let n = specs.length + 1;
    while (specs.some(l => l.name === 'Layout ' + n)) n++;
    specs.push({ name: 'Layout ' + n, boards: [], hidden: [], lanes: [] });
    cur = specs.length - 1; pick = -1;
    await arrange();
    if (view) commit();
  }
  function dropLayout() {
    specs.splice(cur, 1);
    cur = Math.max(0, Math.min(cur, specs.length - 1));
    save(); arrange();
  }
  async function setProject(p) {
    B.project = p; B.boards = []; B.board = 0;
    try { B.boards = await boardsOf(app, p); } catch (e) { B.err = e.message; }
    const last = lastBoard(app, p);
    B.board = (B.boards.find(b => b.ID === last) || B.boards[0] || {}).ID || 0;
    arrange();
  }
  function toggleBoard(id) {
    const spec = specs[cur];
    spec.boards = spec.boards.includes(id) ? spec.boards.filter(x => x !== id) : [...spec.boards, id];
    commit();
  }

  // ---- keyboard: a scope over the settings page's own while editing (as the card designer's)
  let keys = null;
  const outside = e => { if (!e.target.closest || !e.target.closest('.ln, .modal, .pick')) { leave(); host.redraw(O); } };
  const leave = () => { if (keys) { keys.dispose(); keys = null; document.removeEventListener('mousedown', outside, true); host.end(); } };
  const order = () => (view ? [...view.lanes.flatMap(l => l.cols), ...view.hidden] : []);
  const choose = d => { const o = order(); if (!o.length) return; pick = o[(Math.max(0, o.indexOf(pick)) + d + o.length) % o.length]; host.redraw(O); };
  function step(d) { // H/L: onto the lane before or after
    if (pick < 0 || !view) return;
    const l = laneOf(pick), i = l ? view.lanes.indexOf(l) : view.lanes.length;
    const to = view.lanes[i + d];
    if (to) stack(pick, to);
  }
  function shiftLane(d) { // < >: the column's lane one place along
    const l = pick >= 0 && view && laneOf(pick);
    if (!l) return;
    const i = view.lanes.indexOf(l), j = i + d;
    if (j < 0 || j >= view.lanes.length) return;
    view.lanes.splice(i, 1); view.lanes.splice(j, 0, l); commit();
  }
  function rename() {
    const l = pick >= 0 && view && laneOf(pick);
    const inp = l && O.el && O.el.querySelectorAll('.ln-name')[view.lanes.indexOf(l)];
    if (inp) { inp.focus(); inp.select(); }
  }
  O.wide = true;
  O.activate = O.change = () => {
    if (!view) return;
    pick = pick >= 0 ? pick : order()[0] ?? -1;
    host.begin({ o: O, commit: () => {}, cancel: () => { leave(); host.redraw(O); } });
    if (keys) keys.dispose(); else document.addEventListener('mousedown', outside, true);
    keys = app.keys.scope('lanes', { layer: 2, covers: () => true });
    const g = { group: 'Lane layouts' };
    keys.bind('Escape', () => { if (document.activeElement && document.activeElement.matches('input')) document.activeElement.blur(); else { leave(); host.redraw(O); } }, 'leave the editor', { ...g, bar: 'leave', input: true });
    keys.bind(['h', 'ArrowLeft'], () => choose(-1), 'previous column', { ...g, bar: 'h l pick' });
    keys.bind(['l', 'ArrowRight'], () => choose(1), 'next column', g);
    keys.bind('H', () => step(-1), 'stack it on the lane before', { ...g, bar: 'H L stack' });
    keys.bind('L', () => step(1), 'stack it on the lane after', g);
    keys.bind('n', () => { if (pick >= 0) { const l = laneOf(pick); alone(pick, l ? view.lanes[view.lanes.indexOf(l) + 1] || null : null); } }, 'a lane of its own', { ...g, bar: 'n own lane' });
    keys.bind(['x', 'Delete'], () => { if (pick >= 0) hide(pick); }, 'hide it, or show it again', { ...g, bar: 'x hide' });
    keys.bind('<', () => shiftLane(-1), 'move its lane left', { ...g, bar: '< > lane' });
    keys.bind('>', () => shiftLane(1), 'move its lane right', g);
    keys.bind('r', rename, 'rename its lane', g);
    host.redraw(O);
  };
  O.reset = () => { specs.length = 0; cur = 0; view = null; save(); host.redraw(O); };

  // ---- drawing
  let dragLane = null;
  const over = (e, ok) => { if (!ok) return; e.preventDefault(); e.dataTransfer.dropEffect = 'move'; e.currentTarget.classList.add('over'); };
  const leaveZone = e => { if (!e.currentTarget.contains(e.relatedTarget)) e.currentTarget.classList.remove('over'); };
  const has = (e, t) => e.dataTransfer.types.includes(t);
  const chip = ci => h('button.cd-chip.ln-col' + (keys && ci === pick ? '.on' : ''), {
    type: 'button', tabindex: -1, draggable: true, title: (B.cols[ci].StatusIDs || []).map(id => B.names[id] || id).join(', '),
    ondragstart: e => { e.dataTransfer.setData(DRAG_COL, String(ci)); e.dataTransfer.effectAllowed = 'move'; e.currentTarget.classList.add('dragging'); },
    ondragend: e => e.currentTarget.classList.remove('dragging'),
    onclick: () => { pick = ci; if (!keys) O.activate(); else host.redraw(O); },
  }, colName(ci));
  const gap = before => h('div.ln-gap', {
    title: 'Drop a column here for a lane of its own',
    ondragover: e => over(e, has(e, DRAG_COL) || has(e, DRAG_LANE)), ondragleave: leaveZone,
    ondrop: e => {
      e.preventDefault(); e.currentTarget.classList.remove('over');
      if (has(e, DRAG_LANE) && dragLane) return moveLane(dragLane, before);
      const ci = Number(e.dataTransfer.getData(DRAG_COL));
      if (Number.isInteger(ci)) alone(ci, before);
    },
  });
  const lane = l => h('div.ln-lane', {
    ondragover: e => over(e, has(e, DRAG_COL)), ondragleave: leaveZone,
    ondrop: e => { e.preventDefault(); e.currentTarget.classList.remove('over'); const ci = Number(e.dataTransfer.getData(DRAG_COL)); if (Number.isInteger(ci)) stack(ci, l); },
  },
  h('div.ln-head',
    h('span.cd-grip', { draggable: true, title: 'Drag to reorder', ondragstart: e => { dragLane = l; e.dataTransfer.setData(DRAG_LANE, '1'); e.dataTransfer.effectAllowed = 'move'; }, ondragend: () => { dragLane = null; } }, icon('grip-vertical')),
    h('input.input.ln-name', { type: 'text', value: l.name, placeholder: laneName(l), spellcheck: false, autocomplete: 'off', 'aria-label': 'Lane name',
      oninput: e => { l.name = e.target.value.trim(); write(); } })),
  h('div.ln-body', l.cols.map(chip),
    l.foreign.length > 0 && h('span.faint.ln-foreign', { title: l.foreign.join(', ') }, (l.cols.length ? '+ ' : '') + l.foreign.length + (l.foreign.length === 1 ? ' status' : ' statuses') + ' on other boards')));

  O.render = () => {
    const spec = specs[cur], projects = (app.session && app.session.projects) || [];
    const top = h('div.row.ln-top',
      specs.length > 0 && h('select.input', { 'aria-label': 'Layout', onchange: e => { cur = Number(e.target.value); pick = -1; arrange(); } },
        specs.map((l, i) => h('option', { value: i, selected: i === cur }, l.name || 'Untitled'))),
      spec && h('input.input.ln-lname', { type: 'text', value: spec.name, placeholder: 'Name', 'aria-label': 'Layout name', spellcheck: false,
        oninput: debounce(e => { spec.name = e.target.value.trim(); save(); }, 500), onchange: () => host.redraw(O) }),
      h('button.btn', { type: 'button', onclick: newLayout }, icon('plus'), 'New layout'),
      spec && h('button.btn.ghost', { type: 'button', onclick: dropLayout }, 'Delete layout'),
      h('span.spacer'), h('span.cd-status'));
    const board = h('div.row.ln-board',
      h('span.cd-label', 'Board'),
      h('select.input', { 'aria-label': 'Project', onchange: e => setProject(e.target.value) }, projects.map(p => h('option', { value: p, selected: p === B.project }, p))),
      h('select.input', { 'aria-label': 'Board', onchange: e => { B.board = Number(e.target.value); arrange(); } },
        B.boards.map(b => h('option', { value: b.ID, selected: b.ID === B.board }, b.Name))),
      spec && h('span.cd-label', { title: 'None ticked: every board the layout fits' }, 'Only on'),
      spec && h('span.ln-boards',
        B.boards.map(b => h('label.ln-bchk', h('input', { type: 'checkbox', checked: spec.boards.includes(b.ID), onchange: () => toggleBoard(b.ID) }), b.Name)),
        spec.boards.filter(id => !B.boards.some(b => b.ID === id)).map(id => h('span.chip', 'board ' + id, h('button.cd-fx', { type: 'button', 'aria-label': 'Remove board ' + id, onclick: () => toggleBoard(id) }, '×')))));
    if (!spec) return h('div.cd.ln', top, board, h('div.faint', 'No lane layouts yet. A layout stacks, reorders, renames and hides a board\'s columns, for you; alt+l switches to it.'));
    if (!view) return h('div.cd.ln', top, board, h('div.faint', B.err || (B.board ? 'Loading the board…' : 'Pick a board')));
    const lanes = h('div.ln-lanes', view.lanes.flatMap(l => [gap(l), lane(l)]), gap(null));
    const hidden = h('div.cd-trayrow', h('span.cd-label', 'Hidden'),
      h('div.cd-zone.ln-hidden', {
        ondragover: e => over(e, has(e, DRAG_COL)), ondragleave: leaveZone,
        ondrop: e => { e.preventDefault(); e.currentTarget.classList.remove('over'); const ci = Number(e.dataTransfer.getData(DRAG_COL)); if (Number.isInteger(ci) && !view.hidden.includes(ci)) hide(ci); },
      }, view.hidden.map(chip), !view.hidden.length && h('span.faint', 'drop a column here to take it off the board')));
    return h('div.cd.ln', top, board, lanes, hidden,
      h('div.row.cd-foot',
        h('span.faint', keys ? 'h l pick · H L stack · n own lane · x hide · < > lane · r rename · esc done' : 'Drag columns onto lanes, between them, or onto Hidden; enter for keys.'),
        h('span.spacer'),
        !B.fits && h('span.st-err', 'Not on this board: tick it, or place two of its columns')));
  };
  setProject(B.project);
  return () => leave();
}
