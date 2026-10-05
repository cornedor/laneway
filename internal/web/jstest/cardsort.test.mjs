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
