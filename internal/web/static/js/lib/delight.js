// Small celebrations (ui.delight, on unless off), as the TUI's: a card moved into a done lane throws confetti over
// the lane head (paper pieces in the theme's colours, on a fixed layer so no lane clips them). Reduced motion (the
// system's or Settings') skips it.
import { h } from './dom.js';

const COLORS = ['--accent', '--ok', '--warn', '--err', '--info', '--cat-done'];

export const delightOn = app => String((app.session && app.session.ui && app.session.ui.Delight) || '').toLowerCase() !== 'off';

const still = () => document.documentElement.dataset.motion === 'reduce' || (window.matchMedia && matchMedia('(prefers-reduced-motion: reduce)').matches);
const rnd = (a, b) => a + Math.random() * (b - a);

export function confetti(app, el) {
  if (!el || !delightOn(app) || still()) return;
  const r = el.getBoundingClientRect(), w = Math.max(r.width, 160);
  const box = h('div.confetti', { 'aria-hidden': 'true', style: { left: r.left + r.width / 2 + 'px', top: r.top + r.height / 2 + 'px' } });
  for (let i = 0; i < 60; i++) {
    const p = h('i');
    const s = p.style;
    s.setProperty('--dx', Math.round(rnd(-0.75, 0.75) * w) + 'px');
    s.setProperty('--up', -Math.round(rnd(40, 140)) + 'px');
    s.setProperty('--fall', Math.round(rnd(160, 360)) + 'px');
    s.setProperty('--rot', Math.round(rnd(-720, 720)) + 'deg');
    s.width = Math.round(rnd(6, 11)) + 'px';
    s.height = Math.round(rnd(9, 16)) + 'px';
    if (Math.random() < 0.3) s.borderRadius = '50%';
    s.background = 'var(' + COLORS[i % COLORS.length] + ')';
    s.animationDuration = Math.round(rnd(1200, 1900)) + 'ms';
    s.animationDelay = Math.round(rnd(0, 120)) + 'ms';
    box.append(p);
  }
  document.body.append(box);
  setTimeout(() => box.remove(), 2200);
}
