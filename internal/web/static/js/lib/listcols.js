// The order of a card list's columns (lib/cardlist.js), apart for node's tests.

export const FIXED = ['mark', 'key', 'summary']; // every list has them; the mark stays first

// fixCols is list in its order with the mark first and the key and summary in it.
export const fixCols = list => ['mark', ...FIXED.slice(1).filter(c => !list.includes(c)), ...list.filter(c => c !== 'mark')];

// moveCol is cols with id moved before to, or after it when after.
export function moveCol(cols, id, to, after) {
  if (id === to || id === 'mark' || (to === 'mark' && !after)) return cols;
  const out = cols.filter(c => c !== id);
  out.splice(out.indexOf(to) + (after ? 1 : 0), 0, id);
  return fixCols(out);
}

// pickOrder is the columns picked (and those every list has), in cols' order, new ones after in all's.
export const pickOrder = (all, cols, picked) => fixCols([...cols.filter(c => FIXED.includes(c) || picked.includes(c)), ...all.filter(c => picked.includes(c) && !cols.includes(c))]);
