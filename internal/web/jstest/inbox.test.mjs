import { test } from 'node:test';
import assert from 'node:assert/strict';
import { everyMs, stateOf, unreadCount } from '../static/js/lib/inbox.js';

test('inbox_every as the TUI: off stops it, unset is five minutes', () => {
  assert.equal(everyMs('2m'), 120000);
  assert.equal(everyMs('off'), 0);
  assert.equal(everyMs(''), 300000);
  assert.equal(everyMs('1s'), 5000);
});

test('marks are per site/key', () => {
  const t = id => ({ ID: id, Key: 'A-1', Entries: [{ When: '2026-10-01T10:00:00Z' }] });
  const at = Date.parse('2026-10-01T10:00:00Z');
  const data = { threads: [t('one/A-1'), t('two/A-1')], marks: { 'one/A-1': { Read: at } }, floor: 0 };
  assert.equal(unreadCount(data, at + 1), 1);
  assert.equal(stateOf(data.threads[1], { Snooze: at + 10 }, 0, at).snoozed, true);
});
