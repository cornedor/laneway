// Which project and board the planning/report/roadmap views look at: the
// route's, else the last one used, else the first configured.
import { h } from '../lib/dom.js';

export function projectOf(app, params) {
  const s = app.session;
  return params.project || app.prefs.get('project', '') || (s.projects && s.projects[0]) || '';
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
  let board = params.board ? boards.find(b => String(b.ID) === String(params.board)) : null;
  if (!board) {
    const last = Number(app.prefs.get('board:' + project, 0));
    board = boards.find(b => b.ID === last && ok(b)) || boards.find(ok) || null;
  }
  return { project, board, boards };
}

export function remember(app, project, board) {
  if (project && app.prefs.get('project', '') !== project) app.prefs.set('project', project);
  if (board && Number(app.prefs.get('board:' + project, 0)) !== board.ID) app.prefs.set('board:' + project, board.ID);
}

// pickScope → {project, board} from every configured project's boards, or null.
export async function pickScope(app, { scrum = true, boards = true } = {}) {
  const projects = app.session.projects && app.session.projects.length ? app.session.projects : (await app.api.get('/projects')).map(p => p.Key);
  if (!boards) {
    const p = await app.ui.pick({ title: 'Project', items: projects, label: x => x });
    return p ? { project: p } : null;
  }
  const all = (await Promise.all(projects.map(async p => (await boardsOf(app, p)).filter(b => !scrum || b.Type === 'scrum').map(b => ({ project: p, board: b }))))).flat();
  if (!all.length) { app.ui.toast('No scrum boards', { kind: 'err' }); return null; }
  return app.ui.pick({ title: 'Board', items: all, label: x => x.project + ' · ' + x.board.Name });
}

export const noBoard = (what, project) => h('div.empty', h('h2', what), h('p', project ? project + ' has no scrum board. ' + what + ' need sprints.' : 'No project configured.'));
