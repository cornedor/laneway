// Markdown → DOM for Jira text (the dialect internal/jira/adf.go writes). Builds nodes
// with createElement/textContent only: nothing from the text is ever parsed as HTML.
//
//   render(md, {attachment(id)→url, isKey(str)→bool, onKey(key), onTask(n, total, done, input), names:[…]}) → DocumentFragment
import { h } from './dom.js';

const SAFE_HREF = /^(https?:|mailto:|#)/i;
const KEY = /[A-Z][A-Z0-9]+-\d+/y;
const URLRE = /https?:\/\/[^\s<>]+/y;
const TAGS = [
  [/<(u|sub|sup)>([\s\S]*?)<\/\1>/y, (m, o) => h(m[1] === 'u' ? 'u' : m[1], inline(m[2], o))],
  [/<date>(\d{4}-\d{2}-\d{2})<\/date>/y, m => h('span.md-date', fmtDate(m[1]))],
  [/<status color="([a-z-]*)">([\s\S]*?)<\/status>/y, m => h('span.md-status.c-' + m[1].replace(/[^a-z]/g, ''), m[2])],
  [/<span style="[^"]*">([\s\S]*?)<\/span>/y, (m, o) => h('span', inline(m[1], o))],
  [/<(https?:\/\/[^>\s]+)>/y, m => link(m[1], m[1])],
];

function fmtDate(iso) {
  const d = new Date(iso + 'T00:00:00Z');
  return isNaN(d) ? iso : d.toLocaleDateString(undefined, { day: 'numeric', month: 'short', year: 'numeric', timeZone: 'UTC' });
}

function link(href, text, o) {
  if (!SAFE_HREF.test(href)) return document.createTextNode(text);
  const ext = /^https?:/i.test(href);
  return h('a', { href, target: ext ? '_blank' : null, rel: ext ? 'noopener noreferrer' : null }, text);
}

// "[text](href)" at s[i]: {text, href, end} or null. Brackets and parens may nest.
function linkAt(s, i) {
  let depth = 0, j = i;
  for (; j < s.length; j++) {
    const c = s[j];
    if (c === '\\') { j++; continue; }
    if (c === '[') depth++;
    else if (c === ']' && --depth === 0) break;
  }
  if (j >= s.length || s[j + 1] !== '(') return null;
  let k = j + 2, p = 1;
  for (; k < s.length; k++) {
    if (s[k] === '(') p++;
    else if (s[k] === ')' && --p === 0) break;
  }
  if (k >= s.length) return null;
  return { text: s.slice(i + 1, j), href: s.slice(j + 2, k).trim().split(/\s+"/)[0].replace(/^<|>$/g, ''), end: k + 1 };
}

const word = c => c !== undefined && /[\p{L}\p{N}]/u.test(c);

function mentionAt(s, i, names) {
  const rest = s.slice(i + 1, i + 60);
  if (names) for (const n of names) if (rest.startsWith(n) && !word(rest[n.length])) return '@' + n;
  const m = /^[\p{L}][\p{L}\p{N}'._-]*/u.exec(rest);
  return m ? '@' + m[0].replace(/[.]+$/, '') : null;
}

export function inline(s, o = {}) {
  const out = [];
  let buf = '';
  const flush = () => { if (buf) { out.push(document.createTextNode(buf)); buf = ''; } };
  const push = n => { flush(); out.push(n); };
  let i = 0;
  while (i < s.length) {
    const c = s[i];
    if (c === '\\' && i + 1 < s.length && /[\\`*_{}[\]()#+\-.!|~<>@:]/.test(s[i + 1])) { buf += s[i + 1]; i += 2; continue; }
    if (c === '\n') { push(h('br')); i++; continue; }
    if (c === '`') {
      const j = s.indexOf('`', i + 1);
      if (j > i + 1) { push(h('code', s.slice(i + 1, j))); i = j + 1; continue; }
    } else if (c === '!' && s[i + 1] === '[') {
      const m = linkAt(s, i + 1);
      if (m) {
        i = m.end;
        if (m.href.startsWith('attachment:') && o.attachment) push(h('img.md-img', { src: o.attachment(m.href.slice(11)), alt: m.text, loading: 'lazy', dataset: { id: m.href.slice(11) } }));
        else push(link(m.href, m.text || m.href));
        continue;
      }
    } else if (c === '[') {
      const m = linkAt(s, i);
      if (m) { push(SAFE_HREF.test(m.href) ? linkWith(m.href, inline(m.text, o)) : h('span', inline(m.text, o))); i = m.end; continue; }
    } else if (c === '*' || c === '~') {
      const d = s.startsWith(c + c, i) ? c + c : (c === '*' ? '*' : '');
      if (d) {
        let j = i + d.length;
        for (; ;) {
          j = s.indexOf(d, j);
          if (j < 0 || s[j - 1] !== '\\' && (d.length === 2 || s[j + 1] !== '*')) break;
          j += d.length;
        }
        if (j > i + d.length) {
          const tag = d === '**' ? 'strong' : d === '*' ? 'em' : 's';
          push(h(tag, inline(s.slice(i + d.length, j), o))); i = j + d.length; continue;
        }
      }
    } else if (c === '_' && !word(s[i - 1])) {
      let j = i + 1;
      for (; ;) { j = s.indexOf('_', j); if (j < 0 || !word(s[j + 1])) break; j++; }
      if (j > i + 1) { push(h('em', inline(s.slice(i + 1, j), o))); i = j + 1; continue; }
    } else if (c === '<') {
      let hit = false;
      for (const [re, make] of TAGS) {
        re.lastIndex = i;
        const m = re.exec(s);
        if (m) { push(make(m, o)); i = re.lastIndex; hit = true; break; }
      }
      if (hit) continue;
    } else if (c === '@' && !word(s[i - 1])) {
      const m = mentionAt(s, i, o.names);
      if (m) { push(h('span.mention', m)); i += m.length; continue; }
    } else if (c === 'h' && !word(s[i - 1])) {
      URLRE.lastIndex = i;
      const m = URLRE.exec(s);
      if (m) {
        const url = m[0].replace(/[.,;:!?)\]]+$/, '');
        push(link(url, url)); i += url.length; continue;
      }
    } else if (/[A-Z]/.test(c) && o.isKey && !word(s[i - 1]) && o.onKey) {
      KEY.lastIndex = i;
      const m = KEY.exec(s);
      if (m && !word(s[i + m[0].length]) && s[i + m[0].length] !== '-' && o.isKey(m[0])) {
        const key = m[0];
        push(h('a.issue-ref', { href: '#/issue/' + key, onclick: e => { if (e.metaKey || e.ctrlKey) return; e.preventDefault(); o.onKey(key); } }, key));
        i += key.length; continue;
      }
    }
    buf += c; i++;
  }
  flush();
  return out;
}
const linkWith = (href, kids) => {
  const a = link(href, '');
  if (a.nodeType === 1) a.append(...kids);
  return a;
};

// ---- blocks
const FENCE = /^(\s*)```(\S*)\s*$/;
const HEADING = /^(#{1,6})\s+(.*)$/;
const LIST = /^(\s*)([-*+]|\d+[.)])\s+(.*)$/;
const PANEL = /^<!-- panel:([a-z]+) -->$/;
const EXPAND = /^<!-- expand(?::\s*(.*?))? -->$/;
const CLOSE = /^<!-- \/(panel|expand|block) -->$/;
const BLOCK = /^<!-- block:\d+.*-->$/;
const CARD = /^<!-- card: (.*?) -->$/;
const ROW = /^\s*\|.*\|\s*$/;
const SEP = /^\s*\|?\s*:?-{1,}:?\s*(\|\s*:?-{1,}:?\s*)*\|?\s*$/;

const isBlank = l => !l.trim();
const indentOf = l => l.length - l.trimStart().length;

function cells(line) {
  const t = line.trim().replace(/^\|/, '').replace(/\|$/, '');
  const out = []; let cur = '';
  for (let i = 0; i < t.length; i++) {
    if (t[i] === '\\' && t[i + 1] === '|') { cur += '|'; i++; } else if (t[i] === '|') { out.push(cur.trim()); cur = ''; } else cur += t[i];
  }
  out.push(cur.trim());
  return out;
}

function startsBlock(l) {
  return FENCE.test(l) || HEADING.test(l) || LIST.test(l) || /^>/.test(l) || /^---+\s*$/.test(l) || PANEL.test(l) || EXPAND.test(l) || CLOSE.test(l) || CARD.test(l) || /^<> /.test(l) || ROW.test(l);
}

// Index of the line closing the container that opened at lines[from-1].
function closer(lines, from) {
  let depth = 1;
  for (let i = from; i < lines.length; i++) {
    const l = lines[i].trim();
    if (PANEL.test(l) || EXPAND.test(l) || BLOCK.test(l)) depth++;
    else if (CLOSE.test(l) && --depth === 0) return i;
  }
  return lines.length;
}

function blocks(lines, o) {
  const out = [];
  let i = 0;
  while (i < lines.length) {
    const l = lines[i];
    if (isBlank(l)) { i++; continue; }
    let m;
    if ((m = FENCE.exec(l))) {
      const body = []; i++;
      while (i < lines.length && !FENCE.test(lines[i])) {
        body.push(lines[i].slice(Math.min(m[1].length, indentOf(lines[i]))));
        i++;
      }
      i++;
      out.push(h('pre', h('code', { dataset: m[2] ? { lang: m[2] } : null }, body.join('\n'))));
    } else if ((m = HEADING.exec(l))) {
      out.push(h('h' + Math.min(6, m[1].length + 2), { class: 'md-h' }, inline(m[2], o))); i++;
    } else if (/^---+\s*$/.test(l)) { out.push(h('hr')); i++; }
    else if (PANEL.test(l.trim())) {
      const end = closer(lines, i + 1);
      out.push(h('div.md-panel.p-' + PANEL.exec(l.trim())[1], blocks(lines.slice(i + 1, end), o))); i = end + 1;
    } else if ((m = EXPAND.exec(l.trim()))) {
      const end = closer(lines, i + 1);
      out.push(h('details.md-expand', h('summary', m[1] || 'Details'), blocks(lines.slice(i + 1, end), o))); i = end + 1;
    } else if (BLOCK.test(l.trim())) { i++; }
    else if (CLOSE.test(l.trim())) { i++; }
    else if ((m = CARD.exec(l.trim()))) { out.push(h('p', link(m[1], m[1]))); i++; }
    else if (/^>/.test(l)) {
      const q = [];
      while (i < lines.length && /^>/.test(lines[i])) q.push(lines[i++].replace(/^> ?/, ''));
      out.push(h('blockquote', blocks(q, o)));
    } else if (/^<> /.test(l)) {
      const d = [];
      while (i < lines.length && /^<> /.test(lines[i])) d.push(h('div.md-decision', inline(lines[i++].slice(3), o)));
      out.push(h('div.md-decisions', d));
    } else if (ROW.test(l) && i + 1 < lines.length && SEP.test(lines[i + 1]) && lines[i + 1].includes('-')) {
      const head = cells(l); i += 2;
      const rows = [];
      while (i < lines.length && ROW.test(lines[i])) rows.push(cells(lines[i++]));
      const empty = head.every(c => !c);
      out.push(h('div.md-table', h('table', !empty && h('thead', h('tr', head.map(c => h('th', inline(c, o))))),
        h('tbody', rows.map(r => h('tr', r.map(c => h('td', inline(c, o)))))))));
    } else if ((m = LIST.exec(l))) {
      i = list(lines, i, o, out);
    } else {
      const p = [l.trim()]; i++;
      while (i < lines.length && !isBlank(lines[i]) && !startsBlock(lines[i])) p.push(lines[i++].trim());
      out.push(h('p', inline(p.join('\n'), o)));
    }
  }
  return out;
}

// Reads the list starting at lines[i]; appends <ul>/<ol> to out; returns the next index.
function list(lines, i, o, out) {
  const base = indentOf(lines[i]);
  const ordered = /\d/.test(LIST.exec(lines[i])[2]);
  const start = ordered ? parseInt(LIST.exec(lines[i])[2], 10) : 1;
  const items = [];
  while (i < lines.length) {
    const m = LIST.exec(lines[i]);
    if (!m || indentOf(lines[i]) !== base || /\d/.test(m[2]) !== ordered) break;
    const body = [m[3]];
    const pad = base + m[2].length + 1;
    i++;
    while (i < lines.length) {
      const nx = lines[i];
      if (isBlank(nx)) {
        // A blank line keeps the item going only if more indented text follows.
        let j = i; while (j < lines.length && isBlank(lines[j])) j++;
        if (j < lines.length && indentOf(lines[j]) >= pad) { body.push(''); i++; continue; }
        break;
      }
      if (indentOf(nx) <= base && (LIST.test(nx) || startsBlock(nx))) break;
      if (indentOf(nx) <= base) { body.push(nx.trim()); i++; continue; }
      body.push(nx.slice(Math.min(pad, indentOf(nx)))); i++;
    }
    items.push(body);
    while (i < lines.length && isBlank(lines[i])) {
      const j = i + 1;
      if (j < lines.length && LIST.test(lines[j]) && indentOf(lines[j]) === base) i++; else break;
    }
  }
  const lis = items.map(body => {
    let first = body[0], box = null;
    const t = /^\[([ xX])\]\s+(.*)$/.exec(first);
    if (t) { first = t[2]; box = t[1] !== ' '; }
    const kids = blocks([first, ...body.slice(1)], o);
    if (box === null) return h('li', kids);
    const n = ++o.ctx.tasks;
    const input = h('input', { type: 'checkbox', checked: box, disabled: !o.onTask, dataset: { task: n } });
    if (o.onTask) input.addEventListener('change', () => o.onTask(n, o.ctx.tasks, input.checked, input));
    return h('li.task' + (box ? '.done' : ''), input, h('div.task-body', kids));
  });
  const tag = ordered ? 'ol' : 'ul';
  out.push(h(tag, { start: ordered && start !== 1 ? start : null, class: lis.some(li => li.classList.contains('task')) ? 'tasks' : null }, lis));
  return i;
}

export function render(md, opts = {}) {
  const o = { ...opts, ctx: { tasks: 0 } };
  const frag = document.createDocumentFragment();
  frag.append(...blocks(String(md || '').replace(/\r\n?/g, '\n').split('\n'), o));
  return frag;
}

// Plain text of markdown, for previews and titles.
export const plain = md => String(md || '').replace(/<!--.*?-->/g, '').replace(/!?\[([^\]]*)\]\([^)]*\)/g, '$1').replace(/[*_`~>#]/g, '').replace(/\s+/g, ' ').trim();
