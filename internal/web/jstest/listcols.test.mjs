import { test } from 'node:test';
import assert from 'node:assert/strict';
import { fixCols, moveCol, pickOrder } from '../static/js/lib/listcols.js';

const cols = ['mark', 'key', 'summary', 'status', 'points'];

test('a column moves before or after another; the mark stays first', () => {
  assert.deepEqual(moveCol(cols, 'status', 'key', false), ['mark', 'status', 'key', 'summary', 'points']);
  assert.deepEqual(moveCol(cols, 'key', 'points', true), ['mark', 'summary', 'status', 'points', 'key']);
  assert.deepEqual(moveCol(cols, 'status', 'mark', false), cols);
  assert.deepEqual(moveCol(cols, 'mark', 'points', true), cols);
});

test('a stored order keeps; the key and summary come back when missing', () => {
  assert.deepEqual(fixCols(['status', 'summary', 'mark']), ['mark', 'key', 'status', 'summary']);
});

test('the picker keeps the order, new columns after', () => {
  const all = ['mark', 'key', 'summary', 'status', 'priority', 'points'];
  assert.deepEqual(pickOrder(all, ['mark', 'points', 'key', 'summary'], ['points', 'priority']), ['mark', 'points', 'key', 'summary', 'priority']);
});
