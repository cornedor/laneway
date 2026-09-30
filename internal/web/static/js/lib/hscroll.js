// Horizontal wheel for a scroller whose children scroll vertically: shift+wheel
// and trackpad sideways swipes scroll `el` even with the pointer over a lane.
export function hwheel(el) {
  el.addEventListener('wheel', e => {
    const dx = e.deltaX || (e.shiftKey ? e.deltaY : 0);
    if (!dx || el.scrollWidth <= el.clientWidth) return;
    el.scrollLeft += dx;
    e.preventDefault();
  }, { passive: false });
}
