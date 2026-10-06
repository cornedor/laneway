import { test } from 'node:test';
import assert from 'node:assert/strict';
import { highlight, lines, enter, indent, toggleTask, pasteLink, inFence, tableTab, tableArrow, moveLines, wrapWith, backspace } from '../static/js/lib/mdhl.js';

// The highlight's text is the source: tags stripped and entities read back give it again.
const text = html => html.replace(/<[^>]+>/g, '').replace(/&lt;/g, '<').replace(/&gt;/g, '>').replace(/&quot;/g, '"').replace(/&amp;/g, '&');
const apply = (v, e) => v.slice(0, e.from) + e.text + v.slice(e.to);

test('highlight keeps every character', () => {
  const src = [
    '# Title **bold**', '', 'Some *it* and _it_ and ~~gone~~ and `co*de*` <u>u</u> & <b>', 'See [docs](https://x.io/a_b) or https://y.io/p, ABC-12.',
    '- [ ] open', '  - [x] done **b**', '1. one', '> quoted *x*', '```js', 'const a = **b**;', '```', '---',
    '| A | B |', '| --- | :-: |', '| 1 | 2 \\| 3 |', '<!-- panel:info -->', 'x', '<!-- /panel -->', '<!-- keep:3 -->',
    '<status color="green">DONE</status> <date>2026-10-01</date> <span style="color:#bf2600">red</span> :smile: @Ann Lee hi', '<> decided',
    '<!-- keep:1 mediaSingle: move or delete this line -->', '<!-- block:2 columns -->', 'c', '<!-- /block -->', '<!-- table:3 numbered -->',
    '| <!-- bg:#ffeeaa --> x | <!-- th --> y |', '| - | - |', '<!-- card: https://x.io/c -->', 'hi ⟦4 @Ada Lovelace⟧ there', '',
  ].join('\n');
  assert.equal(text(highlight(src, { names: ['Ann Lee'] })), src);
});

test('highlight roles', () => {
  const h = s => highlight(s, { names: ['Ann Lee'] });
  assert.deepEqual(lines('## Hi')[0], { c: 'h h2', h: '<span class="mk">## </span>Hi', s: '', g: 0 });
  assert.match(h('a **b** c'), /<span class="mk">\*\*<\/span><span class="hl-b">b<\/span><span class="mk">\*\*<\/span>/);
  assert.match(h('***x***'), /^<span class="mk">\*\*\*<\/span><span class="hl-b hl-i">x<\/span><span class="mk">\*\*\*<\/span>$/);
  assert.match(h('<span style="color:#0747a6">x</span>'), /class="md-col" style="--c:#0747a6">x/);
  assert.match(h('`**x**`'), /hl-code"><span class="mk">`<\/span>\*\*x\*\*/);
  assert.match(h('snake_case_word'), /^snake_case_word$/);
  assert.match(h('- [x] done'), /hl-box on">\[x\]<\/span> <span class="hl-done">done/);
  assert.match(h('[a](https://x.io)'), /hl-lt" data-href="https:\/\/x.io"/);
  assert.match(h('[a](javascript:x)'), /<span class="hl-lt">a/);
  assert.match(highlight(':smile:', { emoji: n => (n === 'smile' ? '😄' : '') }), /data-g="😄"/);
  assert.match(highlight('![a](attachment:7)', { img: u => '/api/attachments/' + u.slice(11) }), /<img class="hl-img" src="\/api\/attachments\/7"/);
  assert.doesNotMatch(highlight('![a](javascript:x)'), /<img/);
  assert.deepEqual(lines('```\n# not\n```')[1], { c: 'pre', h: '# not', s: '', g: 0 });
  assert.match(h('| A | B |\n| - | - |'), /hl-th"> A /);
  assert.match(h('hi @Ann Lee'), /hl-at">@Ann Lee/);
  assert.match(h('ABC-12'), /hl-key" data-key="ABC-12">ABC-12/);
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

test('lines: block classes and groups', () => {
  const ls = lines(['- a', '  - [x] b', '| A | B |', '| - | - |', '| 1 | 2 |', '<!-- panel:warning -->', 'w', '<!-- /panel -->', '```js', 'x', '```', '---', '> q'].join('\n'));
  assert.deepEqual(ls.map(l => l.c), ['li', 'li task done', 'tr th', 'tr dl', 'tr', 'tag pno pn pn-warning', ' pn pn-warning', 'tag pnc pn pn-warning', 'fc fo', 'pre', 'fc fx', 'hr', 'q']);
  assert.deepEqual(ls.map(l => l.g), [0, 1, 2, 2, 2, 5, 6, 7, 8, 8, 8, 11, 12]);
  assert.equal(ls[1].s, '--h:8ch');
  assert.match(ls[2].h, /^<span class="td">/);
});

test('lines: placeholders desc.go writes', () => {
  const ls = lines(['<!-- keep:1 mediaSingle: move or delete this line -->', '<!-- block:2 columns -->', 'c', '<!-- /block -->', '<!-- table:3 numbered -->', '<!-- card: https://x.io/c -->'].join('\n'));
  assert.deepEqual(ls.map(l => l.c), ['keep', 'tag blo', ' ex', 'tag pnc ex', 'tag shell', 'card']);
  assert.match(ls[0].h, /data-l="image"/);
  assert.match(ls[5].h, /hl-card" data-href="https:\/\/x.io\/c"/);
  assert.match(lines('<!-- keep:1 table with a mention: move or delete this line -->')[0].h, /data-l="table with a mention"/);
  assert.match(highlight('a ⟦4 @Ada Lovelace⟧ b'), /<span class="mk">⟦4 <\/span><span class="hl-kept">@Ada Lovelace<\/span><span class="mk">⟧<\/span>/);
  assert.match(highlight('| <!-- bg:#ffeeaa --> x |'), /hl-sw" style="--c:#ffeeaa"/);
});

test('enter renumbers the list after it, opens a table row', () => {
  let v = '1. a\n2. b\n3. c';
  let e = enter(v, 4, 4);
  assert.equal(apply(v, e), '1. a\n2. \n3. b\n4. c');
  assert.equal(e.a, 8);
  v = '| a | b |';
  e = enter(v, 9, 9);
  assert.equal(apply(v, e), '| a | b |\n| --- | --- |\n|  |  |'); // a header gets its delimiter row
  assert.equal(e.a, 26);
  v = '| a | b |\n| --- | --- |\n| 1 | 2 |';
  e = enter(v, 9, 9);
  assert.equal(apply(v, e), '| a | b |\n| --- | --- |\n|  |  |\n| 1 | 2 |'); // past the delimiter
  assert.equal(e.a, 26);
  e = enter(v, v.length, v.length);
  assert.equal(apply(v, e), v + '\n|  |  |');
  assert.equal(e.a, v.length + 3);
  v = '| a |\n|  |';
  assert.equal(apply(v, enter(v, 10, 10)), '| a |\n');
});

test('tab steps through table cells', () => {
  const v = '| a | bb |\n| - | - |\n| c | d |';
  assert.deepEqual(tableTab(v, 2, false), { from: 2, to: 2, text: '', a: 6, b: 8 });
  assert.deepEqual(tableTab(v, 7, false), { from: 7, to: 7, text: '', a: 23, b: 24 }); // over the delimiter row
  assert.deepEqual(tableTab(v, 23, true), { from: 23, to: 23, text: '', a: 6, b: 8 });
  assert.equal(apply(v, tableTab(v, 28, false)), v + '\n|  |  |');
  assert.equal(tableTab('text', 1, false), null);
});

test('alt+arrows move lines, markers wrap a selection', () => {
  let v = 'a\nb\nc';
  let e = moveLines(v, 2, 2, -1);
  assert.equal(apply(v, e), 'b\na\nc'); assert.equal(e.a, 0);
  e = moveLines(v, 2, 2, 1);
  assert.equal(apply(v, e), 'a\nc\nb'); assert.equal(e.a, 4);
  assert.equal(moveLines(v, 0, 0, -1), null);
  assert.equal(moveLines(v, 4, 4, 1), null);
  v = 'say hi';
  assert.deepEqual(wrapWith(v, 4, 6, '*'), { from: 4, to: 6, text: '*hi*', a: 5, b: 7 });
  assert.equal(apply(v, wrapWith(v, 4, 6, '~')), 'say ~~hi~~');
  assert.equal(apply(v, wrapWith(v, 4, 6, '[')), 'say [hi](url)');
  assert.equal(wrapWith(v, 4, 4, '*'), null);
  assert.equal(wrapWith(v, 4, 6, 'x'), null);
});

test('up and down in a table keep the column', () => {
  const v = 'x\n| a | bb |\n| - | - |\n| c | dd |\ny';
  assert.equal(tableArrow(v, 9, 1), 20); // bb → the delimiter row's second cell
  assert.equal(tableArrow(v, 30, -1), 20);
  assert.equal(tableArrow(v, 3, -1), 1); // out of the table, the same column
  assert.equal(tableArrow('a\nb', 0, 1), null);
});

test('backspace after a marker drops it, a nested item moves out', () => {
  assert.deepEqual(backspace('- a', 2, 2), { from: 0, to: 2, text: '', a: 0, b: 0 });
  assert.equal(apply('- a\n  - b', backspace('- a\n  - b', 8, 8)), '- a\n- b');
  assert.equal(apply('## h', backspace('## h', 3, 3)), 'h');
  assert.equal(apply('> q', backspace('> q', 2, 2)), 'q');
  assert.equal(backspace('- ab', 3, 3), null);
  assert.equal(backspace('```\n- a', 6, 6), null);
});
