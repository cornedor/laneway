import { test } from 'node:test';
import assert from 'node:assert/strict';

globalThis.LANEWAY_I18N = { '': 'nl', 'Open issue': 'Issue openen', '%d issues': '%d issues (nl)', '%s of %s': '%s van %s' };
const { T, Tn, lang } = await import('../static/js/lib/i18n.js');

test('T translates, formats and falls back to the English', () => {
  assert.equal(T('Open issue'), 'Issue openen');
  assert.equal(T('%s of %s', 'a', 'b'), 'a van b');
  assert.equal(T('untranslated %d%%', 5), 'untranslated 5%');
  assert.equal(T('100%'), '100%');
  assert.equal(Tn(2, '%d issue', '%d issues', 2), '2 issues (nl)');
  assert.equal(Tn(1, '%d issue', '%d issues', 1), '1 issue');
  assert.equal(lang(), 'nl');
});
