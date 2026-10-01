// The assignee filter of the board and planning, as the TUI's: several people at once.
// A filter is null for anyone, else a Set of AccountIDs, '-' for unassigned.
import { h } from './dom.js';

export const whoOf = c => c.AssigneeID || c.Assignee || '';
export const passesWho = (who, c) => who == null || who.has(whoOf(c) || '-');
export const whoKey = who => (who ? [...who].sort().join(',') : '');

// "Ada, Bob", past two names "Ada +2"; name(id) names one.
export function whoLabel(who, name) {
  const names = [...who].map(id => (id === '-' ? 'Unassigned' : name(id)));
  return names.length > 2 ? names[0] + ' +' + (names.length - 1) : names.join(', ');
}

// pickWho(app, people: Map id→name, who) → the new filter, or undefined when cancelled. Space ticks several;
// enter without a tick takes the row under the cursor.
export async function pickWho(app, people, who) {
  const items = [{ id: null, name: 'Anyone' }, { id: '-', name: 'Unassigned' }, ...[...people].map(([id, name]) => ({ id, name })).sort((a, b) => a.name.localeCompare(b.name))];
  const r = await app.ui.pick({
    title: 'Assignee', items, multi: true, enterPicks: true, current: items.find(i => who && who.has(i.id)),
    selected: items.filter(i => who && who.has(i.id)),
    label: i => i.name, render: i => h('span.pick-label', i.id && i.id !== '-' ? app.ui.avatar(i.name, '', 18) : '', ' ', i.name),
  });
  if (!r) return undefined;
  const ids = r.filter(i => i.id != null).map(i => i.id);
  return ids.length ? new Set(ids) : null; // Anyone, or nobody ticked
}
