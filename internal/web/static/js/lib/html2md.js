// HTML → markdown, for a paste from a web page, Confluence, Google Docs or a mail:
// headings, bold, italic, strikethrough, underline, code, links, lists (task
// boxes too), quotes, tables, rules and images; anything else as its text.
// Pure: works on any DOM-shaped tree (nodeType, nodeName, childNodes,
// getAttribute, data), tested with node (internal/web/jstest).
//
//   htmlToMd(document.body) → markdown
//   rich(html) → does the HTML carry formatting (not only divs, spans and breaks, as a code editor's copy)?

const SKIP = new Set(['SCRIPT', 'STYLE', 'HEAD', 'META', 'TITLE', 'NOSCRIPT', 'TEMPLATE', 'SVG', 'BUTTON', 'SELECT', 'TEXTAREA', 'INPUT', 'LINK']);
const BLOCK = new Set(['P', 'DIV', 'SECTION', 'ARTICLE', 'HEADER', 'FOOTER', 'MAIN', 'ASIDE', 'NAV', 'FIGURE', 'FIGCAPTION', 'DL', 'DT', 'DD', 'ADDRESS',
  'H1', 'H2', 'H3', 'H4', 'H5', 'H6', 'UL', 'OL', 'LI', 'BLOCKQUOTE', 'PRE', 'TABLE', 'THEAD', 'TBODY', 'TFOOT', 'TR', 'TD', 'TH', 'HR', 'BR']);
const SAFE = /^(https?:|mailto:)/i;

export const rich = html => /<(p|h[1-6]|b|strong|i|em|a|ul|ol|li|table|img|blockquote|pre|code|s|del|strike|hr|u)[\s>]/i.test(html);

const kids = n => [...(n.childNodes || [])];
const attr = (n, a) => (n.getAttribute && n.getAttribute(a)) || '';
const text = n => (n.nodeType === 3 ? n.data : kids(n).map(text).join(''));
const style = (n, re) => re.test(attr(n, 'style'));
const esc = s => s.replace(/([\\*`])/g, '\\$1');
const url = u => u.replace(/[()\s]/g, c => '%' + c.charCodeAt(0).toString(16).toUpperCase().padStart(2, '0'));

// Markers around inline text, its spaces kept outside: "** b **" is no bold.
function wrap(pre, s, post = pre) {
  const m = /^(\s*)([\s\S]*?)(\s*)$/.exec(s);
  return m[2] ? m[1] + pre + m[2] + post + m[3] : s;
}
const ticks = s => { let n = 1; for (const r of s.match(/`+/g) || []) n = Math.max(n, r.length + 1); return '`'.repeat(n); };
const block = s => '\n\n' + s.trim() + '\n\n';

function inlineOnly(n, c) { return conv(n, { ...c, cell: true }).replace(/\s*\n\s*/g, ' ').replace(/ {2,}/g, ' ').trim(); }

function list(n, c, ordered) {
  let num = +(attr(n, 'start') || 1);
  const items = [];
  for (const li of kids(n)) {
    if (li.nodeName !== 'LI') { if (li.nodeName === 'UL' || li.nodeName === 'OL') items.push(list(li, c, li.nodeName === 'OL').trim().replace(/^/gm, '  ')); continue; }
    const box = findBox(li);
    const mark = (ordered ? num++ + '. ' : '- ') + (box ? (box.checked ? '[x] ' : '[ ] ') : '');
    const body = conv(li, c).replace(/\n{2,}/g, '\n').trim().split('\n');
    const pad = ' '.repeat(ordered ? String(num - 1).length + 2 : 2);
    items.push(body.map((l, i) => (i ? (l ? pad + l : '') : mark + l)).join('\n'));
  }
  return block(items.join('\n'));
}
// A checkbox at the start of a list item: a task.
function findBox(li) {
  for (const k of kids(li).slice(0, 3)) {
    if (k.nodeName === 'INPUT' && attr(k, 'type').toLowerCase() === 'checkbox') return { checked: k.checked || (k.hasAttribute && k.hasAttribute('checked')) };
    if (k.nodeName === 'P' || k.nodeName === 'LABEL') { const b = findBox(k); if (b) return b; }
  }
  return null;
}

function table(n, c) {
  const rows = [];
  const walk = x => { for (const k of kids(x)) { if (k.nodeName === 'TR') rows.push(k); else if (/^(THEAD|TBODY|TFOOT)$/.test(k.nodeName)) walk(k); } };
  walk(n);
  if (!rows.length) return '';
  const cells = rows.map(r => kids(r).filter(k => k.nodeName === 'TD' || k.nodeName === 'TH').map(k => inlineOnly(k, c).replace(/\|/g, '\\|')));
  const w = Math.max(...cells.map(r => r.length));
  if (!w) return '';
  const line = r => '| ' + Array.from({ length: w }, (_, i) => r[i] || '').join(' | ') + ' |';
  return block([line(cells[0]), '|' + ' --- |'.repeat(w), ...cells.slice(1).map(line)].join('\n'));
}

function conv(n, c) {
  let out = '';
  const ks = kids(n);
  ks.forEach((k, i) => {
    if (k.nodeType === 3) {
      if (c.pre) { out += k.data; return; }
      const t = k.data.replace(/\s+/g, ' ');
      if (!t.trim() && (BLOCK.has((ks[i - 1] || {}).nodeName) || BLOCK.has((ks[i + 1] || {}).nodeName) || ks.length === 1)) return;
      out += esc(t);
      return;
    }
    if (k.nodeType !== 1 || SKIP.has(k.nodeName)) return;
    out += el(k, c);
  });
  return out;
}

function el(n, c) {
  const name = n.nodeName, inner = () => conv(n, c);
  switch (name) {
    case 'H1': case 'H2': case 'H3': case 'H4': case 'H5': case 'H6':
      return c.cell ? inner() : block('#'.repeat(+name[1]) + ' ' + inlineOnly(n, c));
    case 'P': return c.cell ? ' ' + inner() + ' ' : block(inner());
    case 'DIV': case 'SECTION': case 'ARTICLE': case 'HEADER': case 'FOOTER': case 'MAIN': case 'ASIDE': case 'NAV': case 'FIGURE': case 'FIGCAPTION':
    case 'DL': case 'DT': case 'DD': case 'ADDRESS':
      return c.cell ? ' ' + inner() + ' ' : '\n' + inner().trim() + '\n';
    case 'BR': return c.cell ? ' ' : '\n';
    case 'HR': return c.cell ? '' : block('---');
    case 'B': case 'STRONG':
      return style(n, /font-weight:\s*(normal|[1-4]00)/i) ? inner() : wrap('**', inner());
    case 'I': case 'EM': case 'CITE': return wrap('*', inner());
    case 'S': case 'DEL': case 'STRIKE': return wrap('~~', inner());
    case 'U': case 'INS': return wrap('<u>', inner(), '</u>');
    case 'SUB': case 'SUP': return wrap('<' + name.toLowerCase() + '>', inner(), '</' + name.toLowerCase() + '>');
    case 'SPAN': {
      let s = inner();
      if (style(n, /font-weight:\s*(bold|[6-9]00)/i)) s = wrap('**', s);
      if (style(n, /font-style:\s*italic/i)) s = wrap('*', s);
      if (style(n, /text-decoration[^;]*line-through/i)) s = wrap('~~', s);
      return s;
    }
    case 'CODE': case 'KBD': case 'SAMP': case 'TT': {
      if (c.pre) return text(n);
      const t = text(n).replace(/\s+/g, ' ');
      if (!t.trim()) return t;
      const f = ticks(t);
      return f + (t[0] === '`' ? ' ' : '') + t + (t.endsWith('`') ? ' ' : '') + f;
    }
    case 'PRE': {
      const t = text(n).replace(/\n$/, '');
      if (c.cell) return '`' + t.replace(/\s+/g, ' ') + '`';
      const code = kids(n).find(k => k.nodeName === 'CODE');
      const lang = (/(?:^|\s)(?:language|lang)-([\w+-]+)/.exec(attr(code || n, 'class')) || [])[1] || '';
      const fence = /^```/m.test(t) ? '~~~' : '```';
      return '\n\n' + fence + lang + '\n' + t + '\n' + fence + '\n\n';
    }
    case 'A': {
      const href = attr(n, 'href').trim(), s = inner();
      if (!SAFE.test(href) || !s.trim()) return s;
      if (s.trim() === href || s.trim() === href.replace(/^mailto:/, '')) return href.replace(/^mailto:/, '');
      return wrap('[', s.replace(/[[\]]/g, '\\$&'), '](' + url(href) + ')');
    }
    case 'IMG': {
      const src = attr(n, 'src'), alt = attr(n, 'alt').replace(/[[\]\n]/g, ' ');
      return SAFE.test(src) ? '![' + alt + '](' + url(src) + ')' : alt;
    }
    case 'UL': case 'OL': return c.cell ? inner() : list(n, c, name === 'OL');
    case 'LI': return inner();
    case 'BLOCKQUOTE': {
      if (c.cell) return inner();
      const s = conv(n, c).replace(/\n{3,}/g, '\n\n').trim();
      return block(s.split('\n').map(l => (l ? '> ' + l : '>')).join('\n'));
    }
    case 'TABLE': return c.cell ? inner() : table(n, c);
    default: return inner();
  }
}

export function htmlToMd(root) {
  return conv(root, {})
    .replace(/[ \t]+\n/g, '\n')
    .replace(/\n{3,}/g, '\n\n')
    .replace(/^\n+|\s+$/g, '');
}
