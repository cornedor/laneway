import { test } from 'node:test';
import assert from 'node:assert/strict';
import { burst, step } from '../static/js/lib/delight.js';

test('confetti rises over the lane head, then falls past it and is spent', () => {
  let s = 3; const rnd = () => (s = (s * 16807) % 2147483647) / 2147483647;
  const ps = burst(100, 0, 0, ['#f00'], { rnd });
  let top = 0, frames = 0, live = ps;
  while (live.length && frames < 1000) { live = live.filter(step); frames++; if (frames === 20) top = Math.min(...ps.map(p => p.y)); }
  assert.ok(top < -100 && top > -260, 'peak ' + top); // up, but not off a screen from a lane head
  assert.ok(ps.every(p => p.y > 0), 'every piece falls back past where it started');
  assert.equal(frames, 200);
  const xs = ps.map(p => p.x);
  assert.ok(Math.max(...xs) - Math.min(...xs) > 150, 'spread over a lane');
});

test('reach widens the burst, not its height', () => {
  const fly = reach => {
    let s = 5; const rnd = () => (s = (s * 16807) % 2147483647) / 2147483647;
    const ps = burst(100, 0, 0, ['#f00'], { rnd, reach });
    let top = 0;
    for (let f = 0; f < 60; f++) { ps.forEach(step); if (f === 20) top = Math.min(...ps.map(p => p.y)); }
    return { top, wide: Math.max(...ps.map(p => Math.abs(p.x))) };
  };
  const narrow = fly(140), wide = fly(400);
  assert.equal(Math.round(wide.top), Math.round(narrow.top));
  assert.ok(wide.wide > 2.5 * narrow.wide && wide.wide < 420, 'reach ' + wide.wide);
});
