// Hash URLs and where a navigation leads: pure, so node tests it (jstest/nav.test.mjs).
// A hash is '#/path?query'; ?issue=KEY is the issue panel, on any route.

export function split(hash) {
  const s = String(hash || '').replace(/^#/, '');
  const i = s.indexOf('?');
  const path = i < 0 ? s : s.slice(0, i);
  return { path, query: Object.fromEntries(new URLSearchParams(i < 0 ? '' : s.slice(i + 1))) };
}

export function join(path, query) {
  const q = new URLSearchParams();
  for (const [k, v] of Object.entries(query || {})) if (v != null && v !== '') q.set(k, v);
  const s = q.toString();
  return '#' + path + (s ? '?' + s : '');
}

// withQuery: the hash with patch's keys set, or dropped when null or ''.
export function withQuery(hash, patch) {
  const { path, query } = split(hash);
  return join(path, { ...query, ...patch });
}

// sameBut: whether two queries differ at most in the keys named.
export function sameBut(a, b, ...keys) {
  const strip = q => Object.entries(q || {}).filter(([k]) => !keys.includes(k)).sort(([x], [y]) => (x < y ? -1 : x > y ? 1 : 0));
  return JSON.stringify(strip(a)) === JSON.stringify(strip(b));
}

// target: the hash app.go(to) leads to from cur, or null when it leads nowhere new: the same URL, or a bare
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
