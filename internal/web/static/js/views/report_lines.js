// Lines across a board, as the Go client's jira.Line: an issue is past a line while its status sits in the line's
// column or one right of it; with `first`, from its first crossing on. No line (null) is Jira's done: the resolution.
import { isZero } from '../lib/fmt.js';
import { T } from '../lib/i18n.js';

// makeLine is the line at the column named name (any case), null for '' or a name no column has.
export function makeLine(cols, name, first) {
  if (!name) return null;
  const at = (cols || []).findIndex(c => c.Name.toLowerCase() === name.toLowerCase());
  if (at < 0) return null;
  return { name: cols[at].Name, at, first: !!first, ids: new Set(cols.slice(at).flatMap(c => c.StatusIDs || [])) };
}

export const label = l => (l ? l.name : T('done'));

// statusAt is the issue's status id at t (ms), replayed from its moves.
export function statusAt(is, t) {
  const mv = is.Moves || [];
  for (let i = mv.length - 1; i >= 0; i--) if (+new Date(mv[i].When) <= t) return mv[i].To;
  return mv.length ? mv[0].From : is.Status;
}

// since is when the issue got past the line as it counts now (ms; -Infinity: before its history), null while it is not.
export function since(l, is) {
  if (!l) return isZero(is.Resolved) ? null : +new Date(is.Resolved);
  const mv = is.Moves || [];
  if (l.first) {
    if (l.ids.has(statusAt(is, -Infinity))) return -Infinity;
    const m = mv.find(m => l.ids.has(m.To));
    return m ? +new Date(m.When) : null;
  }
  if (!l.ids.has(is.Status)) return null;
  for (let i = mv.length - 1; i >= 0; i--) if (!l.ids.has(mv[i].From)) return +new Date(mv[i].When);
  return -Infinity;
}

// past is whether the issue stood past the line at t (ms).
export function past(l, is, t) {
  if (!l || l.first) { const s = since(l, is); return s != null && s <= t; }
  return l.ids.has(statusAt(is, t));
}

// order is the two lines, the one further left first; Jira's done comes last.
export function order(a, b) {
  const pos = l => (l ? l.at : Infinity);
  return pos(a) > pos(b) ? [b, a] : [a, b];
}
