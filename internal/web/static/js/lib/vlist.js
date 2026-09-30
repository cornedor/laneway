// Virtual list: only the rows in (and just around) the viewport exist in the
// DOM, recycled as you scroll.
//
//   const v = vlist(scrollerEl, {
//     count: 5000,
//     rowHeight: 40,            // px, or i => px (estimated/variable; offsets are cached)
//     overscan: 6,              // extra rows above and below
//     create: () => el,         // make a blank row element (once per pooled row)
//     bind: (el, i) => {},      // fill a row for index i (called again on refresh and recycle)
//   });
//   v.setCount(n) v.setRowHeight(h|fn) v.refresh([i]) v.scrollTo(i, 'nearest'|'start'|'center')
//   v.indexAt(y) (y relative to the scroller's top) v.offset(i) v.rowEl(i) v.range() v.destroy()
//
// `scrollerEl` must be a sized, overflow:auto box. Rows are absolutely
// positioned and get an explicit height, so keep their content to that height.
export function vlist(scroller, o) {
  let count = o.count || 0, rh = o.rowHeight || 40;
  const over = o.overscan == null ? 6 : o.overscan;
  const inner = document.createElement('div');
  inner.className = 'vl-inner';
  inner.style.cssText = 'position:relative;width:100%;';
  scroller.append(inner);
  const live = new Map(), pool = [];
  let offs = null, first = 0, last = -1, q = 0, dead = false;

  const fixed = () => typeof rh === 'number';
  const heightOf = i => (fixed() ? rh : rh(i));
  function measure() {
    if (fixed()) { offs = null; inner.style.height = count * rh + 'px'; return; }
    offs = new Float64Array(count + 1);
    for (let i = 0; i < count; i++) offs[i + 1] = offs[i] + rh(i);
    inner.style.height = offs[count] + 'px';
  }
  const offset = i => (fixed() ? i * rh : offs[Math.min(Math.max(i, 0), count)]);
  function indexAt(y) {
    if (count === 0) return 0;
    if (fixed()) return Math.min(count - 1, Math.max(0, Math.floor(y / rh)));
    let lo = 0, hi = count - 1;
    while (lo < hi) { const m = (lo + hi + 1) >> 1; if (offs[m] <= y) lo = m; else hi = m - 1; }
    return lo;
  }
  function place(el, i) {
    el.style.transform = 'translateY(' + offset(i) + 'px)';
    el.style.height = heightOf(i) + 'px';
  }
  function release(i, el) {
    live.delete(i);
    el.style.display = 'none';
    pool.push(el);
  }
  function render() {
    q = 0;
    if (dead) return;
    const top = scroller.scrollTop, h = scroller.clientHeight || 600;
    if (count === 0) { for (const [i, el] of [...live]) release(i, el); first = 0; last = -1; return; }
    const a = Math.max(0, indexAt(top) - over), b = Math.min(count - 1, indexAt(top + h) + over);
    for (const [i, el] of [...live]) if (i < a || i > b) release(i, el);
    for (let i = a; i <= b; i++) {
      let el = live.get(i);
      if (!el) {
        el = pool.pop();
        if (!el) {
          el = o.create();
          el.classList.add('vl-row');
          el.style.position = 'absolute'; el.style.left = '0'; el.style.right = '0'; el.style.top = '0';
          inner.append(el);
        }
        el.style.display = '';
        live.set(i, el);
        place(el, i);
        o.bind(el, i);
      }
    }
    first = a; last = b;
  }
  const schedule = () => { if (!q && !dead) q = requestAnimationFrame(render); };
  scroller.addEventListener('scroll', schedule, { passive: true });
  const ro = typeof ResizeObserver === 'function' ? new ResizeObserver(schedule) : null;
  if (ro) ro.observe(scroller);
  measure();
  render();

  return {
    setCount(n) {
      count = n; measure();
      for (const [i, el] of [...live]) if (i >= n) release(i, el);
      for (const [i, el] of live) { place(el, i); o.bind(el, i); }
      render();
    },
    setRowHeight(h) {
      rh = h; measure();
      for (const [i, el] of live) place(el, i);
      render();
    },
    // Rebind one row (if rendered) or all of them.
    refresh(i) {
      if (i == null) { for (const [j, el] of live) o.bind(el, j); return; }
      const el = live.get(i); if (el) o.bind(el, i);
    },
    scrollTo(i, align = 'nearest') {
      if (i < 0 || i >= count) return;
      const t = offset(i), bt = t + heightOf(i), vh = scroller.clientHeight, cur = scroller.scrollTop;
      let to = cur;
      if (align === 'start') to = t;
      else if (align === 'center') to = t - (vh - heightOf(i)) / 2;
      else if (t < cur) to = t;
      else if (bt > cur + vh) to = bt - vh;
      if (to !== cur) { scroller.scrollTop = Math.max(0, to); render(); }
    },
    indexAt, offset,
    rowEl: i => live.get(i) || null,
    range: () => [first, last],
    get count() { return count; },
    destroy() { dead = true; cancelAnimationFrame(q); scroller.removeEventListener('scroll', schedule); if (ro) ro.disconnect(); inner.remove(); live.clear(); pool.length = 0; },
  };
}
export default vlist;
