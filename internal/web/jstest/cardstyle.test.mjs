import { test } from 'node:test';
import assert from 'node:assert/strict';
import { defaultLayout, layoutOf, normLayout, lookOf, normStyle, styleValue, layoutValue } from '../static/js/lib/cardstyle.js';

test('without ui.card_layout the card keeps card_fields in the usual places', () => {
  assert.deepEqual(defaultLayout(null, ['Team']), { top: ['type', 'key', 'flagged'], top_right: ['priority', 'points'], bottom: ['parent', 'subtasks', 'due', 'pr', 'deploy', 'Team', 'labels'], bottom_right: ['age', 'avatar'] });
  assert.deepEqual(defaultLayout(['points', 'assignee']).bottom_right, ['avatar'], 'assignee brings the avatar');
  assert.deepEqual(layoutOf({ CardFields: ['due'], CardLayout: { Top: [], TopRight: null } }).bottom, ['due', 'labels'], 'an empty layout is unset');
});

test('a layout keeps known fields once, custom ones by their configured name, and the key', () => {
  const l = normLayout({ top: ['Status', 'nope'], top_right: ['status', 'points'], bottom: ['team'] }, ['Team']);
  assert.deepEqual(l, { top: ['key', 'status'], top_right: ['points'], bottom: ['Team'], bottom_right: [] });
  assert.deepEqual(layoutOf({ CardLayout: { TopRight: ['age'] } }).top_right, ['age'], 'Go JSON names');
  assert.deepEqual(layoutValue(l), { top: ['key', 'status'], top_right: ['points'], bottom: ['Team'] });
});

test('styles: a later colour wins, show waits for its match, hide wins over show', () => {
  const look = lookOf([
    { When: 'prio>=high', Edge: 'err', Bold: true },
    { when: 'label:ui', edge: '#00ff00', tint: 'warn', hide: ['labels'] },
    { when: 'due<7d', show: ['due', 'labels'] },
    { when: '', edge: 'err' },
  ]);
  const soon = new Date(Date.now() + 2 * 864e5).toISOString();
  const a = look({ Key: 'A-1', Priority: 'High', Labels: 'ui', Due: soon });
  assert.equal(a.edge, '#00ff00'); assert.equal(a.tint, 'warn'); assert.ok(a.bold && !a.fade);
  assert.deepEqual([...a.hidden], ['labels']);
  const b = look({ Key: 'A-2', Priority: 'Low' });
  assert.equal(b.edge, ''); assert.deepEqual([...b.hidden].sort(), ['due', 'labels']);
  assert.equal(lookOf([{ when: ' ' }]), null, 'no usable style: nothing to do');
});

test('a style keeps only what it can use', () => {
  assert.deepEqual(normStyle({ When: 'x', Edge: 'pink', Tint: 'OK', Hide: ['due', 'nope'] }), { when: 'x', edge: '', tint: 'ok', fade: false, bold: false, hide: ['due'], show: [] });
  assert.deepEqual(styleValue(normStyle({ when: ' is:flagged ', fade: true })), { when: 'is:flagged', fade: true });
});
