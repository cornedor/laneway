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

// pickProject → the project key, or null.
export async function pickProject(app) {
  const mine = app.session.projects || [];
  let items = [];
  try { items = [...(await app.api.get('/projects'))].sort((a, b) => (mine.includes(b.Key) - mine.includes(a.Key))); } catch (e) { items = []; }
  if (!items.length) mine.forEach(k => items.push({ Key: k, Name: k }));
  if (!items.length) { app.ui.toast('No projects', { kind: 'err' }); return null; }
  const p = await app.ui.pick({ title: 'Project', items, label: x => x.Key + ' ' + x.Name, detail: x => (mine.includes(x.Key) ? '\u2605' : '') });
  return p ? p.Key : null;
}

// boardOf → the project's remembered board of the wanted type, else its first; null when none. Never asks.
export async function boardOf(app, project, { scrum = true } = {}) {
  const list = (await boardsOf(app, project)).filter(b => !scrum || b.Type === 'scrum');
  return list.find(b => b.ID === lastBoard(app, project)) || list[0] || null;
}

// pickBoard → a board of the project (asks, even with a remembered one), null when cancelled or none.
export async function pickBoard(app, project, { scrum = true } = {}) {
  let list;
  try { list = (await boardsOf(app, project)).filter(b => !scrum || b.Type === 'scrum'); } catch (e) { app.ui.errToast(e); return null; }
  if (!list.length) { app.ui.toast('No boards in ' + project, { kind: 'err' }); return null; }
  if (list.length === 1) return list[0];
  return app.ui.pick({ title: project + ' board', items: list, label: x => x.Name, detail: x => x.Type });
}

// switcher: separate project and board crumbs for a view's context slot. Board key B
// (project-only views: B too), project key alt+p. Picking a project moves to its remembered
// board without asking. onPick({project, board}) after the choice is remembered.
// Returns {btn, boardBtn, label(project, board)}.
export function switcher(app, { scope, context, project, board, scrum = true, boards = true, onPick, key = 'B', group }) {
  let cur = project;
  const done = (p, b) => { setCtx(app, p, b); cur = p; onPick({ project: p, board: b }); };
  const runProject = async () => {
    const p = await pickProject(app); if (!p) return;
    let b = null;
    if (boards) { try { b = await boardOf(app, p, { scrum }); } catch (e) { app.ui.errToast(e); return; } }
    done(p, b);
  };
  const runBoard = async () => {
    if (!boards) return runProject();
    const b = await pickBoard(app, cur, { scrum }); if (b) done(cur, b);
  };
  const btn = app.chrome.crumb('Project  (alt+p' + (boards ? '' : ', ' + key) + ')', runProject);
  const boardBtn = boards ? app.chrome.crumb('Board  (' + key + ')', runBoard) : null;
  const label = (p, b) => {
    cur = p; app.chrome.label(btn, p || '\u2014');
    if (boardBtn) app.chrome.label(boardBtn, b && b.Name || '\u2014');
  };
  label(project, board);
  context.append(btn);
  if (boardBtn) context.append(boardBtn);
  if (scope) {
    scope.bind('alt+p', runProject, 'switch project (its last board)', { group });
    scope.bind(key, runBoard, boards ? 'switch board (same project)' : 'switch project', { group });
  }
  return { btn, boardBtn, label, run: runBoard, runProject };
}

export const noBoard = (what, project) => h('div.empty', h('h2', what), h('p', project ? project + ' has no scrum board. ' + what + ' need sprints.' : 'No project configured.'));
