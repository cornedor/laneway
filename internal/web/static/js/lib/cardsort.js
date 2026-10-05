// How a list of cards sorts by a column: the board's list and planning share it.
import { isZero } from './fmt.js';

const PRIO_ORD = { blocker: 0, highest: 0, critical: 0, high: 1, major: 1, medium: 2, normal: 2, low: 3, minor: 3, lowest: 4, trivial: 4 };
export const prioOrd = c => { const v = PRIO_ORD[(c.Priority || '').toLowerCase()]; return v == null ? 5 : v; };
export const time = t => (isZero(t) ? Infinity : Date.parse(t));
export const num = c => (c.Points === '' || c.Points == null ? -1 : Number(c.Points) || 0);

// comparators by column id, ascending; colIndexOf places a card's status on the board.
export const comparators = colIndexOf => ({
  key: (a, b) => a.Key.localeCompare(b.Key, undefined, { numeric: true }),
  summary: (a, b) => a.Summary.localeCompare(b.Summary),
  status: (a, b) => colIndexOf(a) - colIndexOf(b),
  priority: (a, b) => prioOrd(a) - prioOrd(b),
  points: (a, b) => num(b) - num(a),
  assignee: (a, b) => (!a.Assignee) - (!b.Assignee) || (a.Assignee || '').localeCompare(b.Assignee || ''),
  epic: (a, b) => (!a.ParentSummary) - (!b.ParentSummary) || (a.ParentSummary || '').localeCompare(b.ParentSummary || ''),
  updated: (a, b) => time(b.Updated) - time(a.Updated) || 0,
  created: (a, b) => time(b.Created) - time(a.Created) || 0,
  due: (a, b) => (time(a.Due) === time(b.Due) ? 0 : time(a.Due) < time(b.Due) ? -1 : 1),
});

// sortCards is cards in col's order, dir 1 or -1; 'rank' keeps their order, reversed for -1.
export function sortCards(cards, cmps, col, dir) {
  if (col === 'rank' || !cmps[col]) return dir < 0 ? cards.slice().reverse() : cards;
  return cards.slice().sort((a, b) => cmps[col](a, b) * dir);
}
