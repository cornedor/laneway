import { test } from 'node:test';
import assert from 'node:assert/strict';
import { split, join, withQuery, sameBut, target, tidy } from '../static/js/lib/nav.js';

const nameOf = p => (p.match(/^\/(\w+)/) || [])[1] || '';

test('split and join round trip', () => {
  assert.deepEqual(split('#/board/A/1?issue=A-1&sprint=2'), { path: '/board/A/1', query: { issue: 'A-1', sprint: '2' } });
  assert.deepEqual(split(''), { path: '', query: {} });
  assert.equal(join('/mr', { url: 'https://g/x?y=1', tab: '' }), '#/mr?url=https%3A%2F%2Fg%2Fx%3Fy%3D1');
  assert.equal(withQuery('#/board?issue=A-1&sprint=2', { issue: null }), '#/board?sprint=2');
});

test('sameBut ignores the keys named and the order', () => {
  assert.ok(sameBut({ a: '1', issue: 'X' }, { a: '1' }, 'issue'));
  assert.ok(sameBut({ a: '1', b: '2' }, { b: '2', a: '1' }));
  assert.ok(!sameBut({ a: '1' }, { a: '2' }, 'issue'));
});

test('going to the route shown is no step, unless it shows a view of its own', () => {
  assert.equal(target('#/board/A/1', '/board', { nameOf }), null);
  assert.equal(target('#/board/A/1?issue=A-2', 'board', { nameOf, panel: 'A-2' }), null);
  assert.equal(target('#/board/A/1?sprint=jql%3Ax', '/board', { nameOf }), '#/board');
  assert.equal(target('#/work?tab=day', '/work?tab=day', { nameOf }), null);
  assert.equal(target('#/board/A/1', '/board/A/2', { nameOf }), '#/board/A/2');
});

test('the open panel comes along, not onto the issue page', () => {
  assert.equal(target('#/board/A/1?issue=A-2', '/inbox', { nameOf, panel: 'A-2' }), '#/inbox?issue=A-2');
  assert.equal(target('#/board?issue=A-2', '/issue/A-3', { nameOf, panel: 'A-2' }), '#/issue/A-3');
  assert.equal(target('#/inbox', '#/board?issue=B-1', { nameOf, panel: 'A-2' }), '#/board?issue=B-1');
});

test('tidy: the start route for none, keys in capitals, else as it was', () => {
  assert.equal(tidy('', '/board'), '#/board');
  assert.equal(tidy('#?issue=a-1', '/home'), '#/home?issue=A-1');
  assert.equal(tidy('#/board/DEMO/1?issue=demo-12', '/board'), '#/board/DEMO/1?issue=DEMO-12');
  assert.equal(tidy('#/issue/demo-3', '/board'), '#/issue/DEMO-3');
  const mr = '#/mr?url=' + encodeURIComponent('https://g/a b');
  assert.equal(tidy(mr, '/board'), mr);
});
