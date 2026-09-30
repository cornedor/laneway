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
  setDensity(d) { root.dataset.density = d; set('density', d === 'normal' ? null : d); },
  // Cycle to the next preset (command palette / key).
  next() { const i = presets.findIndex(p => p.id === theme.current); theme.set(presets[(i + 1) % presets.length].id); return theme.current; },
};
export default theme;
