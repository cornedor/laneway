// Marking issues (x): the checkbox a card or list row shows, and the bar that counts the marked ones and edits them.
//   check()                           a .chk; .on when marked
//   const bar = selBar({edit, clear}) bar.el floats over the view's foot; bar.set(n) shows it while n > 0
import { h } from './dom.js';
import { icon } from './icons.js';
import { T } from './i18n.js';

export const check = () => h('span.chk', { role: 'checkbox', 'aria-checked': 'false', title: T('Select  (x)') }, icon('check'));
export function setCheck(el, on) { el.classList.toggle('on', on); el.setAttribute('aria-checked', on); }

export function selBar({ edit, clear }) {
  const n = h('b');
  const el = h('div.selbar', { role: 'toolbar', 'aria-label': T('Selected issues'), hidden: true },
    h('span.selbar-n', n, T(' selected')),
    h('button.btn.primary', { title: T('Bulk edit the selected issues  (X)'), onclick: edit }, T('Edit…'), h('kbd', 'X')),
    h('button.btn.ghost', { title: T('Clear the selection  (esc)'), 'aria-label': T('Clear the selection'), onclick: clear }, icon('x')));
  return { el, set: c => { n.textContent = c; el.hidden = !c; } };
}
