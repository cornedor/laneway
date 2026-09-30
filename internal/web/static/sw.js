// Caches the app shell (this binary's files) so a reload is instant; never touches /api.
// The server fills in VERSION (a hash of the embedded files) and FILES: a new binary is a new cache.
const VERSION = '__VERSION__';
const FILES = __FILES__;
const CACHE = 'lw-' + VERSION;

self.addEventListener('install', e => {
  e.waitUntil(caches.open(CACHE).then(c => c.addAll(FILES.map(f => new Request(f, { cache: 'reload' })))).then(() => self.skipWaiting()));
});
self.addEventListener('activate', e => {
  e.waitUntil(caches.keys().then(ks => Promise.all(ks.filter(k => k.startsWith('lw-') && k !== CACHE).map(k => caches.delete(k)))).then(() => self.clients.claim()));
});
self.addEventListener('fetch', e => {
  const r = e.request, u = new URL(r.url);
  if (r.method !== 'GET' || u.origin !== location.origin || u.pathname.startsWith('/api/')) return;
  const path = r.mode === 'navigate' || u.pathname === '/index.html' ? '/' : u.pathname;
  e.respondWith(caches.open(CACHE).then(c => c.match(path)).then(hit => hit || fetch(r)));
});
