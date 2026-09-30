// Fonts: a UI face and a monospace face, applied as --font-ui / --font-mono on <html> (css/base.css uses only those).
// Prefs (app.prefs, mirrored in localStorage as lw:p:<name>; boot.js applies the resolved result before first paint):
//   font.ui / font.mono            preset id ('system', 'inter', …), 'custom', or 'file:<uploaded name>'
//   font.ui.custom / font.mono.custom   any CSS font-family list for 'custom', e.g. 'Berkeley Mono', "Iosevka"
//   font.ligatures                 'on' (default) | 'off'   (font-variant-ligatures in code)
//   font.lh                        line height multiplier, '' = 1.45
//   terminal.font                  (for the browser terminal) CSS font-family, '' = follow the monospace font
// For the terminal: fonts.terminalStack() (family list to hand to the terminal), fonts.ready() (promise: the face is
// loaded, safe to measure cells), fonts.on(fn) (fn() on any change; bus 'prefs' {key:'font.*'} fires too).
export const SYS_UI = 'system-ui, -apple-system, "Segoe UI", Roboto, sans-serif';
export const SYS_MONO = 'ui-monospace, "SF Mono", "JetBrains Mono", Menlo, Consolas, monospace';
const p = (id, name, note = '') => ({ id, name, note, stack: '"' + name + '", ' });
export const uiPresets = [
  { id: 'system', name: 'System UI', stack: '' },
  p('inter', 'Inter', 'neutral, tall x-height'), p('plex-sans', 'IBM Plex Sans'), p('atkinson', 'Atkinson Hyperlegible Next', 'built for legibility'),
  p('source-sans', 'Source Sans 3'), p('lexend', 'Lexend', 'easier reading'),
];
export const monoPresets = [
  { id: 'system', name: 'System mono', stack: '' },
  p('jetbrains', 'JetBrains Mono', 'ligatures'), p('fira', 'Fira Code', 'ligatures'), p('cascadia', 'Cascadia Code', 'ligatures'),
  p('plex-mono', 'IBM Plex Mono'), p('source-code', 'Source Code Pro'),
];
const ROLES = { ui: { presets: uiPresets, sys: SYS_UI }, mono: { presets: monoPresets, sys: SYS_MONO } };
const FILE = /^[\w.-]{1,64}$/;
const root = document.documentElement;
let prefs = null; // app.prefs once attached
const listeners = new Set();

const ls = {
  get: k => { try { return localStorage.getItem('lw:p:' + k) || ''; } catch (e) { return ''; } },
  set: (k, v) => { try { v ? localStorage.setItem('lw:p:' + k, v) : localStorage.removeItem('lw:p:' + k); } catch (e) { /* ignore */ } },
};
const get = k => ls.get(k);
function set(k, v) { ls.set(k, v); if (prefs) prefs.set(k, v); }

// A user-typed family list goes into a CSS variable only if it cannot break out of the declaration.
export const validFamily = s => !!s && s.length <= 200 && !/[;{}<>\\@]|url\(|\/\*|expression/i.test(s);

export function stack(role) {
  const r = ROLES[role], id = get('font.' + role) || 'system';
  if (id === 'custom') { const c = get('font.' + role + '.custom').trim(); return validFamily(c) ? c + ', ' + r.sys : r.sys; }
  if (id.startsWith('file:') && FILE.test(id.slice(5))) return '"' + upName(id.slice(5)) + '", ' + r.sys;
  const pr = r.presets.find(x => x.id === id);
  return pr && pr.stack ? pr.stack + r.sys : r.sys;
}
export const uiStack = () => stack('ui');
export const monoStack = () => stack('mono');
export function terminalStack() { const t = get('terminal.font').trim(); return validFamily(t) ? t + ', ' + monoStack() : monoStack(); }
export const ligatures = () => get('font.ligatures') !== 'off';
export const lineHeight = () => Number(get('font.lh')) || 0;

// The stack of a preset for previews: "Inter", system-ui…
export const presetStack = (role, pr) => (pr.stack ? pr.stack + ROLES[role].sys : ROLES[role].sys);

export function apply() {
  const s = root.style, ui = uiStack(), mono = monoStack(), lig = ligatures(), lh = lineHeight();
  s.setProperty('--font-ui', ui); s.setProperty('--font-mono', mono);
  s.setProperty('--lig', lig ? 'normal' : 'none');
  if (lh) s.setProperty('--lh', String(lh)); else s.removeProperty('--lh');
  try { localStorage.setItem('lw:fontcss', JSON.stringify({ ui, mono, lig: lig ? '' : 'none', lh: lh || '' })); } catch (e) { /* ignore */ }
  for (const fn of listeners) { try { fn(); } catch (e) { console.error('fonts', e); } }
}
export const on = fn => { listeners.add(fn); return () => listeners.delete(fn); };
// Resolves once the faces of the current stacks are loaded (document.fonts), or after a short wait.
export function ready() {
  const fams = [...new Set([...monoStack().split(','), ...uiStack().split(',')].map(x => x.trim()).filter(x => x.startsWith('"')))];
  const all = Promise.all(fams.map(f => document.fonts.load('400 14px ' + f).catch(() => [])));
  return Promise.race([all, new Promise(r => setTimeout(r, 2500))]);
}

export function setChoice(role, id) { set('font.' + role, id === 'system' ? '' : id); apply(); }
export function setCustom(role, fam) { set('font.' + role + '.custom', fam.trim()); apply(); }
export function setLigatures(on) { set('font.ligatures', on ? '' : 'off'); apply(); }
export function setLineHeight(v) { set('font.lh', v ? String(v) : ''); apply(); }
export const choice = role => get('font.' + role) || 'system';
export const custom = role => get('font.' + role + '.custom');
export function reset() { for (const k of ['font.ui', 'font.mono', 'font.ui.custom', 'font.mono.custom', 'font.ligatures', 'font.lh']) set(k, ''); apply(); }

// Uploaded fonts (POST /api/fonts): declared as faces named lw-upload-<name>.
const upName = n => 'lw-upload-' + n;
const faceCss = document.createElement('style');
document.head.append(faceCss);
let uploaded = [];
export const files = () => uploaded;
export function declare(list) {
  uploaded = (list || []).filter(f => FILE.test(f.Name));
  faceCss.textContent = uploaded.map(f => '@font-face{font-family:"' + upName(f.Name) + '";src:url("/fonts/custom/' + encodeURIComponent(f.Name) + '");font-display:swap}').join('\n');
  apply();
}
export async function refreshFiles(api) { declare(await api.get('/fonts', { fresh: true })); return uploaded; }
export async function upload(name, blob) {
  const r = await fetch('/api/fonts?name=' + encodeURIComponent(name), { method: 'POST', body: blob, headers: { 'Content-Type': 'application/octet-stream' } });
  const d = await r.json().catch(() => ({}));
  if (!r.ok) throw new Error(d.error || 'upload failed (' + r.status + ')');
  declare(d); return d;
}
export async function remove(name) {
  const r = await fetch('/api/fonts/' + encodeURIComponent(name), { method: 'DELETE' });
  const d = await r.json().catch(() => ({}));
  if (!r.ok) throw new Error(d.error || 'delete failed (' + r.status + ')');
  declare(d); return d;
}
export const fileFamily = n => upName(n);

// Is the first family of a CSS list usable? A declared face (bundled/uploaded) or an installed font,
// told apart from the fallback by measuring text (document.fonts.check answers true for unknown names).
export function available(list) {
  const first = (list || '').split(',')[0].trim().replace(/^["']|["']$/g, '');
  if (!first) return null;
  if (/^(serif|sans-serif|monospace|cursive|fantasy|system-ui|ui-[\w-]+)$/i.test(first)) return true;
  for (const f of document.fonts) if (f.family.replace(/^["']|["']$/g, '') === first) return true;
  const c = document.createElement('canvas').getContext('2d');
  if (!c) return null;
  const txt = 'mmmmmmmmmmlliWi10@#', q = '"' + first.replace(/"/g, '') + '"';
  return ['monospace', 'serif', 'sans-serif'].some(b => { c.font = '40px ' + b; const w0 = c.measureText(txt).width; c.font = '40px ' + q + ', ' + b; return c.measureText(txt).width !== w0; });
}

// Another browser / the server store may hold newer values: adopt them (app.prefs.data after load).
export function attach(pf) {
  prefs = pf;
  let changed = false;
  for (const k of ['font.ui', 'font.mono', 'font.ui.custom', 'font.mono.custom', 'font.ligatures', 'font.lh', 'terminal.font']) {
    const v = pf.data && k in pf.data ? String(pf.data[k]) : null;
    if (v !== null && v !== get(k)) { ls.set(k, v); changed = true; }
  }
  if (changed) apply();
}

export const fonts = { uiPresets, monoPresets, stack, uiStack, monoStack, terminalStack, ligatures, lineHeight, apply, on, ready, setChoice, setCustom, setLigatures, setLineHeight, choice, custom, reset, files, declare, refreshFiles, upload, remove, fileFamily, available, validFamily, attach, presetStack };
apply();
export default fonts;
