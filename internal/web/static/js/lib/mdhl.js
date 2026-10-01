// Markdown as it is typed: highlight(text) wraps the source in spans (markers
// .mk, content by role) without changing a character, so the editor's text stays
// the markdown; the editing helpers answer what enter, tab, a task box click and
// a pasted link do to it ({from, to, text, a, b}: replace [from, to) with text,
// then select [a, b)). Pure: no DOM, tested with node (internal/web/jstest).

const esc = s => s.replace(/[&<>"]/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' })[c]);
const span = (cls, inner) => '<span class="' + cls + '">' + inner + '</span>';
const mk = s => span('mk', esc(s));
const word = c => c != null && /[\p{L}\p{N}_]/u.test(c);

const STATUS = new Set(['neutral', 'purple', 'blue', 'red', 'yellow', 'green']);

// Inline rules, tried in order where a character could start one. pre: what the
// character before must not be.
const INLINE = [
  { re: /\\[\\`*_{}[\]()#+\-.!~|<>@:]/y, out: m => mk('\\') + esc(m[0][1]) },
  { re: /(`+)([^`\n]+?)\1(?!`)/y, out: m => span('hl-code', mk(m[1]) + esc(m[2]) + mk(m[1])) },
  { re: /(!?\[)([^\]\n]*)(\]\()([^)\s]*)((?: "[^"\n]*")?\))/y, out: (m, o) => mk(m[1]) + span('hl-lt', inline(m[2], o)) + mk(m[3]) + span('hl-url', esc(m[4])) + mk(m[5]) },
  { re: /\*\*\*(?=\S)([^\n]*?\S)\*\*\*/y, out: (m, o) => mk('***') + span('hl-b hl-i', inline(m[1], o)) + mk('***') },
  { re: /\*\*(?=\S)([^\n]*?\S)\*\*/y, out: (m, o) => mk('**') + span('hl-b', inline(m[1], o)) + mk('**') },
  { re: /__(?=\S)([^\n]*?\S)__(?![\p{L}\p{N}_])/uy, pre: word, out: (m, o) => mk('__') + span('hl-b', inline(m[1], o)) + mk('__') },
  { re: /\*(?=[^\s*])([^\n]*?[^\s*])\*(?!\*)/y, out: (m, o) => mk('*') + span('hl-i', inline(m[1], o)) + mk('*') },
  { re: /_(?=[^\s_])([^\n]*?[^\s_])_(?![\p{L}\p{N}_])/uy, pre: word, out: (m, o) => mk('_') + span('hl-i', inline(m[1], o)) + mk('_') },
  { re: /~~(?=\S)([^\n]*?\S)~~/y, out: (m, o) => mk('~~') + span('hl-s', inline(m[1], o)) + mk('~~') },
  { re: /<u>([^\n]*?)<\/u>/y, out: (m, o) => mk('<u>') + span('hl-u', inline(m[1], o)) + mk('</u>') },
  { re: /<(sub|sup)>([^\n]*?)<\/\1>/y, out: (m, o) => mk('<' + m[1] + '>') + inline(m[2], o) + mk('</' + m[1] + '>') },
  { re: /<status color="([a-z]+)">([^<\n]*)<\/status>/y, out: m => mk(m[0].slice(0, m[0].indexOf('>') + 1)) + span('hl-status' + (STATUS.has(m[1]) ? ' c-' + m[1] : ''), esc(m[2])) + mk('</status>') },
  { re: /<date>([^<\n]*)<\/date>/y, out: m => mk('<date>') + span('hl-date', esc(m[1])) + mk('</date>') },
  { re: /<span style="color:\s*(#[0-9a-fA-F]{3,8})">([^\n]*?)<\/span>/y, out: (m, o) => mk(m[0].slice(0, m[0].indexOf('>') + 1)) + '<span class="md-col" style="--c:' + m[1] + '">' + inline(m[2], o) + '</span>' + mk('</span>') },
  { re: /<!--[^\n]*?-->/y, out: m => span(/^<!-- ?keep:/.test(m[0]) ? 'hl-keep' : 'hl-tag', esc(m[0])) },
  { re: /https?:\/\/[^\s<>()]*[^\s<>().,;:!?'"\]]/y, pre: c => word(c) || c === '/', out: m => span('hl-url', esc(m[0])) },
  { re: /:[a-z0-9_+-]{2,}:/y, pre: word, out: m => span('hl-emo', esc(m[0])) },
  { re: /[A-Z][A-Z0-9_]+-\d+(?![\p{L}\p{N}_-])/uy, pre: c => word(c) || c === '-', out: m => span('hl-key', esc(m[0])) },
];
const STARTS = /[\\`!*[_~<h:@A-Z]/;

function inline(s, o) {
  let out = '', plain = 0;
  for (let i = 0; i < s.length;) {
    const c = s[i];
    let hit = null;
    if (STARTS.test(c)) {
      if (c === '@' && o.names) {
        const n = o.names.find(n => s.startsWith(n, i + 1) && !word(s[i + 1 + n.length]));
        if (n && !word(s[i - 1])) hit = { len: n.length + 1, html: span('hl-at', esc('@' + n)) };
      }
      for (const r of INLINE) {
        if (hit) break;
        if (r.pre && r.pre(s[i - 1])) continue;
        r.re.lastIndex = i;
        const m = r.re.exec(s);
        if (m) hit = { len: m[0].length, html: r.out(m, o) };
      }
    }
    if (!hit) { i++; continue; }
    out += esc(s.slice(plain, i)) + hit.html;
    i += hit.len; plain = i;
  }
  return out + esc(s.slice(plain));
}

const FENCE = /^ {0,3}(`{3,}|~{3,})/;
const LIST = /^(\s*)(?:([-*+])|(\d{1,9})([.)]))( +)(\[[ xX]\] )?/;
const QUOTE = /^\s*(?:> ?)+/;
const DELIM = /^\s*\|?\s*:?-+:?\s*(?:\|\s*:?-+:?\s*)*\|?\s*$/;
const isRow = l => /^\s*\|/.test(l) && (l.match(/(?<!\\)\|/g) || []).length > 1;

// The bars of a table row dimmed, its cells inline; th: a header row (bold).
function row(l, o, th) {
  const parts = l.split(/(?<!\\)\|/);
  return parts.map((p, i) => (i ? mk('|') : '') + (th && p.trim() ? span('hl-th', inline(p, o)) : inline(p, o))).join('');
}

// highlight(text, {names}) → HTML; names: people as "@Name" mentions show.
export function highlight(text, o = {}) {
  const lines = text.split('\n');
  const out = [];
  let fence = '';
  for (let i = 0; i < lines.length; i++) {
    const l = lines[i];
    const f = FENCE.exec(l);
    if (fence) {
      if (f && f[1][0] === fence[0] && f[1].length >= fence.length && !l.slice(f[0].length).trim()) { fence = ''; out.push(span('hl-fence', esc(l))); } else out.push(l ? span('hl-pre', esc(l)) : '');
      continue;
    }
    if (f) { fence = f[1]; out.push(span('hl-fence', esc(l))); continue; }
    let m = /^(#{1,6})( +)(.*)$/.exec(l);
    if (m) { out.push(span('hl-h hl-h' + m[1].length, mk(m[1] + m[2]) + inline(m[3], o))); continue; }
    if (/^ {0,3}([-*_])(?: *\1){2,} *$/.test(l)) { out.push(span('hl-hr', esc(l))); continue; }
    if (/^\s*<!--.*-->\s*$/.test(l)) { out.push(span(/^\s*<!-- ?keep:/.test(l) ? 'hl-keep' : 'hl-tag', esc(l))); continue; }
    if (isRow(l) || (l.includes('|') && DELIM.test(lines[i + 1] || '') && lines[i + 1].includes('|'))) {
      out.push(span('hl-tr', DELIM.test(l) ? mk(l) : row(l, o, DELIM.test(lines[i + 1] || '') && (lines[i + 1] || '').includes('-'))));
      continue;
    }
    m = QUOTE.exec(l);
    if (m && m[0].includes('>')) { out.push(span('hl-q', mk(m[0]) + inline(l.slice(m[0].length), o))); continue; }
    m = LIST.exec(l);
    if (m) {
      const box = m[6] ? span('hl-box' + (m[6][1] === ' ' ? '' : ' on'), esc(m[6].slice(0, 3))) + ' ' : '';
      const body = inline(l.slice(m[0].length), o);
      out.push(esc(m[1]) + span('hl-li', esc(m[2] || m[3] + m[4])) + m[5] + box + (m[6] && m[6][1] !== ' ' ? span('hl-done', body) : body));
      continue;
    }
    if (l.startsWith('<> ')) { out.push(span('hl-li', esc('<>')) + ' ' + inline(l.slice(3), o)); continue; }
    out.push(inline(l, o));
  }
  return out.join('\n');
}

// ---- editing

// listItem(line) → {indent, bullet, num, delim, task, body, width} or null; body: where
// the item's text starts, width: the marker's columns (its children's indent).
export function listItem(line) {
  const m = LIST.exec(line);
  if (!m) return null;
  return { indent: m[1], bullet: m[2] || '', num: m[3] ? +m[3] : 0, delim: m[4] || '', task: !!m[6], body: m[0].length, width: m[0].length - m[1].length - (m[6] ? m[6].length : 0) };
}
const nextMarker = it => it.indent + (it.bullet || it.num + 1 + it.delim) + ' ' + (it.task ? '[ ] ' : '');

const lineAt = (v, i) => { const s = v.lastIndexOf('\n', i - 1) + 1; let e = v.indexOf('\n', i); if (e < 0) e = v.length; return [s, e]; };

// inFence: is position i inside a fenced code block?
export function inFence(v, i) {
  let fence = '';
  for (const l of v.slice(0, v.lastIndexOf('\n', i - 1) + 1).split('\n')) {
    const f = FENCE.exec(l);
    if (!f) continue;
    if (!fence) fence = f[1];
    else if (f[1][0] === fence[0] && f[1].length >= fence.length && !l.slice(f[0].length).trim()) fence = '';
  }
  return !!fence;
}

// enter: a list item opens the next one (an empty one ends the list), a quote
// line continues the quote, other lines keep their indent.
export function enter(v, a, b) {
  const [s, e] = lineAt(v, a);
  const line = v.slice(s, e);
  const nl = t => ({ from: a, to: b, text: t, a: a + t.length, b: a + t.length });
  const indent = /^[ \t]*/.exec(line)[0];
  if (inFence(v, s)) return nl('\n' + indent.slice(0, a - s));
  const it = listItem(line);
  if (it && a - s >= it.body) {
    if (a === b && !line.slice(it.body).trim()) return { from: s, to: e, text: '', a: s, b: s };
    return nl('\n' + nextMarker(it));
  }
  const q = QUOTE.exec(line);
  if (q && q[0].includes('>') && a - s >= q[0].length) {
    if (a === b && !line.slice(q[0].length).trim()) return { from: s, to: e, text: '', a: s, b: s };
    return nl('\n' + q[0]);
  }
  return nl('\n' + indent.slice(0, a - s));
}

// indent (out: false) or outdent (out: true) the list items in [a, b): one level
// under the item above, or back to its parent's. null when the caret is not on
// a list item (tab then leaves the field).
export function indent(v, a, b, out) {
  const [s] = lineAt(v, a), [, e] = lineAt(v, b > a && v[b - 1] === '\n' ? b - 1 : b);
  const lines = v.slice(s, e).split('\n');
  if (!listItem(lines[0])) return null;
  const before = v.slice(0, s).split('\n').slice(0, -1);
  let first = 0, total = 0;
  lines.forEach((l, i) => {
    const it = listItem(l);
    if (!it) return;
    let to = it.indent;
    for (let j = before.length + i - 1; j >= 0; j--) {
      const pl = j < before.length ? before[j] : lines[j - before.length];
      const p = listItem(pl);
      if (!p) {
        if (/^\S/.test(pl)) break; // a paragraph ends the list
        continue;
      }
      if (out ? p.indent.length < it.indent.length : p.indent.length <= it.indent.length) {
        to = out ? p.indent : p.indent.length === it.indent.length ? it.indent + ' '.repeat(p.width) : it.indent;
        break;
      }
    }
    if (out && to === it.indent) to = '';
    const d = to.length - it.indent.length;
    lines[i] = to + l.slice(it.indent.length);
    if (i === 0) first = d;
    total += d;
  });
  const text = lines.join('\n');
  if (text === v.slice(s, e)) return { from: s, to: e, text, a, b };
  return a === b ? { from: s, to: e, text, a: Math.max(s, a + first), b: Math.max(s, a + first) } : { from: s, to: e, text, a: Math.max(s, a + first), b: b + total };
}

// toggleTask: the "[ ]" at i ticked, or unticked.
export function toggleTask(v, i) {
  if (!/^\[[ xX]\]$/.test(v.slice(i, i + 3))) return null;
  const t = v[i + 1] === ' ' ? 'x' : ' ';
  return { from: i + 1, to: i + 2, text: t, a: i + 3, b: i + 3 };
}

// pasteLink: a URL pasted over selected text links that text.
export function pasteLink(v, a, b, text) {
  const url = text.trim(), sel = v.slice(a, b);
  if (a === b || !/^https?:\/\/\S+$/.test(url) || sel.includes('\n') || /^https?:\/\//.test(sel)) return null;
  const t = '[' + sel + '](' + url + ')';
  return { from: a, to: b, text: t, a: a + t.length, b: a + t.length };
}
