// A resize grip, as the issue panel's and the standup's board have: dragging it sets a percentage (pct turns the
// pointer into one), min–max and snapping to def within 3 points; a double click puts def back. show draws a
// percentage while dragging, keep draws and stores where the drag ended.
export function grip(el, { pct, show, keep, def, min = 20, max = 80 }) {
  const clamp = p => Math.min(Math.max(Math.round(p), min), max);
  el.addEventListener('pointerdown', e => {
    e.preventDefault(); el.setPointerCapture(e.pointerId); el.classList.add('drag');
    let at = null;
    const move = ev => { at = clamp(pct(ev)); if (Math.abs(at - def) <= 3) at = def; show(at); };
    const up = () => {
      el.removeEventListener('pointermove', move); el.removeEventListener('pointerup', up); el.classList.remove('drag');
      if (at !== null) keep(at);
    };
    el.addEventListener('pointermove', move); el.addEventListener('pointerup', up);
  });
  el.addEventListener('dblclick', () => keep(def));
  return clamp;
}
