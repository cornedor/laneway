// One project + board context for every project-scoped view (board, planning,
// reports, roadmap, standup, create). Order: route params, else the last
// project, else the first configured; board: the project's remembered board,
// else its first. Prefs 'project' and 'board.last.<project>' (server side,
// mirrored in localStorage by app.prefs) persist it; nothing ever asks for a
// board when one is remembered.
import { h } from '../lib/dom.js';

function pref(app, k) {
  const v = app.prefs.get(k, '');
  if (v !== '' && v != null) return String(v);
  try { return localStorage.getItem('lw:p:' + k) || ''; } catch (e) { return ''; }
}

export function lastProject(app) {
  const legacy = pref(app, 'board.last').split('/')[0];
  return pref(app, 'project') || legacy || '';
}

// The remembered board id of a project, 0 when none.
export function lastBoard(app, project) {
  if (!project) return 0;
  const n = Number(pref(app, 'board.last.' + project) || pref(app, 'board:' + project));
  if (n) return n;
  const [p, b] = pref(app, 'board.last').split('/');
  return p === project ? Number(b) || 0 : 0;
}

export function projectOf(app, params) {
  const s = app.session;
  return (params && params.project) || lastProject(app) || (s.projects && s.projects[0]) || '';
}

// setCtx remembers project and (when given) board for all views.
export function setCtx(app, project, board) {
  if (!project) return;
  if (pref(app, 'project') !== project) app.prefs.set('project', project);
  const id = board && (board.ID || board);
  if (id && lastBoard(app, project) !== Number(id)) app.prefs.set('board.last.' + project, Number(id));
}

export async function boardsOf(app, project) {
  return (await app.api.get('/projects/' + encodeURIComponent(project) + '/boards')) || [];
}

// resolve → {project, board, boards}; board is null when the project has no
// board of the wanted type (scrum unless any).
export async function resolve(app, params, { scrum = true } = {}) {
  const project = projectOf(app, params);
  if (!project) return { project: '', board: null, boards: [] };
  const boards = await boardsOf(app, project);
  const ok = b => !scrum || b.Type === 'scrum';
  const explicit = params && params.board ? boards.find(b => String(b.ID) === String(params.board)) : null;
  const last = lastBoard(app, project);
  const board = explicit || boards.find(b => b.ID === last && ok(b)) || boards.find(ok) || null;
  // A fallback never overwrites a remembered board (a kanban one, in a scrum-only view).
  setCtx(app, project, explicit || (last ? null : board));
  return { project, board, boards };
}

export const remember = (app, project, board) => setCtx(app, project, board);

// pickContext → {project, board} (board null for boards:false or when the
// project has none of the wanted type). Asks for a board only when the project
// is the current one (you want another board of it) or has no remembered one.
export async function pickContext(app, { project: cur = '', scrum = true, boards = true } = {}) {
  const mine = app.session.projects || [];
  let items = [];
  try { items = [...(await app.api.get('/projects'))].sort((a, b) => (mine.includes(b.Key) - mine.includes(a.Key))); } catch (e) { items = []; }
  if (!items.length) mine.forEach(k => items.push({ Key: k, Name: k }));
  if (!items.length) { app.ui.toast('No projects', { kind: 'err' }); return null; }
  const p = await app.ui.pick({ title: 'Project', items, label: x => x.Key + ' ' + x.Name, detail: x => (mine.includes(x.Key) ? '\u2605' : '') });
  if (!p) return null;
  if (!boards) return { project: p.Key, board: null };
  let all;
  try { all = await boardsOf(app, p.Key); } catch (e) { app.ui.errToast(e); return null; }
  const list = all.filter(b => !scrum || b.Type === 'scrum');
  if (!list.length) return { project: p.Key, board: null };
  const last = lastBoard(app, p.Key);
  let b = list.find(x => x.ID === last);
  if (list.length === 1) b = list[0];
  else if (!b || p.Key === cur) {
    b = await app.ui.pick({ title: p.Key + ' board', items: list, label: x => x.Name, detail: x => x.Type });
    if (!b) return null;
  }
  return { project: p.Key, board: b };
}

// switcher: the project/board crumb for a view's context slot, with key B.
// Returns {btn, label(project, board)}; onPick({project, board}) after the choice is remembered.
export function switcher(app, { scope, context, project, board, scrum = true, boards = true, onPick, key = 'B', group }) {
  let cur = project;
  const run = async () => {
    const r = await pickContext(app, { project: cur, scrum, boards }); if (!r) return;
    setCtx(app, r.project, r.board);
    cur = r.project;
    onPick(r);
  };
  const btn = app.chrome.crumb('Project' + (boards ? ' and board' : '') + '  (' + key + ')', run);
  const label = (p, b) => { cur = p; app.chrome.label(btn, p || '\u2014', b && b.Name); };
  label(project, board);
  context.append(btn);
  if (scope) scope.bind(key, run, boards ? 'switch project / board' : 'switch project', { group });
  return { btn, label, run };
}

export const noBoard = (what, project) => h('div.empty', h('h2', what), h('p', project ? project + ' has no scrum board. ' + what + ' need sprints.' : 'No project configured.'));
