// Small celebrations (ui.delight, on unless off), as the TUI's: a card moved into a done lane throws confetti from
// the lane head. Paper pieces on a canvas over the page: launched up in a cone, slowed by the air, pulled down,
// fluttering (wobble) and flipping (tilt) as they fall and fade. One frame of physics per 60th of a second, so a
// faster screen draws more often but the flight is the same. Reduced motion (the system's or Settings') skips it.

export const delightOn = app => String((app.session && app.session.ui && app.session.ui.Delight) || '').toLowerCase() !== 'off';

const still = () => document.documentElement.dataset.motion === 'reduce' || (window.matchMedia && matchMedia('(prefers-reduced-motion: reduce)').matches);

const FRAME = 1000 / 60;
const TICKS = 200;     // a piece's life in frames
const DRAG = 0.9;      // speed kept per frame
const GRAVITY = 3;     // px a frame pulled down, on top of the flight
const TOKENS = ['--accent', '--ok', '--warn', '--err', '--info', '--cat-done'];

// burst: n pieces from (x, y), aimed up within spread degrees, at up to speed px a frame.
export function burst(n, x, y, colors, { spread = 70, speed = 24, rnd = Math.random } = {}) {
  const out = [], aim = -Math.PI / 2, half = spread * Math.PI / 360;
  for (let i = 0; i < n; i++) {
    out.push({
      x, y, color: colors[i % colors.length],
      angle: aim + (rnd() * 2 - 1) * half,
      v: speed * (0.5 + rnd() * 0.5),
      wobble: rnd() * 10, wobbleSpeed: 0.05 + rnd() * 0.06,
      tilt: (0.25 + rnd() * 0.5) * Math.PI, size: 2 + rnd(),
      round: rnd() < 0.25, tick: 0,
    });
  }
  return out;
}

// step: one frame of physics; false once the piece is spent.
export function step(p) {
  p.x += Math.cos(p.angle) * p.v;
  p.y += Math.sin(p.angle) * p.v + GRAVITY;
  p.v *= DRAG;
  p.wobble += p.wobbleSpeed;
  p.tilt += 0.1;
  return ++p.tick < TICKS;
}

// paint: a piece is a quad between its spot and its wobbled spot, stretched by its tilt: a flat paper that flips.
export function paint(ctx, ps) {
  for (const p of ps) {
    const wx = p.x + 10 * Math.cos(p.wobble), wy = p.y + 10 * Math.sin(p.wobble);
    const tc = Math.cos(p.tilt) * p.size * 1.5, ts = Math.sin(p.tilt) * p.size * 1.5;
    ctx.globalAlpha = Math.max(0, 1 - p.tick / TICKS);
    ctx.fillStyle = p.color;
    ctx.beginPath();
    if (p.round) {
      ctx.ellipse(p.x, p.y, Math.abs(wx - p.x) * 0.5 + 1, Math.abs(wy - p.y) * 0.5 + 1, Math.PI / 10 * p.wobble, 0, 2 * Math.PI);
    } else {
      ctx.moveTo(p.x, p.y);
      ctx.lineTo(p.x + tc, p.y + ts);
      ctx.lineTo(wx + tc, wy + ts);
      ctx.lineTo(wx, wy);
    }
    ctx.closePath();
    ctx.fill();
  }
  ctx.globalAlpha = 1;
}

const themeColors = () => {
  const cs = getComputedStyle(document.documentElement);
  return TOKENS.map(t => cs.getPropertyValue(t).trim()).filter(Boolean);
};

let layer = null; // {canvas, ctx, pieces, raf}: one canvas for every burst in flight

export function confetti(app, el) {
  if (!el || !delightOn(app) || still()) return;
  const r = el.getBoundingClientRect();
  const pieces = burst(100, r.left + r.width / 2, r.top + r.height / 2, themeColors());
  if (layer) { layer.pieces.push(...pieces); return; }
  const canvas = document.createElement('canvas');
  canvas.setAttribute('aria-hidden', 'true');
  Object.assign(canvas.style, { position: 'fixed', inset: '0', width: '100vw', height: '100vh', zIndex: '50', pointerEvents: 'none' });
  document.body.append(canvas);
  const ctx = canvas.getContext('2d');
  const size = () => {
    const d = window.devicePixelRatio || 1;
    canvas.width = innerWidth * d; canvas.height = innerHeight * d;
    ctx.setTransform(d, 0, 0, d, 0, 0);
  };
  size();
  layer = { canvas, ctx, pieces, last: performance.now(), acc: 0 };
  addEventListener('resize', size);
  const frame = now => {
    const L = layer;
    L.acc += Math.min(now - L.last, 100) / FRAME; // a stall (hidden tab) does not jump the flight
    L.last = now;
    for (; L.acc >= 1; L.acc--) L.pieces = L.pieces.filter(step);
    ctx.clearRect(0, 0, innerWidth, innerHeight);
    paint(ctx, L.pieces);
    if (L.pieces.length) { requestAnimationFrame(frame); return; }
    removeEventListener('resize', size);
    canvas.remove();
    layer = null;
  };
  requestAnimationFrame(frame);
}
