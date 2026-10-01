import { test } from 'node:test';
import assert from 'node:assert/strict';
import { highlight, enter, indent, toggleTask, pasteLink, inFence } from '../static/js/lib/mdhl.js';

// The highlight's text is the source: tags stripped and entities read back give it again.
const text = html => html.replace(/<[^>]+>/g, '').replace(/&lt;/g, '<').replace(/&gt;/g, '>').replace(/&quot;/g, '"').replace(/&amp;/g, '&');
const apply = (v, e) => v.slice(0, e.from) + e.text + v.slice(e.to);

test('highlight keeps every character', () => {
  const src = [
    '# Title **bold**', '', 'Some *it* and _it_ and ~~gone~~ and `co*de*` <u>u</u> & <b>', 'See [docs](https://x.io/a_b) or https://y.io/p, ABC-12.',
    '- [ ] open', '  - [x] done **b**', '1. one', '> quoted *x*', '```js', 'const a = **b**;', '```', '---',
    '| A | B |', '| --- | :-: |', '| 1 | 2 \\| 3 |', '<!-- panel:info -->', 'x', '<!-- /panel -->', '<!-- keep:3 -->',
    '<status color="green">DONE</status> <date>2026-10-01</date> <span style="color:#bf2600">red</span> :smile: @Ann Lee hi', '<> decided', '',
  ].join('\n');
  assert.equal(text(highlight(src, { names: ['Ann Lee'] })), src);
});

test('highlight roles', () => {
  const h = s => highlight(s, { names: ['Ann Lee'] });
  assert.match(h('## Hi'), /class="hl-h hl-h2"><span class="mk">## <\/span>Hi/);
  assert.match(h('a **b** c'), /<span class="mk">\*\*<\/span><span class="hl-b">b<\/span><span class="mk">\*\*<\/span>/);
  assert.match(h('`**x**`'), /hl-code"><span class="mk">`<\/span>\*\*x\*\*/);
  assert.match(h('snake_case_word'), /^snake_case_word$/);
  assert.match(h('- [x] done'), /hl-box on">\[x\]<\/span> <span class="hl-done">done/);
  assert.match(h('```\n# not\n```'), /hl-pre"># not/);
  assert.match(h('| A | B |\n| - | - |'), /hl-th"> A /);
  assert.match(h('hi @Ann Lee'), /hl-at">@Ann Lee/);
  assert.match(h('ABC-12'), /hl-key">ABC-12/);
  assert.doesNotMatch(h('xABC-12'), /hl-key/);
  assert.match(h('<span style="color:red;x">y</span>'), /^&lt;span/);
});

test('enter continues lists and quotes, ends an empty item', () => {
  let v = '- a';
  assert.equal(apply(v, enter(v, 3, 3)), '- a\n- ');
  v = '  3. a';
  assert.equal(apply(v, enter(v, 6, 6)), '  3. a\n  4. ');
  v = '- [x] a';
  assert.deepEqual(enter(v, 7, 7), { from: 7, to: 7, text: '\n- [ ] ', a: 14, b: 14 });
  v = 'x\n- ';
  assert.deepEqual(enter(v, 4, 4), { from: 2, to: 4, text: '', a: 2, b: 2 });
  v = '> q';
  assert.equal(apply(v, enter(v, 3, 3)), '> q\n> ');
  v = '```\n  a';
  assert.equal(apply(v, enter(v, 7, 7)), '```\n  a\n  ');
  assert.ok(inFence('```\na', 4));
  v = '-a';
  assert.equal(apply(v, enter(v, 2, 2)), '-a\n');
});

test('indent nests under the item above, outdent goes back', () => {
  let v = '- a\n- b';
  let e = indent(v, 7, 7, false);
  assert.equal(apply(v, e), '- a\n  - b');
  assert.equal(e.a, 9);
  v = apply(v, e);
  assert.equal(apply(v, indent(v, 9, 9, true)), '- a\n- b');
  v = '1. a\n2. b';
  assert.equal(apply(v, indent(v, 9, 9, false)), '1. a\n   2. b');
  assert.equal(indent('text', 0, 0, false), null);
  v = '- a';
  assert.equal(apply(v, indent(v, 3, 3, false)), '- a');
});

test('task boxes and pasted links', () => {
  assert.equal(apply('- [ ] a', toggleTask('- [ ] a', 2)), '- [x] a');
  assert.equal(apply('- [x] a', toggleTask('- [x] a', 2)), '- [ ] a');
  assert.equal(toggleTask('- a', 2), null);
  assert.equal(apply('see docs', pasteLink('see docs', 4, 8, 'https://x.io')), 'see [docs](https://x.io)');
  assert.equal(pasteLink('see docs', 4, 4, 'https://x.io'), null);
  assert.equal(pasteLink('see docs', 4, 8, 'not a url'), null);
});
