import { test } from 'node:test';
import assert from 'node:assert/strict';
import { compile, builderValues } from '../static/js/lib/cardquery.js';

const day = 864e5, zero = '0001-01-01T00:00:00Z';
const ago = d => new Date(Date.now() - d * day).toISOString();
const cards = [
  { Key: 'A-1', Status: 'New', Assignee: 'Ada', Resolution: '', Resolved: zero },
  { Key: 'A-2', Status: 'Closed', Assignee: 'Bob', Resolution: 'Duplicate', Resolved: ago(2), Done: true },
  { Key: 'A-3', Status: 'Closed', Assignee: 'Ada', Resolution: 'Done', Resolved: ago(10), Done: true },
];
const find = q => cards.filter(compile(q)).map(c => c.Key).join(' ');

test('resolution and resolved, as the TUI reads them', () => {
  assert.equal(find('resolution:dup'), 'A-2');
  assert.equal(find('resolved<7d'), 'A-2');
  assert.equal(find('resolved>7d'), 'A-3');
  assert.equal(find('resolution:'), 'A-1');
});

test('the builder counts a value over what the query\'s other terms leave', () => {
  const vals = (field, q) => builderValues(cards, field, {}, q).map(v => v.label + ' ' + v.n).join(', ');
  assert.equal(vals('assignee', 'status:closed'), 'Ada 1, Bob 1');
  assert.equal(vals('assignee', ''), 'Ada 2, Bob 1');
  // its own field's terms don't narrow it: picking another value adds to them
  assert.equal(vals('status', 'status:closed'), 'Closed 2, New 1');
  assert.equal(vals('resolution', '-is:done'), '');
});
