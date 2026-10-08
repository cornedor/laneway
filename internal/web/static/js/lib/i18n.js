// Translations: T('Open issue') is the text in ui.language, as the TUI's i18n.T; the English text is the key.
// The catalog comes from /api/i18n.js (a plain script before the modules), so T works at a module's top level too.
// With arguments the text is a format: %s %d %v take them in order, %% is a percent sign (as Go's Sprintf).
const cat = () => globalThis.LANEWAY_I18N || {};

const fill = (f, args) => { let i = 0; return f.replace(/%(?:\[\d+\])?[-+# 0]*\d*(?:\.\d+)?([sdvqfx%])/g, (m, v) => v === '%' ? '%' : String(args[i++])); };

export function T(msg, ...args) {
  const s = cat()[msg] || msg;
  return args.length ? fill(s, args) : s;
}

// Tn picks one (n is 1) or other, translated, formatted with args: Tn(n, '%d issue', '%d issues', n).
export const Tn = (n, one, other, ...args) => T(n === 1 ? one : other, ...args);

// lang is 'en' or the catalog's language.
export const lang = () => cat()[''] || 'en';
