// How a list of cards sorts by a column: the board's list and planning share it.
import { isZero } from './fmt.js';
import { extraOf } from './cardquery.js';

const PRIO_ORD = { blocker: 0, highest: 0, critical: 0, high: 1, major: 1, medium: 2, normal: 2, low: 3, minor: 3, lowest: 4, trivial: 4 };
export const prioOrd = c => { const v = PRIO_ORD[(c.Priority || '').toLowerCase()]; return v == null ? 5 : v; };
export const time = t => (isZero(t) ? Infinity : Date.parse(t));
export const num = c => (c.Points === '' || c.Points == null ? -1 : Number(c.Points) || 0);
// text orders strings A–Z, numbers by value, empty last.
const text = f => (a, b) => (!f(a)) - (!f(b)) || (f(a) || '').localeCompare(f(b) || '', undefined, { numeric: true });
// ageFrom is when a card's age counts from; done cards have none.
const ageFrom = c => (c.Done ? Infinity : isZero(c.Since) ? time(c.Created) : time(c.Since));

// comparators by column id, ascending; colIndexOf places a card's status on the board.
export const comparators = colIndexOf => ({
  key: (a, b) => a.Key.localeCompare(b.Key, undefined, { numeric: true }),
  summary: (a, b) => a.Summary.localeCompare(b.Summary),
  status: (a, b) => colIndexOf(a) - colIndexOf(b),
  priority: (a, b) => prioOrd(a) - prioOrd(b),
  points: (a, b) => num(b) - num(a),
  assignee: text(c => c.Assignee),
  epic: text(c => c.ParentSummary),
  labels: text(c => c.Labels),
  reporter: text(c => c.Reporter),
  updated: (a, b) => time(b.Updated) - time(a.Updated) || 0,
  created: (a, b) => time(b.Created) - time(a.Created) || 0,
  due: (a, b) => (time(a.Due) === time(b.Due) ? 0 : time(a.Due) < time(b.Due) ? -1 : 1),
  age: (a, b) => (ageFrom(a) === ageFrom(b) ? 0 : ageFrom(a) < ageFrom(b) ? -1 : 1), // stalest first
});

// cmpOf is col's comparator, a custom field's ('x:Name') too; undefined when col doesn't sort.
export function cmpOf(cmps, col) {
  if (!col.startsWith('x:')) return cmps[col];
  const f = col.slice(2).toLowerCase();
  return text(c => extraOf(c)[f]);
}

// sortCards is cards in col's order, dir 1 or -1; 'rank' keeps their order, reversed for -1.
export function sortCards(cards, cmps, col, dir) {
  const cmp = col === 'rank' ? null : cmpOf(cmps, col);
  if (!cmp) return dir < 0 ? cards.slice().reverse() : cards;
  return cards.slice().sort((a, b) => cmp(a, b) * dir);
}
