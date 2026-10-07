import { test } from 'node:test';
import assert from 'node:assert/strict';
import { makeLine, past, since, order, label } from '../static/js/views/report_lines.js';

const cols = [{ Name: 'To Do', StatusIDs: ['1'] }, { Name: 'In Progress', StatusIDs: ['2'] }, { Name: 'In Review', StatusIDs: ['3', '4'] }, { Name: 'Done', StatusIDs: ['5'] }];
const oct = d => `2026-10-0${d}T12:00:00Z`, at = d => +new Date(oct(d));
// In progress on the 2nd, review on the 3rd, back on the 4th, review (its other status) on the 5th, done on the 6th.
const is = { Status: '5', Moves: [['1', '2', 2], ['2', '3', 3], ['3', '2', 4], ['2', '4', 5], ['4', '5', 6]].map(([From, To, d]) => ({ From, To, When: oct(d) })) };

test('a line holds its column and those right of it, as jira.Line', () => {
  const live = makeLine(cols, 'in review', false), first = makeLine(cols, 'In Review', true);
  assert.equal(live.name, 'In Review');
  for (const [d, l, f] of [[1, false, false], [3, true, true], [4, false, true], [5, true, true], [7, true, true]]) {
    assert.equal(past(live, is, at(d)), l, 'live on the ' + d);
    assert.equal(past(first, is, at(d)), f, 'first on the ' + d);
  }
  assert.equal(since(live, is), at(5));
  assert.equal(since(first, is), at(3));
  assert.equal(since(live, { Status: '2', Moves: [{ From: '2', To: '3', When: oct(2) }, { From: '3', To: '2', When: oct(3) }] }), null);
  assert.equal(past(live, { Status: '99' }, at(1)), false);
});

test('no line is Jira\'s done, last in order', () => {
  assert.equal(makeLine(cols, '', false), null);
  assert.equal(makeLine(cols, 'Nope', false), null);
  assert.ok(past(null, { Resolved: oct(3) }, at(3)) && !past(null, { Resolved: oct(3) }, at(2)));
  assert.equal(since(null, { Resolved: '0001-01-01T00:00:00Z' }), null);
  const review = makeLine(cols, 'In Review'), done = makeLine(cols, 'Done');
  assert.deepEqual(order(null, review), [review, null]);
  assert.deepEqual(order(done, review), [review, done]);
  assert.equal(label(null), 'done');
});
