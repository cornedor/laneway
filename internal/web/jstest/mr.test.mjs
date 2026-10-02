import { test } from 'node:test';
import assert from 'node:assert/strict';
import { issueKeys, stageStatus } from '../static/js/lib/mr.js';

test('the Jira keys a merge request names: title, branch, description, the session\'s projects only', () => {
  const m = { Title: 'DEMO-7: cache rates', SourceBranch: 'issue/demo-7-cache', Description: 'Also DEMO-9, and UTF-8 everywhere.' };
  assert.deepEqual(issueKeys(m, ['DEMO']), ['DEMO-7', 'DEMO-9']);
  assert.deepEqual(issueKeys(m, []), ['DEMO-7', 'DEMO-9', 'UTF-8']);
  assert.deepEqual(issueKeys({ Title: 'No key' }, ['DEMO']), []);
});

test('a stage shows as its worst job', () => {
  assert.equal(stageStatus({ Jobs: [{ Status: 'success' }, { Status: 'warning' }] }), 'warning');
  assert.equal(stageStatus({ Jobs: [{ Status: 'success' }, { Status: 'failed' }, { Status: 'running' }] }), 'failed');
  assert.equal(stageStatus({ Jobs: [{ Status: 'success' }] }), 'success');
  assert.equal(stageStatus({ Jobs: [] }), 'skipped');
});
