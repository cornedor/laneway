import { test } from 'node:test';
import assert from 'node:assert/strict';
import { comparators, sortCards } from '../static/js/lib/cardsort.js';

const a = { Key: 'D-2', Summary: 'b', Points: '3', StatusID: '2' }, b = { Key: 'D-10', Summary: 'a', Points: '', StatusID: '1' }, c = { Key: 'D-9', Summary: 'c', Points: '1', StatusID: '1' };
const cmps = comparators(x => Number(x.StatusID));
const keys = l => l.map(x => x.Key).join(' ');

test('rank keeps the order, reversed backwards', () => {
  assert.equal(keys(sortCards([a, b, c], cmps, 'rank', 1)), 'D-2 D-10 D-9');
  assert.equal(keys(sortCards([a, b, c], cmps, 'rank', -1)), 'D-9 D-10 D-2');
});

test('a column sorts both ways; keys by number, points most first, unestimated last', () => {
  assert.equal(keys(sortCards([a, b, c], cmps, 'key', 1)), 'D-2 D-9 D-10');
  assert.equal(keys(sortCards([a, b, c], cmps, 'points', 1)), 'D-2 D-9 D-10');
  assert.equal(keys(sortCards([a, b, c], cmps, 'status', -1)), 'D-2 D-10 D-9');
});

test('labels, reporter and custom fields sort A–Z, numbers by value, empty last', () => {
  const x = { Key: 'X-1', Labels: 'ui', Reporter: '', Extra: 'Team=b\u001fSize=10' };
  const y = { Key: 'X-2', Labels: '', Reporter: 'Ann', Extra: 'Size=9' };
  const z = { Key: 'X-3', Labels: 'api', Reporter: 'Bob', Extra: 'Team=a' };
  assert.equal(keys(sortCards([x, y, z], cmps, 'labels', 1)), 'X-3 X-1 X-2');
  assert.equal(keys(sortCards([x, y, z], cmps, 'reporter', 1)), 'X-2 X-3 X-1');
  assert.equal(keys(sortCards([x, y, z], cmps, 'x:Team', 1)), 'X-3 X-1 X-2');
  assert.equal(keys(sortCards([x, y, z], cmps, 'x:size', 1)), 'X-2 X-1 X-3');
  assert.equal(keys(sortCards([x, y, z], cmps, 'x:Size', -1)), 'X-3 X-1 X-2');
});

test('age sorts stalest first, done last', () => {
  const Z = '0001-01-01T00:00:00Z';
  const old = { Key: 'A-1', Since: '2026-01-01T00:00:00Z', Created: '2026-03-01T00:00:00Z' };
  const fresh = { Key: 'A-2', Since: Z, Created: '2026-02-01T00:00:00Z' };
  const done = { Key: 'A-3', Done: true, Since: '2025-01-01T00:00:00Z', Created: '2025-01-01T00:00:00Z' };
  assert.equal(keys(sortCards([done, fresh, old], cmps, 'age', 1)), 'A-1 A-2 A-3');
});
