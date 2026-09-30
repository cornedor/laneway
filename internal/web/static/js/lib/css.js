// Load an area's stylesheet once: css('board') → css/board.css.
const done = new Set();
export function css(name) {
  if (done.has(name)) return;
  done.add(name);
  const l = document.createElement('link');
  l.rel = 'stylesheet'; l.href = 'css/' + name + '.css';
  document.head.append(l);
}
