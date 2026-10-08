// Palette commands for planning, reports and the roadmap; registered at start,
// the views themselves load on demand.
import { KINDS } from './report_kinds.js';
import { T } from '../lib/i18n.js';

export function register(app) {
  const c = app.commands;
  c.register({ id: 'plan:open', title: T('Planning'), group: T('Planning'), run: () => app.go('/planning') });
  c.register({ id: 'roadmap:open', title: T('Roadmap'), group: T('Planning'), run: () => app.go('/roadmap') });
  for (const [id, label] of KINDS) c.register({ id: 'reports:' + id, title: T('Report: %s', label), group: T('Reports'), run: () => app.go('/reports/' + id) });
  const inPlan = () => app.route && app.route.name === 'planning';
  const fire = a => () => document.dispatchEvent(new CustomEvent('plan:action', { detail: a }));
  for (const [id, title] of [['new', T('Sprint: new')], ['start', T('Sprint: start')], ['close', T('Sprint: complete')], ['edit', T('Sprint: edit name, goal, dates')]])
    c.register({ id: 'plan:' + id, title, group: T('Planning'), when: inPlan, run: fire(id) });
}
