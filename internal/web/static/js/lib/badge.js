// Avatar and status pill: plain DOM, for modules that can't load ui.js (md.js, under node too).
import { h } from './dom.js';
import { initials, hue } from './fmt.js';

// Avatar: an image when the API gave a URL, else coloured initials.
export function avatar(name, url, size = 20) {
  const r = v => Math.round(v / 14 * 1000) / 1000 + 'rem';
  const s = { width: r(size), height: r(size), fontSize: r(Math.round(size * 0.42)) };
  if (!name) return h('span.avatar.none', { style: s, title: 'Unassigned' }, '·');
  return h('span.avatar', { style: { ...s, background: `hsl(${hue(name)} 45% 42%)` }, title: name }, initials(name),
    (url && (url.startsWith('/api/avatar/') || url.startsWith('data:'))) && h('img', { src: url, alt: '', loading: 'lazy', onerror: e => e.target.remove(),
      onload: e => Object.assign(e.target.parentNode.style, { background: 'none', color: 'transparent' }) }));
}
// Status pill coloured by status category ('new'|'indeterminate'|'done' or the card's Done/InProgress).
export const statusPill = (name, cat) => h('span.pill.cat-' + (cat || 'new'), name);
