// Per-site localStorage. Jira project keys, board ids, issue keys and the like differ per site, so
// anything remembered about them is stored under the site it was learned on. `setSite` runs at
// boot, before anything reads state. Theme, fonts and the key registry are global and use localStorage directly.
let site = '';
const k = key => 'lw:s:' + site + ':' + key;

export function setSite(s) {
  site = s || '';
  migrate();
}
export const key = x => k(x);
export const siteName = () => site;

export function get(key, d = '') { try { const v = localStorage.getItem(k(key)); return v == null ? d : v; } catch (e) { return d; } }
export function set(key, v) { try { v == null || v === '' ? localStorage.removeItem(k(key)) : localStorage.setItem(k(key), String(v)); } catch (e) { /* private mode, quota */ } }

// Old unscoped keys: the pref mirror (lw:p:*, the server store is the truth, except the global font.*) is
// dropped, the running timer goes to the site that is open now.
function migrate() {
  try {
    if (localStorage.getItem('lw:migrated:sites')) return;
    localStorage.setItem('lw:migrated:sites', '1');
    const t = localStorage.getItem('lw:timer');
    if (t != null) { localStorage.removeItem('lw:timer'); if (localStorage.getItem(k('timer')) == null) localStorage.setItem(k('timer'), t); }
    for (const key of Object.keys(localStorage)) if (/^lw:p:(?!font\.|terminal\.)/.test(key)) localStorage.removeItem(key);
  } catch (e) { /* ignore */ }
}
export default { get, set, setSite, siteName };
