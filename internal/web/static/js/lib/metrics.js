// Sizes the UI is laid out in. Everything in CSS is rem (html font-size = --fs), so JS that needs
// pixels (virtual lists, roadmap geometry) asks here and re-lays-out on onChange().
// remPx()        the root font size in px
// rem(n)         n CSS rem at 14px-design numbers: px14(n) = n / 14 rem in px
// rowPx()        the list row height (--row) in px
// onChange(fn)   density, font size, font or line-height changed; returns an unsubscribe
const root = document.documentElement;
const subs = new Set();
export const remPx = () => parseFloat(getComputedStyle(root).fontSize) || 14;
// A length designed at the default 14px, in px now.
export const px14 = n => n * remPx() / 14;
let probe = null, key = '', cached = 32;
export function cssPx(expr) {
  if (!probe) { probe = document.createElement('div'); probe.style.cssText = 'position:absolute;visibility:hidden;pointer-events:none;width:0;border:0;padding:0'; }
  if (!probe.isConnected) root.append(probe);
  probe.style.height = expr;
  const v = probe.getBoundingClientRect().height;
  probe.remove();
  return v;
}
export function rowPx() {
  const k = remPx() + '|' + (root.dataset.density || '');
  if (k !== key) { key = k; cached = cssPx('var(--row)') || 32; }
  return cached;
}
export const onChange = fn => { subs.add(fn); return () => subs.delete(fn); };
export function changed(kind) { key = ''; for (const fn of [...subs]) { try { fn(kind); } catch (e) { console.error('metrics', e); } } }
// The phone breakpoint swaps the density's size (css/mobile.css).
try { matchMedia('(max-width: 700px)').addEventListener('change', () => changed('breakpoint')); } catch (e) { /* ignore */ }
