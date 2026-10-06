// JQL completion, the TUI's rules (internal/ui/jql.go): fields, functions and keywords at the start of a clause,
// the field's values after an operator. Pure: the palette's # mode and lib/jqlinput.js use it.
export const OPS = ['=', '!=', '~', '!~', '>', '>=', '<', '<=', 'in', 'is', 'was', 'changed'];

// jqlContext reads what the end of s completes: a value of field after "field operator" (or inside "field in (…"),
// else a field or keyword. start is where the word being completed begins.
export function jqlContext(s) {
  let start = Math.max(s.lastIndexOf(' '), s.lastIndexOf('('), s.lastIndexOf(',')) + 1;
  if (((s.match(/"/g) || []).length) % 2 === 1) start = s.lastIndexOf('"');
  const prefix = s.slice(start).replace(/^"+|"+$/g, '');
  let head = s.slice(0, start);
  const open = head.lastIndexOf('(');
  if (open >= 0 && !head.slice(open).includes(')')) head = head.slice(0, open);
  const w = head.replace(/,/g, ' ').trim().split(/\s+/).filter(Boolean), n = w.length;
  const lw = i => w[i].toLowerCase(), isOp = i => OPS.includes(lw(i));
  if (n >= 3 && lw(n - 1) === 'in' && lw(n - 2) === 'not') return { field: w[n - 3], prefix, start, value: true };
  if (n >= 3 && lw(n - 1) === 'not' && isOp(n - 2)) return { field: w[n - 3], prefix, start, value: true };
  if (n >= 2 && isOp(n - 1)) return { field: w[n - 2], prefix, start, value: true };
  return { field: '', prefix, start, value: false };
}

// jqlComplete is s with its last word replaced by word, quoted when it has a space.
export function jqlComplete(s, word) {
  const { start } = jqlContext(s);
  if (word.includes(' ') && !word.startsWith('"')) word = '"' + word + '"';
  return s.slice(0, start) + word + ' ';
}

// jqlMatches are the words starting like prefix, then those containing it.
export function jqlMatches(words, prefix) {
  const p = prefix.toLowerCase(), head = [], rest = [];
  for (const w of words) {
    const l = w.replace(/^"|"$/g, '').toLowerCase();
    if (l.startsWith(p)) head.push(w); else if (p && l.includes(p)) rest.push(w);
  }
  return head.concat(rest);
}

// afterField is whether s.slice(0, start) ends in one of fields and a space: an operator comes next.
function afterField(fields, s, start) {
  if (start === 0 || s[start - 1] !== ' ') return false;
  const head = s.slice(0, start).trimEnd().toLowerCase();
  return fields.some(f => { const l = f.toLowerCase(); return head.endsWith(l) && /(^|[\s(])$/.test(head.slice(0, head.length - l.length)); });
}

// jqlWordsFor are the local completions of s as [{text, kind}] (words: /api/jql/words), and the context; when
// c.value, Jira's values of c.field go before them (/api/jql/values). After a field it offers the operators.
export function jqlWordsFor(words, s, max = 40) {
  const c = jqlContext(s);
  if (c.value) return { c, list: jqlMatches(words.Functions || [], c.prefix).map(text => ({ text, kind: 'function' })) };
  if (afterField(words.Fields || [], s, c.start)) return { c, list: jqlMatches(OPS, c.prefix).map(text => ({ text, kind: 'operator' })) };
  const tag = (arr, kind) => (arr || []).map(text => ({ text, kind }));
  const all = [...tag(words.Fields, 'field'), ...tag(words.Functions, 'function'), ...tag(words.Reserved, 'keyword')];
  const rank = new Map(jqlMatches(all.map(a => a.text), c.prefix).map((t, i) => [t, i]));
  return { c, list: all.filter(a => rank.has(a.text)).sort((a, b) => rank.get(a.text) - rank.get(b.text)).slice(0, max) };
}
