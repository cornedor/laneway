// Jira's document format (ADF) as a ProseMirror schema: ADF is ProseMirror's
// JSON (Atlassian's editor is ProseMirror), so its nodes and marks keep their
// ADF names and attributes and a document goes in and out unchanged. What the
// schema has no room for (a macro, a node or mark it does not know, a known one
// in a place it may not be) is kept whole: an atom holding its JSON, put back
// as it was. Attributes the schema does not name ride along in `extra`.
//
//   fromADF(json) → PM doc     toADF(doc) → json      schema, emptyDoc(), isEmpty(doc), localId()
//
// Pure (no DOM until a view draws it), tested with node (internal/web/jstest).
import { Schema, Fragment } from '../../vendor/prosemirror/prosemirror-model.js';

export const localId = () => (globalThis.crypto && crypto.randomUUID ? crypto.randomUUID() : 'l' + Date.now().toString(36) + Math.random().toString(36).slice(2, 10));

// Each spec's attrs, all optional (null), plus extra.
const A = (...names) => Object.fromEntries([...names, 'extra'].map(n => [n, { default: null }]));
const data = v => (v == null ? null : String(v));
const num = v => (v == null || v === '' || isNaN(+v) ? null : +v);

// What may sit where (ADF's own content rules, a little wider where Jira writes wider).
const LIST = 'bulletList | orderedList | taskList';
const LEAFY = 'paragraph | heading | ' + LIST + ' | decisionList | blockquote | codeBlock | rule | panel | mediaSingle | mediaGroup | blockCard | embedCard | unknownBlock';
const CELL = '(' + LEAFY + ' | nestedExpand)+';

const panelIcon = { info: 'ⓘ', note: '✎', success: '✓', warning: '⚠', error: '⨯', tip: '✦', custom: '' };

const nodes = {
  doc: { content: '(block | layoutSection)+', marks: '_', attrs: A() },
  text: { group: 'inline' },
  paragraph: { group: 'block', content: 'inline*', attrs: A('localId'), marks: '_',
    parseDOM: [{ tag: 'p' }], toDOM: () => ['p', 0] },
  heading: { group: 'block', content: 'inline*', defining: true, attrs: { level: { default: 1 }, ...A('localId') }, marks: '_',
    parseDOM: [1, 2, 3, 4, 5, 6].map(l => ({ tag: 'h' + l, attrs: { level: l } })), toDOM: n => ['h' + n.attrs.level, 0] },
  blockquote: { group: 'block', content: '(paragraph | bulletList | orderedList | codeBlock | mediaSingle | mediaGroup | unknownBlock)+', defining: true, marks: '_', attrs: A('localId'),
    parseDOM: [{ tag: 'blockquote' }], toDOM: () => ['blockquote', 0] },
  bulletList: { group: 'block', content: 'listItem+', marks: '_', attrs: A('localId'),
    parseDOM: [{ tag: 'ul', getAttrs: d => (d.hasAttribute('data-tasks') ? false : null) }], toDOM: () => ['ul', 0] },
  orderedList: { group: 'block', content: 'listItem+', marks: '_', attrs: { order: { default: 1 }, ...A('localId') },
    parseDOM: [{ tag: 'ol', getAttrs: d => ({ order: d.hasAttribute('start') ? +d.getAttribute('start') || 1 : 1 }) }],
    toDOM: n => ['ol', n.attrs.order !== 1 ? { start: n.attrs.order } : {}, 0] },
  listItem: { content: '(paragraph | mediaSingle | codeBlock) (paragraph | ' + LIST + ' | mediaSingle | codeBlock | unknownBlock)*', defining: true, marks: '_', attrs: A('localId'),
    parseDOM: [{ tag: 'li', getAttrs: d => (d.hasAttribute('data-task') ? false : null) }], toDOM: () => ['li', 0] },
  taskList: { group: 'block', content: 'taskItem (taskItem | taskList)*', marks: '_', attrs: A('localId'),
    parseDOM: [{ tag: 'ul[data-tasks]', priority: 60 }, { tag: 'ul.contains-task-list', priority: 60 }], toDOM: () => ['ul', { 'data-tasks': '', class: 'tasks' }, 0] },
  taskItem: { content: 'inline*', defining: true, marks: '_', attrs: { state: { default: 'TODO' }, ...A('localId') },
    parseDOM: [{ tag: 'li[data-task]', priority: 60, getAttrs: d => ({ state: d.getAttribute('data-task') === 'DONE' ? 'DONE' : 'TODO' }) },
      { tag: 'li.task-list-item', priority: 60, getAttrs: d => ({ state: d.querySelector('input[type=checkbox]')?.checked ? 'DONE' : 'TODO' }) }],
    toDOM: n => ['li', { 'data-task': n.attrs.state, class: 'task' + (n.attrs.state === 'DONE' ? ' done' : '') }, 0] },
  decisionList: { group: 'block', content: 'decisionItem+', marks: '_', attrs: A('localId'),
    parseDOM: [{ tag: 'div[data-decisions]' }], toDOM: () => ['div', { 'data-decisions': '', class: 'md-decisions' }, 0] },
  decisionItem: { content: 'inline*', defining: true, marks: '_', attrs: { state: { default: 'DECIDED' }, ...A('localId') },
    parseDOM: [{ tag: 'div[data-decision]' }], toDOM: () => ['div', { 'data-decision': '', class: 'md-decision' }, 0] },
  codeBlock: { group: 'block', content: 'text*', marks: '', code: true, defining: true, attrs: A('language', 'uniqueId'),
    parseDOM: [{ tag: 'pre', preserveWhitespace: 'full', getAttrs: d => ({ language: d.getAttribute('data-language') || d.querySelector('code')?.getAttribute('data-lang') || (/(?:^|\s)language-(\S+)/.exec(d.querySelector('code')?.className || '') || [])[1] || null }) }],
    toDOM: n => ['pre', n.attrs.language ? { 'data-language': n.attrs.language } : {}, ['code', 0]] },
  rule: { group: 'block', attrs: A(), parseDOM: [{ tag: 'hr' }], toDOM: () => ['hr'] },
  panel: { group: 'block', content: '(paragraph | heading | ' + LIST + ' | decisionList | codeBlock | rule | mediaSingle | mediaGroup | blockCard | unknownBlock)+', defining: true, marks: '_',
    attrs: { panelType: { default: 'info' }, ...A('panelIcon', 'panelIconId', 'panelIconText', 'panelColor', 'localId') },
    parseDOM: [{ tag: 'div[data-panel]', getAttrs: d => ({ panelType: d.getAttribute('data-panel') || 'info' }) }],
    toDOM: n => ['div', { 'data-panel': n.attrs.panelType, class: 'md-panel p-' + n.attrs.panelType }, ['span', { class: 'rte-pi', contenteditable: 'false' }, panelIcon[n.attrs.panelType] || 'ⓘ'], ['div', { class: 'rte-pc' }, 0]] },
  expand: { group: 'block', content: '(' + LEAFY + ' | table | nestedExpand)+', defining: true, isolating: true, marks: '_', attrs: { title: { default: '' }, ...A('localId') },
    parseDOM: [{ tag: 'div[data-expand]', contentElement: '.rte-xb', getAttrs: d => ({ title: d.getAttribute('data-title') || '' }) },
      { tag: 'details', contentElement: d => { const s = d.querySelector('summary'); if (s) s.remove(); return d; }, getAttrs: d => ({ title: d.querySelector('summary')?.textContent || '' }) }],
    toDOM: n => ['div', { 'data-expand': '', 'data-title': n.attrs.title, class: 'md-expand' }, ['div', { class: 'rte-xh', contenteditable: 'false' }, n.attrs.title], ['div', { class: 'rte-xb' }, 0]] },
  nestedExpand: { content: '(' + LEAFY + ')+', defining: true, isolating: true, marks: '_', attrs: { title: { default: '' }, ...A('localId') },
    parseDOM: [{ tag: 'div[data-nested-expand]', contentElement: '.rte-xb', getAttrs: d => ({ title: d.getAttribute('data-title') || '' }) }],
    toDOM: n => ['div', { 'data-nested-expand': '', 'data-title': n.attrs.title, class: 'md-expand' }, ['div', { class: 'rte-xh', contenteditable: 'false' }, n.attrs.title], ['div', { class: 'rte-xb' }, 0]] },
  table: { group: 'block', content: 'tableRow+', tableRole: 'table', isolating: true, marks: '_',
    attrs: A('isNumberColumnEnabled', 'layout', 'localId', 'width', 'displayMode'),
    parseDOM: [{ tag: 'table' }], toDOM: () => ['div', { class: 'md-table' }, ['table', ['tbody', 0]]] },
  tableRow: { content: '(tableCell | tableHeader)+', tableRole: 'row', marks: '_', attrs: A('localId'),
    parseDOM: [{ tag: 'tr' }], toDOM: () => ['tr', 0] },
  tableCell: cell('td', 'cell'),
  tableHeader: cell('th', 'header_cell'),
  mediaSingle: { group: 'block', content: 'media caption?', marks: '_', draggable: true, isolating: true,
    attrs: { layout: { default: 'center' }, ...A('width', 'widthType') },
    parseDOM: [{ tag: 'figure[data-media-single]', getAttrs: d => ({ layout: d.getAttribute('data-layout') || 'center', width: num(d.getAttribute('data-width')), widthType: d.getAttribute('data-width-type') || null }) },
      { tag: 'img[src]', getAttrs: d => (/^(https?:|data:image\/)/.test(d.getAttribute('src')) && !d.closest('[data-media-single],[data-media-group]') ? null : false),
        getContent: (d, s) => Fragment.from(s.nodes.media.create({ type: 'external', url: d.getAttribute('src'), alt: d.getAttribute('alt') || null })) }],
    toDOM: n => ['figure', { 'data-media-single': '', 'data-layout': n.attrs.layout, 'data-width': data(n.attrs.width), 'data-width-type': n.attrs.widthType }, 0] },
  media: { atom: true, marks: '_', attrs: { type: { default: 'file' }, ...A('id', 'collection', 'alt', 'width', 'height', 'url', 'occurrenceKey') },
    parseDOM: [{ tag: 'img[data-media]', getAttrs: d => mediaAttrs(d) }, { tag: 'div[data-media]', getAttrs: d => mediaAttrs(d) }],
    toDOM: n => ['div', { 'data-media': '', 'data-id': n.attrs.id, 'data-type': n.attrs.type, 'data-collection': n.attrs.collection, 'data-alt': n.attrs.alt, 'data-url': n.attrs.url, 'data-w': data(n.attrs.width), 'data-h': data(n.attrs.height) }, n.attrs.alt || ''] },
  caption: { content: 'inline*', marks: '_', attrs: A('localId'), parseDOM: [{ tag: 'figcaption' }], toDOM: () => ['figcaption', 0] },
  mediaGroup: { group: 'block', content: 'media+', marks: '_', attrs: A(),
    parseDOM: [{ tag: 'div[data-media-group]' }], toDOM: () => ['div', { 'data-media-group': '', class: 'rte-files' }, 0] },
  mediaInline: { group: 'inline', inline: true, atom: true, attrs: { type: { default: 'file' }, ...A('id', 'collection', 'alt', 'width', 'height', 'localId') },
    parseDOM: [{ tag: 'span[data-media-inline]', getAttrs: d => ({ id: d.getAttribute('data-id'), collection: d.getAttribute('data-collection') || '', type: d.getAttribute('data-type') || 'file', alt: d.getAttribute('data-alt') }) }],
    toDOM: n => ['span', { 'data-media-inline': '', 'data-id': n.attrs.id, 'data-collection': n.attrs.collection, 'data-type': n.attrs.type, 'data-alt': n.attrs.alt, class: 'rte-file' }, n.attrs.alt || 'file'] },
  blockCard: { group: 'block', atom: true, attrs: A('url', 'datasource', 'width', 'layout', 'localId'),
    parseDOM: [{ tag: 'div[data-block-card]', getAttrs: d => ({ url: d.getAttribute('data-url') }) }],
    toDOM: n => ['div', { 'data-block-card': '', 'data-url': n.attrs.url, class: 'rte-card' }, n.attrs.url || ''] },
  embedCard: { group: 'block', atom: true, attrs: { layout: { default: 'center' }, ...A('url', 'width', 'originalWidth', 'originalHeight', 'localId') },
    parseDOM: [{ tag: 'div[data-embed-card]', getAttrs: d => ({ url: d.getAttribute('data-url') }) }],
    toDOM: n => ['div', { 'data-embed-card': '', 'data-url': n.attrs.url, class: 'rte-card' }, n.attrs.url || ''] },
  layoutSection: { content: 'layoutColumn+', isolating: true, marks: '_', attrs: A('localId'),
    parseDOM: [{ tag: 'div[data-layout-section]' }], toDOM: () => ['div', { 'data-layout-section': '', class: 'rte-cols' }, 0] },
  layoutColumn: { content: '(' + LEAFY + ' | table | expand)+', isolating: true, marks: '_', attrs: A('width', 'localId'),
    parseDOM: [{ tag: 'div[data-layout-column]', getAttrs: d => ({ width: num(d.getAttribute('data-width')) }) }],
    toDOM: n => ['div', { 'data-layout-column': '', 'data-width': data(n.attrs.width), class: 'rte-col', style: n.attrs.width ? 'flex:' + n.attrs.width : null }, 0] },
  hardBreak: { group: 'inline', inline: true, selectable: false, attrs: A(), parseDOM: [{ tag: 'br' }], toDOM: () => ['br'], leafText: () => '\n' },
  mention: { group: 'inline', inline: true, atom: true, attrs: A('id', 'text', 'accessLevel', 'userType', 'localId'),
    parseDOM: [{ tag: 'span[data-mention]', getAttrs: d => ({ id: d.getAttribute('data-mention'), text: d.textContent }) }],
    toDOM: n => ['span', { 'data-mention': n.attrs.id, class: 'mention' }, mentionText(n)], leafText: n => mentionText(n) },
  emoji: { group: 'inline', inline: true, atom: true, attrs: A('shortName', 'id', 'text'),
    parseDOM: [{ tag: 'span[data-emoji]', getAttrs: d => ({ shortName: d.getAttribute('data-emoji'), id: d.getAttribute('data-emoji-id'), text: d.textContent }) }],
    toDOM: n => ['span', { 'data-emoji': n.attrs.shortName, 'data-emoji-id': n.attrs.id, class: 'md-emo', title: n.attrs.shortName }, n.attrs.text || n.attrs.shortName || ''],
    leafText: n => n.attrs.text || n.attrs.shortName || '' },
  status: { group: 'inline', inline: true, atom: true, attrs: { color: { default: 'neutral' }, ...A('text', 'localId', 'style') },
    parseDOM: [{ tag: 'span[data-status]', getAttrs: d => ({ color: d.getAttribute('data-status') || 'neutral', text: d.textContent }) }],
    toDOM: n => ['span', { 'data-status': n.attrs.color, class: 'md-status c-' + n.attrs.color }, (n.attrs.text || '').toUpperCase()], leafText: n => n.attrs.text || '' },
  date: { group: 'inline', inline: true, atom: true, attrs: A('timestamp', 'localId'),
    parseDOM: [{ tag: 'span[data-date]', getAttrs: d => ({ timestamp: d.getAttribute('data-date') }) }, { tag: 'time[datetime]', getAttrs: d => { const t = Date.parse(d.getAttribute('datetime')); return isNaN(t) ? false : { timestamp: String(t) }; } }],
    toDOM: n => ['span', { 'data-date': n.attrs.timestamp, class: 'md-date' }, dateText(n.attrs.timestamp)], leafText: n => dateText(n.attrs.timestamp) },
  inlineCard: { group: 'inline', inline: true, atom: true, attrs: A('url', 'data', 'localId'),
    parseDOM: [{ tag: 'span[data-inline-card]', getAttrs: d => ({ url: d.getAttribute('data-url') }) }],
    toDOM: n => ['span', { 'data-inline-card': '', 'data-url': n.attrs.url, class: 'rte-icard' }, n.attrs.url || ''], leafText: n => n.attrs.url || '' },
  // Kept whole: the JSON of a node the editor can't edit (raw), drawn as a chip.
  unknownBlock: { group: 'block', atom: true, attrs: { raw: { default: null } },
    parseDOM: [{ tag: 'div[data-adf]', getAttrs: d => rawAttr(d) }], toDOM: n => ['div', { 'data-adf': JSON.stringify(n.attrs.raw), class: 'rte-kept' }, keptLabel(n.attrs.raw)] },
  unknownInline: { group: 'inline', inline: true, atom: true, attrs: { raw: { default: null } },
    parseDOM: [{ tag: 'span[data-adf]', getAttrs: d => rawAttr(d) }], toDOM: n => ['span', { 'data-adf': JSON.stringify(n.attrs.raw), class: 'rte-kept' }, keptLabel(n.attrs.raw)],
    leafText: n => keptLabel(n.attrs.raw) },
};

function cell(tag, role) {
  return { content: CELL, tableRole: role, isolating: true, marks: '_', attrs: { colspan: { default: 1 }, rowspan: { default: 1 }, colwidth: { default: null }, ...A('background', 'localId') },
    parseDOM: [{ tag, getAttrs: d => {
      const w = d.getAttribute('data-colwidth'), cw = w && /^\d+(,\d+)*$/.test(w) ? w.split(',').map(Number) : null;
      return { colspan: +d.getAttribute('colspan') || 1, rowspan: +d.getAttribute('rowspan') || 1, colwidth: cw, background: d.getAttribute('data-background') || null };
    } }],
    toDOM: n => [tag, { colspan: n.attrs.colspan !== 1 ? n.attrs.colspan : null, rowspan: n.attrs.rowspan !== 1 ? n.attrs.rowspan : null,
      'data-colwidth': n.attrs.colwidth ? n.attrs.colwidth.join(',') : null, 'data-background': n.attrs.background,
      class: n.attrs.background ? 'md-bg' : null, style: n.attrs.background ? '--cell:' + n.attrs.background : null }, 0] };
}

function mediaAttrs(d) {
  return { id: d.getAttribute('data-id'), type: d.getAttribute('data-type') || 'file', collection: d.getAttribute('data-collection') || '', alt: d.getAttribute('data-alt') || null,
    url: d.getAttribute('data-url') || null, width: num(d.getAttribute('data-w')), height: num(d.getAttribute('data-h')) };
}
function rawAttr(d) { try { const raw = JSON.parse(d.getAttribute('data-adf')); return raw && typeof raw === 'object' ? { raw } : false; } catch (e) { return false; } }
const mentionText = n => { const t = n.attrs.text || ''; return t.startsWith('@') ? t : '@' + (t || 'someone'); };
export function dateText(ts) {
  const d = new Date(+ts);
  return isNaN(d) ? String(ts || '') : d.toLocaleDateString(undefined, { day: 'numeric', month: 'short', year: 'numeric', timeZone: 'UTC' });
}
// A kept node's label: its kind, a macro's name, the text in it.
export function keptLabel(raw) {
  if (!raw || typeof raw !== 'object') return '?';
  const name = raw.attrs && (raw.attrs.extensionTitle || raw.attrs.extensionKey);
  const text = textOf(raw).trim();
  return (name ? name : raw.type || 'node') + (text ? ': ' + (text.length > 60 ? text.slice(0, 60) + '…' : text) : '');
}
const textOf = n => (n.text || '') + (Array.isArray(n.content) ? n.content.map(textOf).join(' ') : '');

const marks = {
  link: { attrs: A('href', 'title', 'id', 'collection', 'occurrenceKey'), inclusive: false,
    parseDOM: [{ tag: 'a[href]', getAttrs: d => ({ href: d.getAttribute('href'), title: d.getAttribute('title') || null }) }],
    toDOM: m => ['a', { href: m.attrs.href, title: m.attrs.title, rel: 'noopener noreferrer' }, 0] },
  em: { attrs: A(), parseDOM: [{ tag: 'i' }, { tag: 'em' }, { style: 'font-style=italic' }], toDOM: () => ['em', 0] },
  strong: { attrs: A(), parseDOM: [{ tag: 'strong' }, { tag: 'b', getAttrs: d => d.style.fontWeight !== 'normal' && null }, { style: 'font-weight', getAttrs: v => /^(bold(er)?|[5-9]\d{2,})$/.test(v) && null }], toDOM: () => ['strong', 0] },
  strike: { attrs: A(), parseDOM: [{ tag: 's' }, { tag: 'del' }, { tag: 'strike' }, { style: 'text-decoration', getAttrs: v => (v.includes('line-through') ? null : false) }], toDOM: () => ['s', 0] },
  underline: { attrs: A(), parseDOM: [{ tag: 'u' }, { style: 'text-decoration', getAttrs: v => (v.includes('underline') ? null : false) }], toDOM: () => ['u', 0] },
  code: { attrs: A(), excludes: 'strong em strike underline textColor backgroundColor subsup', code: true, parseDOM: [{ tag: 'code' }], toDOM: () => ['code', 0] },
  subsup: { attrs: { type: { default: 'sub' }, ...A() }, parseDOM: [{ tag: 'sub', attrs: { type: 'sub' } }, { tag: 'sup', attrs: { type: 'sup' } }], toDOM: m => [m.attrs.type === 'sup' ? 'sup' : 'sub', 0] },
  textColor: { attrs: A('color'), parseDOM: [{ tag: 'span[data-color]', getAttrs: d => ({ color: d.getAttribute('data-color') }) }],
    toDOM: m => ['span', { 'data-color': m.attrs.color, class: 'md-col', style: '--c:' + m.attrs.color }, 0] },
  backgroundColor: { attrs: A('color'), parseDOM: [{ tag: 'span[data-bg]', getAttrs: d => ({ color: d.getAttribute('data-bg') }) }],
    toDOM: m => ['span', { 'data-bg': m.attrs.color, class: 'md-hi', style: '--c:' + m.attrs.color }, 0] },
  // Block marks: on a paragraph or heading (alignment, indentation), a code block or an expand (breakout).
  alignment: { attrs: A('align'), toDOM: m => ['div', { style: 'text-align:' + (m.attrs.align === 'end' ? 'right' : m.attrs.align === 'center' ? 'center' : 'left') }, 0] },
  indentation: { attrs: A('level'), toDOM: m => ['div', { style: 'margin-left:' + 1.5 * (+m.attrs.level || 1) + 'em' }, 0] },
  breakout: { attrs: A('mode', 'width'), toDOM: () => ['div', 0] },
  annotation: { attrs: A('id', 'annotationType'), excludes: '', toDOM: () => ['span', 0] },
  border: { attrs: A('size', 'color'), toDOM: () => ['span', 0] },
  unknownMark: { attrs: { raw: { default: null } }, excludes: '', toDOM: () => ['span', 0] },
};

export const schema = new Schema({ nodes, marks });

// ---- ADF → ProseMirror
const known = new Set(Object.keys(nodes).filter(n => !/^(text|unknownBlock|unknownInline)$/.test(n)));

function attrsIn(type, json) {
  const given = json.attrs && typeof json.attrs === 'object' ? json.attrs : {};
  const out = {}, extra = {};
  for (const [k, v] of Object.entries(given)) (k in type.attrs && k !== 'extra' && k !== 'raw' ? (out[k] = v) : (extra[k] = v));
  if (Object.keys(extra).length) out.extra = extra;
  return out;
}

function markIn(json) {
  if (!json || typeof json !== 'object') return null;
  const t = schema.marks[json.type];
  if (!t || json.type === 'unknownMark') return schema.marks.unknownMark.create({ raw: json });
  try { return t.create(attrsIn(t, json)); } catch (e) { return schema.marks.unknownMark.create({ raw: json }); }
}

// One ADF node as PM nodes (a text run with no text is none). inline: its parent holds inline content.
function nodeIn(json, inline) {
  const keep = () => [schema.nodes[inline ? 'unknownInline' : 'unknownBlock'].create({ raw: json })];
  if (!json || typeof json !== 'object') return [];
  if (json.type === 'text') {
    if (!inline) return keep();
    if (typeof json.text !== 'string' || !json.text) return [];
    const ms = (json.marks || []).map(markIn).filter(Boolean);
    return [schema.text(json.text, ms.reduce((set, m) => m.addToSet(set), []))];
  }
  if (!known.has(json.type)) return keep();
  const type = schema.nodes[json.type];
  if (type.isInline !== inline) return keep();
  const kids = Array.isArray(json.content) ? json.content : [];
  const content = [];
  for (const c of kids) content.push(...nodeIn(c, type.inlineContent));
  let frag = Fragment.from(content);
  // An empty container Jira wrote (a list item, a cell) gets the paragraph it needs.
  if (!frag.size && !type.isLeaf && !type.validContent(frag)) {
    const fill = type.contentMatch.fillBefore(Fragment.empty, true);
    if (fill) frag = fill;
  }
  if (!type.validContent(frag)) return keep();
  // The marks it carries (a paragraph's alignment, a text's) must be ones its kind may hold.
  if (type.inlineContent) {
    let ok = true;
    frag.forEach(c => { if (!type.allowsMarks(c.marks)) ok = false; });
    if (!ok) return keep();
  }
  try {
    const ms = (json.marks || []).map(markIn).filter(Boolean);
    const node = type.create(attrsIn(type, json), frag, ms.reduce((set, m) => m.addToSet(set), []));
    node.check();
    return [node];
  } catch (e) { return keep(); }
}

export function fromADF(json) {
  let doc = json;
  if (typeof doc === 'string') { try { doc = JSON.parse(doc); } catch (e) { doc = null; } }
  const content = [];
  for (const c of (doc && doc.type === 'doc' && Array.isArray(doc.content) ? doc.content : [])) content.push(...nodeIn(c, false));
  // A block the doc can't hold at the top (a list item alone) is kept.
  const top = content.map(n => (schema.nodes.doc.contentMatch.matchType(n.type) || n.type.name === 'layoutSection' ? n : schema.nodes.unknownBlock.create({ raw: toADFNode(n) })));
  if (!top.length) top.push(schema.nodes.paragraph.create());
  return schema.nodes.doc.create(doc && doc.type === 'doc' ? attrsIn(schema.nodes.doc, doc) : null, top);
}

// ---- ProseMirror → ADF
function attrsOut(node) {
  const out = {};
  for (const [k, v] of Object.entries(node.attrs)) if (k !== 'extra' && v != null) out[k] = v;
  if (node.attrs.extra) Object.assign(out, node.attrs.extra);
  return out;
}
function markOut(m) {
  if (m.type.name === 'unknownMark') return m.attrs.raw;
  const out = { type: m.type.name }, a = attrsOut(m);
  if (Object.keys(a).length) out.attrs = a;
  return out;
}
function toADFNode(node) {
  if (node.isText) {
    const out = { type: 'text', text: node.text };
    if (node.marks.length) out.marks = node.marks.map(markOut);
    return out;
  }
  if (node.type.name === 'unknownBlock' || node.type.name === 'unknownInline') return node.attrs.raw;
  const out = { type: node.type.name }, a = attrsOut(node);
  if (Object.keys(a).length) out.attrs = a;
  if (!node.isLeaf) { out.content = []; node.forEach(c => out.content.push(toADFNode(c))); }
  if (node.marks.length) out.marks = node.marks.map(markOut);
  return out;
}

export function toADF(doc) {
  const out = { type: 'doc', version: 1, content: [] };
  if (doc.attrs.extra) Object.assign(out, { ...doc.attrs.extra, type: 'doc', content: [] });
  doc.forEach(c => out.content.push(toADFNode(c)));
  // A lone empty paragraph is an empty document.
  if (isEmpty(doc)) out.content = [];
  return out;
}

export const emptyDoc = () => schema.nodes.doc.create(null, schema.nodes.paragraph.create());
export const isEmpty = doc => doc.childCount === 1 && doc.firstChild.type.name === 'paragraph' && !doc.firstChild.content.size && !doc.firstChild.marks.length;
