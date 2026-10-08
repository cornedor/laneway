// App URLs and where a navigation leads: pure, so node tests it (jstest/nav.test.mjs).
// A URL is '/path?query' (the '#/path?query' of old links reads the same); ?issue=KEY is the issue panel, on any route.
import { T } from './i18n.js';

// routeTitle: a route's title, translated (the table in views/index.js holds the English).
export const routeTitle = r => ({ Home: T('Home'), Board: T('Board'), Issue: T('Issue'), Planning: T('Planning'), Reports: T('Reports'), Roadmap: T('Roadmap'), 'My work': T('My work'), Inbox: T('Inbox'), Standup: T('Standup'), Agents: T('Agents'), 'Merge request': T('Merge request'), 'Merge requests': T('Merge requests'), Rules: T('Rules'), Settings: T('Settings') }[r.title] || r.title);

export function split(url) {
  const s = String(url || '').replace(/^#/, '');
  const i = s.indexOf('?');
  const path = i < 0 ? s : s.slice(0, i);
  return { path, query: Object.fromEntries(new URLSearchParams(i < 0 ? '' : s.slice(i + 1))) };
}

export function join(path, query) {
  const q = new URLSearchParams();
  for (const [k, v] of Object.entries(query || {})) if (v != null && v !== '') q.set(k, v);
  const s = q.toString();
  return path + (s ? '?' + s : '');
}

// withQuery: the URL with patch's keys set, or dropped when null or ''.
export function withQuery(url, patch) {
  const { path, query } = split(url);
  return join(path, { ...query, ...patch });
}

// tidy: the URL as laneway writes it: the start route for none, issue keys in capitals (a typed demo-12).
const KEY = /^[a-z][a-z0-9_]*-\d+$/i;
export function tidy(url, start) {
  const { path, query } = split(url);
  if (query.issue && KEY.test(query.issue)) query.issue = query.issue.toUpperCase();
  const p = path && path !== '/' ? path.replace(/^\/issue\/([^/]+)/, (m, k) => (KEY.test(k) ? '/issue/' + k.toUpperCase() : m)) : start;
  const out = join(p, query);
  return out === join(path, split(url).query) ? url : out;
}

// sameBut: whether two queries differ at most in the keys named.
export function sameBut(a, b, ...keys) {
  const strip = q => Object.entries(q || {}).filter(([k]) => !keys.includes(k)).sort(([x], [y]) => (x < y ? -1 : x > y ? 1 : 0));
  return JSON.stringify(strip(a)) === JSON.stringify(strip(b));
}

// target: the URL app.go(to) leads to from cur, or null when it leads nowhere new: the same URL, or a bare
// route name (g b, the Board link) on the route already shown without a view of its own (a sprint, a JQL view).
// The open panel comes along (?issue), except onto the issue page. nameOf(path) is the route a path mounts.
export function target(cur, to, { panel = null, nameOf = () => '' } = {}) {
  const t = split('/' + String(to).replace(/^#?\/?/, '')), c = split(cur);
  const name = nameOf(t.path);
  if (name && name === nameOf(c.path) && t.path.replace(/\/$/, '') === '/' + name && !Object.keys(t.query).length && sameBut(c.query, {}, 'issue')) return null;
  if (panel && !('issue' in t.query) && name !== 'issue') t.query.issue = panel;
  const url = join(t.path, t.query);
  return url === join(c.path, c.query) ? null : url;
}
