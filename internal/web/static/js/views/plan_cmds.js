// Palette commands for planning, reports and the roadmap; registered at start,
// the views themselves load on demand.
import { KINDS } from './report_kinds.js';

export function register(app) {
  const c = app.commands;
  c.register({ id: 'plan:open', title: 'Planning', group: 'Planning', run: () => app.go('/planning') });
  c.register({ id: 'roadmap:open', title: 'Roadmap', group: 'Planning', run: () => app.go('/roadmap') });
  for (const [id, label] of KINDS) c.register({ id: 'reports:' + id, title: 'Report: ' + label, group: 'Reports', run: () => app.go('/reports/' + id) });
  const inPlan = () => app.route && app.route.name === 'planning';
  const fire = a => () => document.dispatchEvent(new CustomEvent('plan:action', { detail: a }));
  for (const [id, title] of [['new', 'Sprint: new'], ['start', 'Sprint: start'], ['close', 'Sprint: complete'], ['edit', 'Sprint: edit name, goal, dates']])
    c.register({ id: 'plan:' + id, title, group: 'Planning', when: inPlan, run: fire(id) });
}
