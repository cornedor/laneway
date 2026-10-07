// A link to an issue on this Jira site, drawn as a pill: [status] KEY summary (avatar).
// It opens the issue here instead of in Jira.
import { h } from './dom.js';
import { api } from './api.js';
import { bus } from './bus.js';
import { statusPill, avatar } from './badge.js';

// jiraKey is the issue key href points at on the site at base (…/browse/ABC-1,
// or a board's …?selectedIssue=ABC-1), '' when it is not one.
export function jiraKey(href, base) {
  base = String(base || '').replace(/\/+$/, '');
  if (!base || !String(href).toLowerCase().startsWith(base.toLowerCase() + '/')) return '';
  const m = /\/browse\/([A-Za-z][A-Za-z0-9_]*-\d+)(?=$|[/?#])/.exec(href) || /[?&]selectedIssue=([A-Za-z][A-Za-z0-9_]*-\d+)(?=$|[&#])/.exec(href);
  return m ? m[1].toUpperCase() : '';
}

const cards = new Map(); // key → Promise of its card, null when it can't be had
bus.on('issue:changed', ({ key } = {}) => cards.delete(key));
const card = key => {
  if (!cards.has(key)) cards.set(key, api.get('/issues/' + encodeURIComponent(key) + '/card').catch(() => null));
  return cards.get(key);
};

const catOf = c => (c.Done ? 'done' : c.InProgress ? 'indeterminate' : 'new');

// issuePill draws key at once and fills in the rest when its card comes.
export function issuePill(key, onKey, url) {
  const sum = h('span.ip-sum'), a = h('a.issue-pill', { href: '/issue/' + key, title: url || key, onclick: e => { if (e.metaKey || e.ctrlKey) return; e.preventDefault(); onKey(key); } },
    h('span.ip-key', key));
  card(key).then(c => {
    if (!c) return a.classList.add('missing');
    sum.textContent = c.Summary || '';
    a.title = key + ' ' + (c.Summary || '') + (c.Assignee ? ' · ' + c.Assignee : '');
    a.prepend(statusPill(c.Status, catOf(c)));
    a.append(sum);
    if (c.Assignee) a.append(avatar(c.Assignee, c.AvatarURL, 16));
  });
  return a;
}
