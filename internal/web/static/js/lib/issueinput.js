// An issue key input that searches as you type (/find): a key, words of the summary, or a bare number in
// o.project. With o.parentOf (an issue key), or o.project and o.type (a new issue's), it offers only the
// issues that can be its parent, the open ones as soon as it has the focus. The list is combo.js's. The
// issue the key names shows under it.
import { h, clear, debounce } from './dom.js';
import api from './api.js';
import { combo } from './combo.js';
import { statusPill } from './badge.js';
import { typeBadge } from './card.js';
import { T } from './i18n.js';

const KEY_RE = /^[A-Z][A-Z0-9_]*-\d+$/;
const catOf = c => (c.Done ? 'done' : c.InProgress ? 'indeterminate' : 'new');
const row = c => [typeBadge(c), h('b.iin-key', c.Key), h('span.iin-text', c.Summary), c.Status && statusPill(c.Status, catOf(c))];

// issueInput(o) → {el, input, get() → the key typed, '' for none}: o.value, o.placeholder, o.label,
// o.parentOf, o.project, o.type, o.oninput(key).
export function issueInput(o = {}) {
  const input = h('input.input.mono.iin-in', { type: 'text', value: o.value || '', placeholder: o.placeholder || T('Key or words of its summary'),
    spellcheck: false, autocomplete: 'off', autocapitalize: 'off', 'aria-label': o.label || T('Issue') });
  const chosen = h('div.iin-sum');
  const scoped = !!(o.parentOf || o.type);
  const known = new Map(); // key → its card, from the lists shown
  const get = () => input.value.trim().toUpperCase();

  // The issue the key names, under the input: its type, summary and status, or why it can't be.
  const show = debounce(() => {
    const k = get();
    if (!k) return clear(chosen);
    if (!KEY_RE.test(k)) return clear(chosen).append(h('span.faint', T('Not an issue key')));
    const c = known.get(k);
    if (c) return clear(chosen).append(...row(c));
    find(k).then(cs => {
      if (k !== get()) return;
      const hit = cs.find(x => x.Key === k);
      clear(chosen).append(...(hit ? row(hit) : [h('span.faint', scoped ? T('%s can’t be the parent, or doesn’t exist', k) : T('%s not found', k))]));
    }, () => {});
  }, 250);

  const find = q => {
    const p = new URLSearchParams({ q, n: '15' });
    if (o.parentOf) p.set('parentOf', o.parentOf);
    else { if (o.project) p.set('project', o.project); if (o.type) p.set('type', o.type); }
    return api.get('/find?' + p).then(d => {
      const cs = (d.cards || []).filter(c => c.Key !== o.parentOf);
      for (const c of cs) known.set(c.Key, c);
      return cs;
    });
  };

  const cb = combo(input, {
    find, row, key: c => c.Key, open: scoped,
    none: q => (q ? T('No issue matches “%s”', q) : T('Nothing open to choose')),
    accept: c => {
      input.value = c.Key;
      clear(chosen).append(...row(c));
      if (o.oninput) o.oninput(c.Key);
    },
  });

  input.addEventListener('input', () => { show(); if (o.oninput) o.oninput(get()); });
  if (get()) show();

  return { el: h('div.iin', cb.el, chosen), input, get };
}
