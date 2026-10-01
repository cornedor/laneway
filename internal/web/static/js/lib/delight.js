// Small celebrations (ui.delight, on unless off), as the TUI's: a card moved into a done lane throws a short
// braille confetti over the lane head. Reduced motion (the system's or Settings') skips it.
import { h } from './dom.js';

const DOTS = [...'⠁⠂⠄⡀⢀⠠⠐⠈⠃⠘⡠⢄⠌⠡'];

export const delightOn = app => String((app.session && app.session.ui && app.session.ui.Delight) || '').toLowerCase() !== 'off';

const still = () => document.documentElement.dataset.motion === 'reduce' || (window.matchMedia && matchMedia('(prefers-reduced-motion: reduce)').matches);

export function confetti(app, el) {
  if (!el || !delightOn(app) || still()) return;
  const box = h('span.confetti', { 'aria-hidden': 'true' });
  for (let i = 0; i < 14; i++) {
    const d = h('i', DOTS[Math.floor(Math.random() * DOTS.length)]);
    d.style.setProperty('--dx', Math.round((Math.random() * 2 - 1) * 56) + 'px');
    d.style.setProperty('--dy', -Math.round(10 + Math.random() * 30) + 'px');
    d.style.animationDelay = Math.round(Math.random() * 140) + 'ms';
    box.append(d);
  }
  el.append(box);
  setTimeout(() => box.remove(), 1100);
}
