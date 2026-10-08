// Formatting helpers. The Go API sends zero times as "0001-01-01T00:00:00Z".
import { T } from './i18n.js';
export const isZero = t => !t || String(t).startsWith('0001-');
export const date = t => (isZero(t) ? null : new Date(t));
// toLocale*String builds a formatter per call; a board formats thousands of dates, so keep them.
const DM = new Intl.DateTimeFormat(undefined, { day: 'numeric', month: 'short' });
const DMY = new Intl.DateTimeFormat(undefined, { day: 'numeric', month: 'short', year: 'numeric' });
const DMT = new Intl.DateTimeFormat(undefined, { day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit' });
const YMD = new Intl.DateTimeFormat();
export const localDate = d => YMD.format(d); // what toLocaleDateString() gives
export function ago(t, now = Date.now()) {
  const d = date(t); if (!d) return '';
  const s = Math.round((now - d) / 1000);
  if (s < 45) return T('just now');
  const u = [[60, 'm', 60], [3600, 'h', 24], [86400, 'd', 7], [604800, 'w', 5]];
  if (s < 3600) return T('%dm ago', Math.round(s / 60));
  if (s < 86400) return T('%dh ago', Math.round(s / 3600));
  if (s < 7 * 86400) return T('%dd ago', Math.round(s / 86400));
  return (d.getFullYear() === new Date(now).getFullYear() ? DM : DMY).format(d);
}
export const shortDate = t => { const d = date(t); return d ? DM.format(d) : ''; };
export const dateTime = t => { const d = date(t); return d ? DMT.format(d) : ''; };
export function duration(sec) {
  sec = Math.round(sec); if (!sec) return '0m';
  const h = Math.floor(sec / 3600), m = Math.round((sec % 3600) / 60);
  return (h ? h + 'h' : '') + (m || !h ? (h ? ' ' : '') + m + 'm' : '');
}
export const initials = n => (n || '?').split(/\s+/).slice(0, 2).map(w => w[0] || '').join('').toUpperCase();
// Stable hue from a string, for avatars and label chips.
export function hue(s) { let h = 0; for (let i = 0; i < s.length; i++) h = (h * 31 + s.charCodeAt(i)) % 360; return h; }
export const plural = (n, w) => n + ' ' + w + (n === 1 ? '' : 's');
