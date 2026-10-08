import { test } from 'node:test';
import assert from 'node:assert/strict';
import { postComment } from '../static/js/lib/comment.js';

// fake answers each post with the next of answers: an object, or an error status.
const fake = answers => {
  const sent = [];
  return { sent, post: async (path, body) => { sent.push(body); const a = answers.shift(); if (typeof a === 'number') throw Object.assign(new Error('timed out'), { status: a }); return a; } };
};

test('a post that timed out checks before it posts again', async () => {
  const api = fake([504, { Found: true }]);
  await assert.rejects(postComment(api, 'ABC-1', { Markdown: 'hi' }), /may have posted: timed out/);
  assert.equal(await postComment(api, 'ABC-1', { Markdown: 'hi' }), true);
  assert.deepEqual(api.sent, [{ Markdown: 'hi' }, { Markdown: 'hi', Check: true }]);
});

test('other text, another issue or another failure posts without a check', async () => {
  const api = fake([504, null, 502, null]);
  await assert.rejects(postComment(api, 'ABC-2', { Markdown: 'hi' }));
  assert.equal(await postComment(api, 'ABC-2', { Markdown: 'hi there' }), false);
  await assert.rejects(postComment(api, 'ABC-3', { Markdown: 'x' }), /^Error: timed out$/);
  assert.equal(await postComment(api, 'ABC-3', { Markdown: 'x' }), false);
  assert.ok(api.sent.every(b => !b.Check));
});
