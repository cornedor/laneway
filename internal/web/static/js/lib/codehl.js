// Code blocks coloured by language: the server's highlighter (POST /api/highlight, chroma, as the
// merge request diff's) answers a block's tokens, kept here by language and text.
//
//   spans(lang, code) → Promise of [[from, to, class], …] (offsets in the string), cached(lang, code) → them or undefined,
//   paint(codeEl, lang): a drawn <code>'s text wrapped in its tokens' spans, once they come.
import { api } from './api.js';
import { h } from './dom.js';

const cache = new Map(); // lang \0 code → spans (or the promise of them)
const MAX = 300;
const key = (lang, code) => String(lang).toLowerCase() + '\0' + code;

export const cached = (lang, code) => { const v = cache.get(key(lang, code)); return Array.isArray(v) ? v : undefined; };

export function spans(lang, code) {
  if (!lang || !code) return Promise.resolve([]);
  const k = key(lang, code), v = cache.get(k);
  if (v) return Array.isArray(v) ? Promise.resolve(v) : v;
  if (cache.size >= MAX) cache.delete(cache.keys().next().value);
  const p = api.post('/highlight', { Lang: lang, Code: code }).then(r => (r && r.Spans) || [], () => []).then(s => { cache.set(k, s); return s; });
  cache.set(k, p);
  return p;
}

export function paint(code, lang) {
  const text = code.textContent;
  spans(lang, text).then(sp => {
    if (!sp.length || code.textContent !== text) return;
    const out = [];
    let at = 0;
    for (const [a, b, cls] of sp) {
      if (a > at) out.push(text.slice(at, a));
      out.push(h('span.' + cls, text.slice(a, b)));
      at = b;
    }
    if (at < text.length) out.push(text.slice(at));
    code.replaceChildren(...out);
  });
}
