import { test } from 'node:test';
import assert from 'node:assert/strict';

// keys.js listens on the document when loaded.
globalThis.document = { addEventListener() {} };
globalThis.KeyboardEvent = class { constructor(type, o) { Object.assign(this, { type }, o); } };
const { keys } = await import('../static/js/lib/keys.js');
const { barHints } = await import('../static/js/lib/keybar.js');

const shown = () => barHints(keys.active()).map(x => x.keys.map(k => k.spec).join(' ') + ' ' + x.label);

test('the view\'s labelled keys, then the global ones; aliases show the first, a shared label is one hint', () => {
  const g = keys.scope('global'), v = keys.scope('board');
  g.bind('?', () => {}, 'show keys', { bar: 'keys' });
  g.bind('/', () => {}, 'search', { bar: 'search' });
  v.bind('/', () => {}, 'filter', { bar: 'filter' });
  v.bind(['Enter', 'o'], () => {}, 'open', { bar: 'open' });
  v.bind('H', () => {}, 'left', { bar: 'move' });
  v.bind('L', () => {}, 'right', { bar: 'move' });
  v.bind('r', () => {}, 'refresh');
  assert.deepEqual(shown(), ['/ filter', 'Enter open', 'H L move', '? keys']);
  v.dispose(); g.dispose();
});

test('a panel waiting for focus has no hints; remapped keys show as bound; a click presses', () => {
  let focused = false, pressed = '';
  const v = keys.scope('board'), p = keys.scope('issue', { covers: () => focused });
  v.bind('s', () => {}, 'status', { bar: 'status' });
  p.bind('c', e => { pressed = e.key; }, 'comment', { bar: 'comment', help: true, when: () => focused });
  assert.deepEqual(shown(), ['s status']);
  focused = true;
  assert.deepEqual(shown(), ['c comment']);
  keys.configure({ web: { 'issue:c': 'C' } });
  const [h] = barHints(keys.active());
  assert.equal(h.keys[0].spec, 'C');
  h.keys[0].run();
  assert.equal(pressed, 'C');
  keys.configure({ web: {} });
  p.dispose(); v.dispose();
});

test('none unbinds: in ui.web_keys by id, in ui.keys by action; the key does nothing, the palette keeps the action', () => {
  const v = keys.scope('board');
  v.bind('s', () => {}, 'status', { bar: 'status' });
  v.bind('C', () => {}, 'list columns', { bar: 'columns' });
  keys.configure({ web: { 'board:C': 'none' }, conf: { status: ['none'] } });
  assert.deepEqual(shown(), []);
  assert.deepEqual(keys.registry().filter(r => r.scope === 'board' && ['s', 'C'].includes(r.def)).map(r => [r.def, r.specs, r.from]),
    [['s', [], 'ui.keys'], ['C', [], 'ui.web_keys']]);
  assert.deepEqual(keys.screen().filter(b => ['board:s', 'board:C'].includes(b.id)).map(b => [b.id, b.spec]), [['board:s', ''], ['board:C', '']]); // still in the palette
  keys.configure({ web: {}, conf: {} });
  assert.deepEqual(shown(), ['s status', 'C columns']);
  v.dispose();
});
