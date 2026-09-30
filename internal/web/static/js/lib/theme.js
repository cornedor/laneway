// Themes are CSS variable sets (css/themes.css) picked with data-theme on <html>.
// `auto` follows prefers-color-scheme. The accent and density are overrides.
export const presets = [
  { id: 'auto', name: 'System' },
  { id: 'light', name: 'Light' },
  { id: 'dark', name: 'Dark' },
  { id: 'nord', name: 'Nord' },
  { id: 'gruvbox', name: 'Gruvbox' },
  { id: 'solarized', name: 'Solarized' },
  { id: 'mono', name: 'Mono' },
];
export const accents = ['#5b8def', '#e5484d', '#f76b15', '#e0a100', '#30a46c', '#12a594', '#8e4ec6', '#d6409f'];
import { changed } from './metrics.js';
const root = document.documentElement;
const get = (k, d) => { try { return localStorage.getItem('lw:' + k) || d; } catch (e) { return d; } };
const set = (k, v) => { try { v == null ? localStorage.removeItem('lw:' + k) : localStorage.setItem('lw:' + k, v); } catch (e) { /* ignore */ } };

export const theme = {
  presets, accents,
  get current() { return get('theme', 'auto'); },
  get accent() { return get('accent', ''); },
  get density() { return get('density', 'normal'); },
  set(id) { if (id === 'auto') { delete root.dataset.theme; set('theme', null); } else { root.dataset.theme = id; set('theme', id); } },
  setAccent(c) { if (c) root.style.setProperty('--accent', c); else root.style.removeProperty('--accent'); set('accent', c || null); },
  setDensity(d) { root.dataset.density = d; set('density', d === 'normal' ? null : d); changed('density'); },
  // Cycle to the next preset (command palette / key).
  next() { const i = presets.findIndex(p => p.id === theme.current); theme.set(presets[(i + 1) % presets.length].id); return theme.current; },
};

// ---- display extras: font size, reduced motion, custom token overrides (all local to this browser)
const TOKENS = ['bg', 'bg-2', 'bg-3', 'fg', 'fg-2', 'border', 'accent', 'ok', 'warn', 'err'];
let customKeys = [];
const motionCss = document.createElement('style');
motionCss.textContent = ':root[data-motion="reduce"] *, :root[data-motion="reduce"] *::before, :root[data-motion="reduce"] *::after { animation: none !important; transition: none !important; }';
document.head.append(motionCss);
// defineProperties, not assign: assign would freeze the getters' values at load.
Object.defineProperties(theme, Object.getOwnPropertyDescriptors({
  get fontSize() { return Number(get('fs', '')) || 0; },
  setFontSize(px) { if (px) root.style.setProperty('--fs', px + 'px'); else root.style.removeProperty('--fs'); set('fs', px ? String(px) : null); changed('fontsize'); },
  // The size in use right now (px), whatever set it: the setting, the density or the phone breakpoint.
  get effectiveFontSize() { return parseFloat(getComputedStyle(root).fontSize) || 14; },
  get motion() { return get('motion', 'auto'); },
  setMotion(m) { if (m === 'reduce') root.dataset.motion = 'reduce'; else delete root.dataset.motion; set('motion', m === 'reduce' ? 'reduce' : null); },
  // Custom overrides: {'--bg': '#000', …}; anything not starting with "--" is ignored.
  get custom() { try { return JSON.parse(get('custom', '{}')) || {}; } catch (e) { return {}; } },
  setCustom(obj) {
    for (const k of customKeys) root.style.removeProperty(k);
    customKeys = [];
    const clean = {};
    for (const [k, v] of Object.entries(obj || {})) if (/^--[\w-]+$/.test(k) && typeof v === 'string') { root.style.setProperty(k, v); customKeys.push(k); clean[k] = v; }
    set('custom', customKeys.length ? JSON.stringify(clean) : null);
  },
  // The colours of a preset, for swatches: {bg, fg, accent, …}. Read with the local overrides lifted.
  tokens(id) {
    const keep = root.dataset.theme, inline = ['--accent', ...customKeys].map(k => [k, root.style.getPropertyValue(k)]);
    for (const [k] of inline) root.style.removeProperty(k);
    if (id === 'auto') delete root.dataset.theme; else root.dataset.theme = id;
    const cs = getComputedStyle(root), out = {};
    for (const t of TOKENS) out[t] = cs.getPropertyValue('--' + t).trim();
    if (keep) root.dataset.theme = keep; else delete root.dataset.theme;
    for (const [k, v] of inline) if (v) root.style.setProperty(k, v);
    return out;
  },
}));
if (theme.fontSize) theme.setFontSize(theme.fontSize);
if (theme.motion === 'reduce') theme.setMotion('reduce');
theme.setCustom(theme.custom);

import fonts from './fonts.js'; // applies --font-ui / --font-mono at load
theme.fonts = fonts;
fonts.on(() => changed('font'));

export default theme;
