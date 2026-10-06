import { test } from 'node:test';
import assert from 'node:assert/strict';
import { jqlContext, jqlComplete, jqlWordsFor } from '../static/js/lib/jql.js';

const words = { Fields: ['status', 'assignee', '"Story Points"'], Functions: ['currentUser()', 'openSprints()'], Reserved: ['AND', 'OR', 'ORDER BY'] };
const texts = r => r.list.map(x => x.text);

test('context: a value after an operator or inside a list, else a field', () => {
  assert.deepEqual(jqlContext('status = In'), { field: 'status', prefix: 'In', start: 9, value: true });
  assert.deepEqual(jqlContext('status not in (Done, '), { field: 'status', prefix: '', start: 21, value: true });
  assert.equal(jqlContext('stat').value, false);
  assert.equal(jqlContext('assignee = "Ada Lo').prefix, 'Ada Lo');
});

test('complete quotes a word with a space and ends with one', () => {
  assert.equal(jqlComplete('status = In', 'In Progress'), 'status = "In Progress" ');
  assert.equal(jqlComplete('stat', 'status'), 'status ');
});

test('words: fields first by prefix, operators after a field, functions for a value', () => {
  assert.deepEqual(texts(jqlWordsFor(words, 'st')), ['status', '"Story Points"']);
  assert.deepEqual(texts(jqlWordsFor(words, 'status ')), ['=', '!=', '~', '!~', '>', '>=', '<', '<=', 'in', 'is', 'was', 'changed']);
  assert.deepEqual(texts(jqlWordsFor(words, 'status i')), ['in', 'is']);
  assert.deepEqual(texts(jqlWordsFor(words, '"Story Points" >')), ['>', '>=']);
  assert.deepEqual(texts(jqlWordsFor(words, 'status = Done ')), ['status', 'assignee', '"Story Points"', 'currentUser()', 'openSprints()', 'AND', 'OR', 'ORDER BY'], 'a value, not a field, before the space');
  const r = jqlWordsFor(words, 'assignee = cur');
  assert.equal(r.c.value, true);
  assert.deepEqual(texts(r), ['currentUser()']);
});
