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
