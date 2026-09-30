// Installable shell and phone behaviour: service worker, theme-color, offline banner,
// card menu on right-click / long-press (the TUI's card menu).
import { h } from './dom.js';

const KEY = /^[A-Z][A-Z0-9_]*-\d+$/;

export function install(app) {
  worker();
  themeColor();
  offline();
  cardMenu(app);
}

function worker() {
  if (!('serviceWorker' in navigator)) return;
  navigator.serviceWorker.register('/sw.js').catch(() => {}); // needs localhost or https
}

// The browser chrome follows the theme: the header's colour, re-read when the theme or the OS scheme changes.
function themeColor() {
  const root = document.documentElement;
  let meta = document.querySelector('meta[name=theme-color]');
  if (!meta) { meta = h('meta', { name: 'theme-color' }); document.head.append(meta); }
  let raf = 0;
  const sync = () => {
    cancelAnimationFrame(raf);
    raf = requestAnimationFrame(() => { const c = getComputedStyle(root).getPropertyValue('--bg-2').trim(); if (c) meta.content = c; });
  };
  new MutationObserver(sync).observe(root, { attributes: true, attributeFilter: ['data-theme', 'style'] });
  matchMedia('(prefers-color-scheme: dark)').addEventListener('change', sync);
  sync();
}

function offline() {
  const bar = h('div.offline', { role: 'status', hidden: true }, 'Offline. Showing what was loaded; edits will fail until the connection is back.');
  document.body.append(bar);
  const sync = () => { bar.hidden = navigator.onLine; document.body.classList.toggle('is-offline', !navigator.onLine); };
  addEventListener('online', sync); addEventListener('offline', sync); sync();
}

const keyAt = el => {
  const row = el.closest && el.closest('.card, .lrow, .pl-row, [data-key]');
  if (!row) return '';
  const k = (row.dataset && row.dataset.key) || (row.querySelector('.ckey, .l-key') || {}).textContent || '';
  return KEY.test(k.trim()) ? k.trim() : '';
};

function cardMenu(app) {
  let opened = 0, press = 0, sx = 0, sy = 0, swallow = 0;
  const open = key => {
    if (Date.now() - opened < 800) return;
    opened = Date.now();
    const url = () => app.session.baseURL.replace(/\/$/, '') + '/browse/' + key;
    const items = [
      ['Open', () => app.panel.open(key)],
      ['Change status', () => app.actions.transition(key)],
      ['Assign', () => app.actions.edit(key, 'assignee')],
      ['Set priority', () => app.actions.edit(key, 'priority')],
      ['Set story points', () => app.actions.edit(key, 'points')],
      ['Edit summary', () => app.actions.edit(key, 'summary')],
      ['Edit labels', () => app.actions.edit(key, 'labels')],
      ['Copy key', () => navigator.clipboard && navigator.clipboard.writeText(key).then(() => app.ui.toast('Copied ' + key))],
      ['Copy link', () => navigator.clipboard && navigator.clipboard.writeText(url()).then(() => app.ui.toast('Copied link'))],
      ['Open in Jira', () => window.open(url(), '_blank', 'noopener')],
    ];
    app.ui.pick({ title: key, items, label: i => i[0], placeholder: 'Action…' }).then(r => r && r[1]());
  };
  document.addEventListener('contextmenu', e => {
    if (e.defaultPrevented || e.target.closest('input, textarea, a[href]')) return;
    const key = keyAt(e.target); if (!key) return;
    e.preventDefault(); open(key);
  });
  // iOS fires no contextmenu on a long press.
  document.addEventListener('touchstart', e => {
    if (e.touches.length !== 1) return;
    const key = keyAt(e.target); if (!key) return;
    sx = e.touches[0].clientX; sy = e.touches[0].clientY;
    clearTimeout(press);
    press = setTimeout(() => { swallow = Date.now(); if (navigator.vibrate) navigator.vibrate(10); open(key); }, 450);
  }, { passive: true });
  const cancel = () => clearTimeout(press);
  document.addEventListener('touchend', cancel, { passive: true });
  document.addEventListener('touchcancel', cancel, { passive: true });
  document.addEventListener('touchmove', e => { const t = e.touches[0]; if (Math.abs(t.clientX - sx) + Math.abs(t.clientY - sy) > 10) cancel(); }, { passive: true });
  document.addEventListener('click', e => { if (Date.now() - swallow < 700) { e.stopPropagation(); e.preventDefault(); } }, true);
}
