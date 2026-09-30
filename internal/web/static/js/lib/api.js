// JSON API client. GETs are de-duplicated while in flight; `swr` answers from
// a persisted cache at once and again when fresh data arrives.
const inflight = new Map();
const mem = new Map();
let site = '';
export const setSite = s => { site = s; };

export class ApiError extends Error { constructor(msg, status) { super(msg); this.status = status; } }

async function call(method, path, body, signal) {
  const res = await fetch('/api' + path, {
    method,
    headers: body === undefined ? undefined : { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
    signal,
  });
  let data = null;
  try { data = await res.json(); } catch (e) { /* empty body */ }
  if (!res.ok) throw new ApiError((data && data.error) || res.statusText, res.status);
  if (res.status === 202) window.dispatchEvent(new CustomEvent('lw:queued')); // a write kept for when Jira is back
  return data;
}

// `signal` (an AbortSignal) cancels the request; such a call is never shared.
export function get(path, { fresh = false, signal } = {}) {
  if (signal) return call('GET', path, undefined, signal);
  if (!fresh && inflight.has(path)) return inflight.get(path);
  const p = call('GET', path).then(d => { mem.set(path, d); return d; }).finally(() => inflight.delete(path));
  inflight.set(path, p);
  return p;
}
export const post = (path, body = {}) => write('POST', path, body);
export const put = (path, body = {}) => write('PUT', path, body);
export const del = (path, body) => write('DELETE', path, body);
function write(method, path, body) {
  // Any write may change what cached reads say.
  for (const k of mem.keys()) mem.delete(k);
  return call(method, path, body);
}

const lsKey = p => 'lw:c:' + site + ':' + p;
function readLS(path) { try { const v = localStorage.getItem(lsKey(path)); return v ? JSON.parse(v) : undefined; } catch (e) { return undefined; } }
function writeLS(path, data) {
  try { const s = JSON.stringify(data); if (s.length < 600000) localStorage.setItem(lsKey(path), s); } catch (e) { /* quota */ }
}

// swr(path, onData): onData(data, {cached}) runs with the stored copy first (if
// any), then with the fresh one. Resolves with the fresh data; rejects if the
// fetch fails (after a cached answer the caller may keep showing it).
export function swr(path, onData, { persist = true } = {}) {
  const hit = mem.has(path) ? mem.get(path) : persist ? readLS(path) : undefined;
  if (hit !== undefined) onData(hit, { cached: true });
  return get(path, { fresh: hit !== undefined }).then(d => { if (persist) writeLS(path, d); onData(d, { cached: false }); return d; });
}
export function forget() { mem.clear(); try { for (const k of Object.keys(localStorage)) if (k.startsWith('lw:c:')) localStorage.removeItem(k); } catch (e) { /* ignore */ } }
export const api = { get, post, put, del, swr, forget, setSite };
export default api;
