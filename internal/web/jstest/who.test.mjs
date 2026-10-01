import { test } from 'node:test';
import assert from 'node:assert/strict';
import { passesWho, whoLabel, whoKey, pickWho } from '../static/js/lib/who.js';

const ada = { AssigneeID: 'a1', Assignee: 'Ada' }, bob = { AssigneeID: 'b2', Assignee: 'Bob' }, none = {};

test('a filter of several passes any of them', () => {
  assert.ok(passesWho(null, none));
  const w = new Set(['a1', '-']);
  assert.ok(passesWho(w, ada) && passesWho(w, none) && !passesWho(w, bob));
  assert.equal(whoKey(new Set(['b2', 'a1'])), 'a1,b2');
});

test('the label names two, then counts', () => {
  const name = id => ({ a1: 'Ada', b2: 'Bob', c3: 'Cy' })[id];
  assert.equal(whoLabel(new Set(['a1', '-']), name), 'Ada, Unassigned');
  assert.equal(whoLabel(new Set(['a1', 'b2', 'c3']), name), 'Ada +2');
});

test('pickWho: ticked people, Anyone clears, cancel keeps', async () => {
  const people = new Map([['b2', 'Bob'], ['a1', 'Ada']]);
  let seen;
  const app = r => ({ ui: { pick: o => { seen = o; return Promise.resolve(r(o.items)); } } });
  assert.deepEqual(await pickWho(app(it => [it[2], it[3]]), people, null), new Set(['a1', 'b2']));
  assert.equal(seen.multi && seen.enterPicks, true);
  await pickWho(app(() => null), people, new Set(['b2']));
  assert.deepEqual(seen.selected.map(i => i.id), ['b2']);
  assert.equal(await pickWho(app(it => [it[0]]), people, new Set(['b2'])), null);
  assert.equal(await pickWho(app(() => null), people, null), undefined);
});
