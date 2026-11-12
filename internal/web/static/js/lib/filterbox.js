// A filter input with a button at its end that opens the filter builder (F, views/board_filter.js): the board's,
// planning's and my work's. The input keeps its class (bd-filter…); the wrapper is sized as <class>-box.
import { h } from './dom.js';
import { icon } from './icons.js';
import { T } from './i18n.js';

export function filterBox(input, open) {
  const own = [...input.classList].filter(c => c !== 'input');
  input.classList.add('fbox-in');
  const btn = h('button.fbox-btn', { type: 'button', title: T('Filter builder  (F)'), 'aria-label': T('Filter builder'), onclick: () => open() }, icon('list-filter'));
  const box = h('span.fbox', input, btn);
  box.classList.add(...own.map(c => c + '-box'));
  return box;
}
