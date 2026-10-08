// A JQL text input with completions under it (lib/jql.js): fields, operators, functions, keywords and Jira's
// values for the field, at the caret. ↑↓ pick, tab or enter takes one, esc closes the list (dismiss()),
// ctrl+space opens it.
import { h, clear, debounce } from './dom.js';
import api from './api.js';
import { jqlComplete, jqlMatches, jqlWordsFor } from './jql.js';
import { T } from './i18n.js';

const KIND = { function: T('function'), value: T('value'), field: T('field'), operator: T('operator'), keyword: T('keyword') };
let words = null; // /api/jql/words, once a page
let shut = null; // closes the list that is open

const loadWords = then => {
  if (words) return then();
  words = { Fields: [], Functions: [], Reserved: [] };
  api.swr('/jql/words', d => { words = d; then(); }).catch(() => {});
};

// dismiss closes the open completion list; true when there was one, so esc closes it before it leaves an editor.
export function dismiss() {
  if (!shut) return false;
  shut();
  return true;
}

// jqlInput → {el, input}: o.value, o.placeholder, o.label, o.oninput(value).
export function jqlInput(o = {}) {
  const input = h('input.input.mono.jqi-in', { type: 'text', value: o.value || '', placeholder: o.placeholder || '', spellcheck: false, autocomplete: 'off',
    'aria-label': o.label || 'JQL', role: 'combobox', 'aria-autocomplete': 'list', 'aria-expanded': 'false' });
  const pop = h('div.mention-pop.jqi-pop', { role: 'listbox', hidden: true });
  let items = [], sel = 0, seq = 0;
  const caret = () => input.selectionStart == null ? input.value.length : input.selectionStart;
  const close = () => { seq++; pop.hidden = true; items = []; input.setAttribute('aria-expanded', 'false'); if (shut === close) shut = null; };
  const paint = () => {
    if (!items.length || document.activeElement !== input) { close(); return; }
    clear(pop).append(...items.map((it, i) => h('div.mp' + (i === sel ? '.sel' : ''), { role: 'option', 'aria-selected': String(i === sel), onmousedown: e => { e.preventDefault(); accept(i); } },
      h('span.mono.jqi-word', it.text), h('span.chip', KIND[it.kind] || it.kind))));
    pop.hidden = false; input.setAttribute('aria-expanded', 'true');
    if (shut && shut !== close) shut();
    shut = close;
    const s = pop.querySelector('.sel'); if (s) s.scrollIntoView({ block: 'nearest' });
  };
  const values = debounce((c, n) => {
    api.get('/jql/values?field=' + encodeURIComponent(c.field) + '&prefix=' + encodeURIComponent(c.prefix)).then(d => {
      if (n !== seq) return;
      const fns = jqlMatches(words.Functions || [], c.prefix).map(text => ({ text, kind: 'function' }));
      items = (d || []).map(text => ({ text, kind: 'value' })).concat(fns).slice(0, 40); sel = 0; paint();
    }, () => {});
  }, 150);
  const suggest = (force) => {
    const s = input.value.slice(0, caret());
    if (!force && !s.trim()) { close(); return; }
    const n = ++seq, { c, list } = jqlWordsFor(words, s);
    items = list; sel = 0; paint();
    if (c.value) values(c, n);
  };
  function accept(i) {
    const it = items[i]; if (!it) return;
    const at = caret(), head = jqlComplete(input.value.slice(0, at), it.text);
    input.value = head + input.value.slice(at).replace(/^[^\s(),]*\s*/, '');
    input.setSelectionRange(head.length, head.length);
    if (o.oninput) o.oninput(input.value);
    suggest();
  }
  input.addEventListener('input', () => { if (o.oninput) o.oninput(input.value); loadWords(() => { if (document.activeElement === input) suggest(); }); });
  input.addEventListener('blur', close);
  input.addEventListener('keydown', e => {
    if (e.key === ' ' && e.ctrlKey) { e.preventDefault(); loadWords(() => suggest(true)); return; }
    if (pop.hidden) return;
    const step = e.key === 'ArrowDown' ? 1 : e.key === 'ArrowUp' ? -1 : 0;
    if (step) { e.preventDefault(); sel = (sel + step + items.length) % items.length; paint(); return; }
    if ((e.key === 'Tab' && !e.shiftKey) || e.key === 'Enter') { e.preventDefault(); e.stopPropagation(); accept(sel); }
  });
  return { el: h('div.jqi', input, pop), input };
}
