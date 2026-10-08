import { test } from 'node:test';
import assert from 'node:assert/strict';
import { fromADF, toADF, schema, isEmpty } from '../static/js/lib/adf.js';

const doc = (...content) => ({ type: 'doc', version: 1, content });
const p = (...content) => ({ type: 'paragraph', content });
const t = (text, ...marks) => (marks.length ? { type: 'text', text, marks } : { type: 'text', text });
const round = d => toADF(fromADF(d));

test('a document Jira wrote comes back as it was', () => {
  const d = doc(
    { type: 'heading', attrs: { level: 2 }, content: [t('Title')] },
    p(t('plain '), t('bold', { type: 'strong' }), t(' and '), t('a link', { type: 'link', attrs: { href: 'https://x.test' } }), { type: 'hardBreak' }, t('next')),
    { type: 'bulletList', content: [{ type: 'listItem', content: [p(t('one')), { type: 'orderedList', attrs: { order: 3 }, content: [{ type: 'listItem', content: [p(t('nested'))] }] }] }] },
    { type: 'taskList', attrs: { localId: 'a' }, content: [{ type: 'taskItem', attrs: { localId: 'b', state: 'DONE' }, content: [t('done')] },
      { type: 'taskList', attrs: { localId: 'c' }, content: [{ type: 'taskItem', attrs: { localId: 'd', state: 'TODO' }, content: [t('sub')] }] }] },
    { type: 'panel', attrs: { panelType: 'warning' }, content: [p(t('careful'))] },
    { type: 'codeBlock', attrs: { language: 'go' }, content: [t('x := 1\n')] },
    { type: 'expand', attrs: { title: 'More' }, content: [p(t('inside'))] },
    { type: 'rule' },
    p({ type: 'mention', attrs: { id: 'abc', text: '@Ann', accessLevel: '' } }, t(' '), { type: 'emoji', attrs: { shortName: ':smile:', id: '1f604', text: '😄' } },
      { type: 'status', attrs: { text: 'DONE', color: 'green', localId: 's' } }, { type: 'date', attrs: { timestamp: '1700000000000' } }, { type: 'inlineCard', attrs: { url: 'https://x.test/browse/A-1' } }),
    p(t('colour', { type: 'textColor', attrs: { color: '#ff5630' } }), t('sub', { type: 'subsup', attrs: { type: 'sub' } }), t('code', { type: 'code' })),
  );
  assert.deepEqual(round(d), d);
});

test('an image keeps its size and place', () => {
  const d = doc({ type: 'mediaSingle', attrs: { layout: 'wrap-left', width: 320, widthType: 'pixel' }, content: [
    { type: 'media', attrs: { id: 'a1b2', type: 'file', collection: '', alt: 'shot.png', width: 1200, height: 800 } },
    { type: 'caption', content: [t('the caption')] }] },
  { type: 'mediaGroup', content: [{ type: 'media', attrs: { id: 'f1', type: 'file', collection: 'c' } }] });
  assert.deepEqual(round(d), d);
});

test('a table keeps its widths, spans and colours', () => {
  const cell = (type, attrs, text) => ({ type, attrs, content: [p(t(text))] });
  const d = doc({ type: 'table', attrs: { isNumberColumnEnabled: false, layout: 'default', localId: 'x', width: 760 }, content: [
    { type: 'tableRow', content: [cell('tableHeader', { colspan: 1, rowspan: 1, colwidth: [200] }, 'h1'), cell('tableHeader', { colspan: 1, rowspan: 1, colwidth: [560] }, 'h2')] },
    { type: 'tableRow', content: [cell('tableCell', { colspan: 2, rowspan: 1, background: '#deebff' }, 'wide')] }] });
  assert.deepEqual(round(d), d);
});

test('what the editor does not know is kept whole', () => {
  const ext = { type: 'bodiedExtension', attrs: { extensionKey: 'toc', extensionType: 'com.atlassian' }, content: [p(t('x'))] };
  const odd = { type: 'futureNode', attrs: { q: 1 }, content: [t('?')] };
  const wrongPlace = { type: 'listItem', content: [p(t('stray'))] };
  const d = doc(ext, odd, wrongPlace, p(t('a'), { type: 'inlineExtension', attrs: { extensionKey: 'k' } }, t('b', { type: 'fancyMark', attrs: { z: 2 } })));
  assert.deepEqual(round(d), d);
  const pm = fromADF(d);
  assert.equal(pm.child(0).type.name, 'unknownBlock');
  assert.equal(pm.child(2).type.name, 'unknownBlock');
});

test('attributes the schema does not name ride along', () => {
  const d = doc({ type: 'paragraph', attrs: { localId: 'p1', futureAttr: 'y' }, content: [t('x')], marks: [{ type: 'alignment', attrs: { align: 'center' } }] });
  assert.deepEqual(round(d), d);
});

test('a block holding what it may not is kept, not broken', () => {
  const d = doc({ type: 'codeBlock', content: [t('bold?', { type: 'strong' })] }, { type: 'heading', attrs: { level: 1 }, content: [{ type: 'mediaSingle', content: [] }] });
  assert.deepEqual(round(d), d);
});

test('empty documents and containers', () => {
  assert.ok(isEmpty(fromADF(null)));
  assert.deepEqual(toADF(fromADF({ type: 'doc', version: 1, content: [] })), doc());
  // A list item Jira left empty gets the paragraph it needs.
  const pm = fromADF(doc({ type: 'bulletList', content: [{ type: 'listItem', content: [] }] }));
  assert.equal(pm.firstChild.firstChild.firstChild.type.name, 'paragraph');
  // An empty text run is dropped.
  assert.deepEqual(round(doc(p(t('a'), { type: 'text', text: '' }))), doc(p(t('a'))));
});

test('a new paragraph writes no attrs', () => {
  const pm = schema.nodes.doc.create(null, [schema.nodes.paragraph.create(null, schema.text('hi'))]);
  assert.deepEqual(toADF(pm), doc(p(t('hi'))));
});
