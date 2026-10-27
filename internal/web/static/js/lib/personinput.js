// A person input that searches as you type (/users), as the issue input does (issueinput.js): with nothing
// typed it lists the people the field ranks first (internal/people), the merge requests' suggestions filling
// in when they come. Who is chosen shows under it, × takes them off.
import { h, clear } from './dom.js';
import api from './api.js';
import { combo } from './combo.js';
import { avatar } from './badge.js';
import { T } from './i18n.js';

// Why a person ranks high (people.Rank's source).
export const why = () => ({ frequent: T('picked often'), codeowners: T('code owner'), reviewers: T('reviewed this code'), blame: T('wrote this code') });

// personInput(o) → {el, input, get() → [{AccountID, DisplayName}]}: o.issue or o.project (who can be
// assigned there), o.field (an id, 'assignee', 'reporter') and o.name rank them and count the pick, o.multi
// keeps several, o.value the people chosen, o.me ({AccountID, DisplayName}) lists you as Me, o.label,
// o.onchange().
export function personInput(o = {}) {
  let people = [...(o.value || [])];
  const input = h('input.input.iin-in', { type: 'text', placeholder: o.multi ? T('Add someone…') : T('Search people…'),
    spellcheck: false, autocomplete: 'off', autocapitalize: 'off', 'aria-label': o.label || T('Person') });
  const chosen = h('div.iin-sum.pin-chosen');
  const reasons = why();
  const scopeQ = new URLSearchParams(o.issue ? { issue: o.issue } : { project: o.project || '' });
  if (o.field) { scopeQ.set('field', o.field); scopeQ.set('name', o.name || ''); }
  const me = o.me && o.me.AccountID ? o.me : null;

  const name = u => (me && u.AccountID === me.AccountID ? T('Me · %s', u.DisplayName) : u.DisplayName);
  const row = u => [avatar(u.DisplayName), h('span.iin-text', name(u)), u.Why && h('span.faint.pin-why', reasons[u.Why] || '')];
  // Ranked first, then you, then the rest in Jira's order; the ones chosen left out.
  const list = us => {
    us = us.filter(u => !people.some(p => p.AccountID === u.AccountID));
    const i = me ? us.findIndex(u => u.AccountID === me.AccountID) : -1;
    const mine = i >= 0 ? us.splice(i, 1) : [];
    return [...us.filter(u => u.Why), ...mine, ...us.filter(u => !u.Why)];
  };
  let suggested = null, asked = false; // the list with the merge requests' part, once it came
  const find = q => (!q && suggested ? Promise.resolve(list(suggested))
    : api.get('/users?' + scopeQ + '&q=' + encodeURIComponent(q), { fresh: true }).then(list));

  const draw = () => {
    clear(chosen).append(...people.map(u => h('span.pin-person', avatar(u.DisplayName), h('span', name(u)),
      h('button.pin-x', { type: 'button', title: T('Remove %s', u.DisplayName), 'aria-label': T('Remove %s', u.DisplayName),
        onclick: () => { people = people.filter(p => p.AccountID !== u.AccountID); draw(); if (o.onchange) o.onchange(); input.focus(); } }, '×'))));
  };
  const cb = combo(input, {
    find, row, key: u => u.AccountID, open: true,
    none: q => (q ? T('No one matches “%s”', q) : T('No one to choose')),
    accept: u => {
      const p = { AccountID: u.AccountID, DisplayName: u.DisplayName };
      people = o.multi ? [...people, p] : [p];
      input.value = '';
      draw();
      if (o.onchange) o.onchange();
      if (o.field) api.post('/users/picked', { Where: o.issue || o.project, Field: o.field, Account: p.AccountID }).catch(() => {});
    },
  });
  // The merge requests' suggestions take a while: asked once, on the first focus, and shown when they come.
  if (o.field && o.issue) {
    input.addEventListener('focus', () => {
      if (asked) return;
      asked = true;
      api.get('/users?' + scopeQ + '&q=&suggest=1', { fresh: true }).then(us => { suggested = us; cb.update('', list(us)); }, () => { asked = false; });
    });
  }
  draw();

  return { el: h('div.iin.pin', cb.el, chosen), input, get: () => people };
}
