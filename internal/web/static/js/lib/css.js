// Load an area's stylesheet once: css('board') → css/board.css.
const done = new Set();
// index.html links them all up front (render-blocking, so no unstyled frame); this adds one it misses.
export function css(name) {
  if (done.has(name)) return;
  done.add(name);
  if (document.querySelector('link[href="css/' + name + '.css"]')) return;
  const l = document.createElement('link');
  l.rel = 'stylesheet'; l.href = '/css/' + name + '.css';
  document.head.append(l);
}
