import { test } from 'node:test';
import assert from 'node:assert/strict';
import { htmlToMd, rich } from '../static/js/lib/html2md.js';

// A DOM-shaped tree: el('p', {attrs}, ...children), text as strings.
const el = (name, attrs, ...kids) => {
  const n = { nodeType: 1, nodeName: name.toUpperCase(), attrs: attrs || {}, getAttribute: a => (a in n.attrs ? n.attrs[a] : null), hasAttribute: a => a in n.attrs };
  n.childNodes = kids.map(k => (typeof k === 'string' ? { nodeType: 3, data: k } : k));
  return n;
};
const body = (...k) => el('body', null, ...k);

test('inline formatting', () => {
  assert.equal(htmlToMd(body(el('p', null, 'a ', el('b', null, 'bold '), el('em', null, 'it'), ' ', el('code', null, 'x`y'), ' ', el('a', { href: 'https://x.io' }, 'link'), ' 2*3'))),
    'a **bold** *it* ``x`y`` [link](https://x.io) 2\\*3');
  assert.equal(htmlToMd(body(el('a', { href: 'javascript:alert(1)' }, 'bad'))), 'bad');
  assert.equal(htmlToMd(body(el('a', { href: 'https://x.io' }, 'https://x.io'))), 'https://x.io');
  // Google Docs: a b that is not bold around spans that are
  assert.equal(htmlToMd(body(el('b', { style: 'font-weight:normal;' }, el('span', { style: 'font-weight:700' }, 'B'), el('span', { style: 'font-style:italic' }, 'I')))), '**B***I*');
});

test('blocks', () => {
  const md = htmlToMd(body(
    el('h2', null, 'Title'), '\n',
    el('ul', null, '\n', el('li', null, 'one'), el('li', null, el('input', { type: 'checkbox', checked: '' }), ' done', el('ul', null, el('li', null, 'deep')))),
    el('ol', { start: '3' }, el('li', null, 'three'), el('li', null, 'four')),
    el('blockquote', null, el('p', null, 'q1'), el('p', null, 'q2')),
    el('pre', null, el('code', { class: 'language-go' }, 'if x {\n\treturn\n}\n')),
    el('table', null, el('thead', null, el('tr', null, el('th', null, 'A'), el('th', null, 'B'))), el('tbody', null, el('tr', null, el('td', null, '1|2'), el('td', null, el('p', null, 'x'), el('p', null, 'y'))))),
    el('hr'), el('img', { src: 'https://x.io/a.png', alt: 'pic' }),
  ));
  assert.equal(md, [
    '## Title', '', '- one', '- [x] done', '  - deep', '', '3. three', '4. four', '', '> q1', '>', '> q2', '',
    '```go', 'if x {', '\treturn', '}', '```', '', '| A | B |', '| --- | --- |', '| 1\\|2 | x y |', '', '---', '', '![pic](https://x.io/a.png)',
  ].join('\n'));
});

test('rich tells formatting from a code editor copy', () => {
  assert.ok(rich('<p>a</p>'));
  assert.ok(rich('<meta charset="utf-8"><b style="font-weight:normal">x</b>'));
  assert.ok(!rich('<div style="white-space: pre;"><div><span style="color:#fff">const</span> a</div><br></div>'));
});
