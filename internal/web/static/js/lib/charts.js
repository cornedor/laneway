// Hand-written SVG charts. Colours are CSS variables (themes apply), text is
// styled in css/reports.css.
//
//   const c = chart(host, {
//     title, desc, height,
//     x: {min, max, ticks: [{v, label}]},          // numbers: time in ms, or bar index
//     y: {min: 0, max, fmt},
//     layers: [ {type:'line', pts:[[x,y]], color, dash, step, area, width},
//               {type:'area', pts:[[x,y0,y1]], color, opacity},
//               {type:'bars', items:[{x,y,y0,color,opacity}], bw},
//               {type:'dots', pts:[[x,y]], color, r},
//               {type:'hline', y, color, label, dash}, {type:'vline', x, color, label, dash} ],
//     targets: [{x, y, head, rows:[{color, label, value, y}], onclick}],   // hover and ←/→ stops
//     nearest: 'x' | 'xy', legend: [{name, color, dash}],
//   });
//   c.update(spec); c.destroy();
import { h } from './dom.js';
import { css } from './css.js';
import { T } from './i18n.js';

const NS = 'http://www.w3.org/2000/svg';
function S(tag, attrs, ...kids) {
  const e = document.createElementNS(NS, tag);
  for (const k in attrs) { const v = attrs[k]; if (v != null && v !== false) e.setAttribute(k, v); }
  for (const kid of kids.flat()) if (kid != null && kid !== false) e.append(kid.nodeType ? kid : document.createTextNode(String(kid)));
  return e;
}

// niceTicks(max, n) → {max, ticks}: round steps (1, 2, 5 × 10^k) from 0 up to ≥ max.
export function niceTicks(max, n = 5) {
  if (!(max > 0)) return { max: 1, ticks: [0, 1] };
  const raw = max / n, mag = Math.pow(10, Math.floor(Math.log10(raw)));
  const step = [1, 2, 2.5, 5, 10].map(m => m * mag).find(s => s >= raw) || raw;
  const ticks = [];
  for (let v = 0; v < max + step * 0.999; v += step) ticks.push(Math.round(v * 1e6) / 1e6);
  return { max: ticks[ticks.length - 1], ticks };
}

const DAY = 86400000;
const localDay = t => { const d = new Date(t); return new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime(); };
export { localDay };

// timeTicks(t0, t1, n, fmt): local-midnight ticks, a whole number of days apart (or months for long spans).
export function timeTicks(t0, t1, n = 6, fmt) {
  const f = fmt || (t => new Date(t).toLocaleDateString(undefined, { day: 'numeric', month: 'short' }));
  const days = Math.max((t1 - t0) / DAY, 1);
  const out = [];
  if (days > 150) {
    const step = days > 600 ? 3 : 1;
    const d = new Date(t0); d.setDate(1); d.setHours(0, 0, 0, 0);
    if (d.getTime() < t0) d.setMonth(d.getMonth() + 1);
    for (; d.getTime() <= t1; d.setMonth(d.getMonth() + step)) out.push({ v: d.getTime(), label: d.toLocaleDateString(undefined, { month: 'short', year: d.getMonth() === 0 ? 'numeric' : undefined }) });
    return out;
  }
  const step = [1, 2, 3, 7, 14, 30].find(s => days / s <= n) || 30;
  for (let d = new Date(localDay(t0)); d.getTime() <= t1; d.setDate(d.getDate() + step)) if (d.getTime() >= t0) out.push({ v: d.getTime(), label: f(d.getTime()) });
  return out;
}

export const num = f => String(Math.round(f * 10) / 10);

export function chart(host, spec) {
  css('reports');
  let ro = null, w = 0, raf = 0, active = -1, dead = false;
  const svgBox = h('div.ch-svg');
  const tip = h('div.ch-tip', { hidden: true, 'aria-hidden': 'true' });
  const live = h('div.sr-only', { 'aria-live': 'polite' });
  const legend = h('div.ch-legend');
  const box = h('div.ch', { tabindex: 0, role: 'img' }, svgBox, tip, live);
  host.append(box, legend);
  let geo = null;

  function draw() {
    raf = 0; if (dead) return;
    w = Math.floor(box.clientWidth); if (w < 40) return;
    const hgt = spec.height || 260, yf = spec.y.fmt || num;
    const yt = spec.y.ticks || niceTicks(spec.y.max).ticks;
    const y0 = spec.y.min ?? 0, y1 = Math.max(spec.y.max, yt[yt.length - 1] || 1);
    const padL = Math.max(...yt.map(v => yf(v).length)) * 6.5 + 14;
    const m = { l: padL, r: 16, t: 12, b: 26 }, iw = w - m.l - m.r, ih = hgt - m.t - m.b;
    const x0 = spec.x.min, x1 = spec.x.max;
    const X = v => m.l + (v - x0) / (x1 - x0 || 1) * iw, Y = v => m.t + (1 - (v - y0) / (y1 - y0 || 1)) * ih;
    geo = { X, Y, m, iw, ih, hgt };
    const svg = S('svg', { width: w, height: hgt, viewBox: `0 0 ${w} ${hgt}`, 'aria-hidden': 'true' });
    const grid = S('g', { class: 'ch-grid' });
    for (const v of yt) {
      const y = Math.round(Y(v)) + 0.5;
      grid.append(S('line', { x1: m.l, x2: w - m.r, y1: y, y2: y, class: v === y0 ? 'ch-base' : '' }), S('text', { x: m.l - 6, y: y + 4, 'text-anchor': 'end' }, yf(v)));
    }
    for (const t of spec.x.ticks || []) {
      const x = Math.round(X(t.v)) + 0.5;
      grid.append(S('line', { x1: x, x2: x, y1: hgt - m.b, y2: hgt - m.b + 4, class: 'ch-tickmark' }), S('text', { x, y: hgt - 8, 'text-anchor': 'middle' }, t.label));
    }
    svg.append(grid);
    for (const L of spec.layers) layer(svg, L, X, Y, m, w, y0);
    const cursor = S('g', { class: 'ch-cursor', visibility: 'hidden' },
      S('line', { y1: m.t, y2: hgt - m.b, class: 'ch-cline' }));
    svg.append(cursor);
    svgBox.replaceChildren(svg);
    geo.svg = svg; geo.cursor = cursor;
    legend.replaceChildren(...(spec.legend || []).map(l => h('span.ch-key', h('i', { style: { background: l.dash ? 'transparent' : l.color, borderTop: l.dash ? `2px dashed ${l.color}` : '' } }), l.name)));
    if (active >= 0) show(Math.min(active, (spec.targets || []).length - 1), false);
  }

  function layer(svg, L, X, Y, m, w, y0) {
    const g = S('g', { class: 'ch-layer' });
    const col = L.color || 'var(--accent)';
    switch (L.type) {
      case 'line': {
        if (!L.pts.length) break;
        let d = '';
        L.pts.forEach(([x, y], i) => {
          const px = X(x), py = Y(y);
          d += i === 0 ? `M${px},${py}` : L.step ? `H${px}V${py}` : `L${px},${py}`;
        });
        if (L.area) g.append(S('path', { d: d + `L${X(L.pts[L.pts.length - 1][0])},${Y(y0)}L${X(L.pts[0][0])},${Y(y0)}Z`, style: `fill:${col};opacity:${L.area === true ? 0.15 : L.area}` }));
        g.append(S('path', { d, class: 'ch-line', style: `stroke:${col};stroke-width:${L.width || 2}` + (L.dash ? ';stroke-dasharray:5 4' : '') }));
        break;
      }
      case 'area': {
        if (!L.pts.length) break;
        const top = L.pts.map(([x, , hi], i) => `${i ? 'L' : 'M'}${X(x)},${Y(hi)}`).join('');
        const bot = [...L.pts].reverse().map(([x, lo]) => `L${X(x)},${Y(lo)}`).join('');
        g.append(S('path', { d: top + bot + 'Z', style: `fill:${col};opacity:${L.opacity ?? 0.7}`, class: 'ch-area' }));
        break;
      }
      case 'bars': {
        const bw = Math.abs(X(L.bw || 0.8) - X(0));
        for (const b of L.items) {
          const top = Y(b.y), bot = Y(b.y0 || 0);
          if (Math.abs(bot - top) < 0.5 && !b.y) continue;
          g.append(S('rect', { x: X(b.x) - bw / 2 + (b.dx || 0), width: Math.max(bw * (b.wf || 1), 1), y: Math.min(top, bot), height: Math.max(Math.abs(bot - top), 0), rx: 2, style: `fill:${b.color || col};opacity:${b.opacity ?? 1}` }));
        }
        break;
      }
      case 'dots':
        for (const [x, y] of L.pts) g.append(S('circle', { cx: X(x), cy: Y(y), r: L.r || 3.5, style: `fill:${col};opacity:${L.opacity ?? 0.75}` }));
        break;
      case 'hline': {
        const y = Y(L.y);
        g.append(S('line', { x1: m.l, x2: w - m.r, y1: y, y2: y, class: 'ch-ref', style: `stroke:${col}` + (L.dash === false ? '' : ';stroke-dasharray:4 4') }));
        if (L.label) g.append(S('text', { x: w - m.r - 4, y: y - 4, 'text-anchor': 'end', class: 'ch-reflabel', style: `fill:${col}` }, L.label));
        break;
      }
      case 'vline': {
        const x = X(L.x);
        g.append(S('line', { x1: x, x2: x, y1: m.t, y2: geo.hgt - m.b, class: 'ch-ref', style: `stroke:${col}` + (L.dash === false ? '' : ';stroke-dasharray:4 4') }));
        if (L.label) {
          const right = x > w - 70;
          g.append(S('text', { x: x + (right ? -4 : 4), y: m.t + 10, 'text-anchor': right ? 'end' : 'start', class: 'ch-reflabel', style: `fill:${col}` }, L.label));
        }
        break;
      }
    }
    svg.append(g);
  }

  function show(i, announce = true) {
    const ts = spec.targets || [];
    if (!geo || !ts[i]) return hide();
    active = i;
    const t = ts[i], px = geo.X(t.x);
    const c = geo.cursor; c.setAttribute('visibility', 'visible');
    c.replaceChildren(S('line', { x1: px, x2: px, y1: geo.m.t, y2: geo.hgt - geo.m.b, class: 'ch-cline', visibility: spec.nearest === 'xy' ? 'hidden' : 'visible' }));
    const dots = t.rows && t.rows.some(r => r.y != null) ? t.rows.filter(r => r.y != null) : (t.y != null ? [{ y: t.y, color: 'var(--fg)' }] : []);
    for (const r of dots) c.append(S('circle', { cx: px, cy: geo.Y(r.y), r: 5, class: 'ch-cdot', style: `stroke:${r.color || 'var(--fg)'}` }));
    tip.replaceChildren(h('div.ch-head', t.head), ...(t.rows || []).map(r => h('div.ch-row', r.color && h('i', { style: { background: r.color } }), h('span', r.label), h('b', r.value))));
    tip.hidden = false;
    const tw = tip.offsetWidth, th = tip.offsetHeight;
    const top = (dots.length ? Math.min(...dots.map(r => geo.Y(r.y))) : geo.m.t + 10) - th - 10;
    tip.style.left = Math.max(4, Math.min(px + 12 + tw > w - 4 ? px - tw - 12 : px + 12, w - tw - 4)) + 'px';
    tip.style.top = Math.max(4, Math.min(top < 4 ? geo.m.t + 10 : top, geo.hgt - th - 4)) + 'px';
    if (announce) live.textContent = [t.head, ...(t.rows || []).map(r => r.label + ' ' + r.value)].join(', ');
  }
  function hide() { active = -1; if (geo) geo.cursor.setAttribute('visibility', 'hidden'); tip.hidden = true; }

  function nearest(e) {
    const ts = spec.targets || []; if (!ts.length || !geo) return -1;
    const r = box.getBoundingClientRect(), mx = e.clientX - r.left, my = e.clientY - r.top;
    let best = -1, bd = Infinity;
    ts.forEach((t, i) => {
      const dx = geo.X(t.x) - mx, dy = spec.nearest === 'xy' && t.y != null ? geo.Y(t.y) - my : 0;
      const d = dx * dx + dy * dy;
      if (d < bd) { bd = d; best = i; }
    });
    return spec.nearest === 'xy' && bd > 28 * 28 ? -1 : best;
  }
  box.addEventListener('pointermove', e => { const i = nearest(e); i < 0 ? hide() : i !== active && show(i); });
  box.addEventListener('pointerleave', () => { if (document.activeElement !== box) hide(); });
  box.addEventListener('click', e => { const t = (spec.targets || [])[nearest(e)]; if (t && t.onclick) t.onclick(); });
  box.addEventListener('focus', () => { if (active < 0) { const n = (spec.targets || []).length; if (n) show(n - 1); } });
  box.addEventListener('blur', hide);
  box.addEventListener('keydown', e => {
    const n = (spec.targets || []).length; if (!n) return;
    const go = i => { e.preventDefault(); e.stopPropagation(); show(Math.max(0, Math.min(n - 1, i))); };
    if (e.key === 'ArrowRight') go(active < 0 ? 0 : active + 1);
    else if (e.key === 'ArrowLeft') go(active < 0 ? n - 1 : active - 1);
    else if (e.key === 'Home') go(0);
    else if (e.key === 'End') go(n - 1);
    else if (e.key === 'Enter' && active >= 0 && spec.targets[active].onclick) spec.targets[active].onclick();
    else if (e.key === 'Escape' && active >= 0) { e.preventDefault(); e.stopPropagation(); hide(); }
  }, true);

  function apply() {
    svgBox.style.height = (spec.height || 260) + 'px'; // the drawing comes a frame later; keep its room meanwhile
    box.setAttribute('aria-label', spec.title || T('chart'));
    if (spec.desc) box.setAttribute('aria-description', spec.desc);
    if (!raf) raf = requestAnimationFrame(draw);
  }
  ro = new ResizeObserver(() => { if (Math.floor(box.clientWidth) !== w) apply(); });
  ro.observe(box);
  apply();
  return {
    update(s) { spec = s; apply(); },
    destroy() { dead = true; ro.disconnect(); cancelAnimationFrame(raf); box.remove(); legend.remove(); },
  };
}

// Percentile of sorted-or-not numbers, nearest rank like the Go client.
export function percentile(vals, p) {
  if (!vals.length) return 0;
  const s = [...vals].sort((a, b) => a - b);
  return s[Math.min(Math.max(Math.ceil(p * s.length / 100) - 1, 0), s.length - 1)];
}
