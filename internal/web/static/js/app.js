// laneway web shell: session, hash router, panel host, global keys, command registry.
// Feature views live in js/views/*.js and talk to the shell only through `app`.
import { h, clear, $ } from './lib/dom.js';
import api from './lib/api.js';
import bus from './lib/bus.js';
import { keys, kbd } from './lib/keys.js';
import theme from './lib/theme.js';
import * as ui from './lib/ui.js';
import { routes } from './views/index.js';

// ---- commands: the palette lists these. register({id, title, keys?, group?, run, when?}) → unregister
const cmds = new Map();
const commands = {
  register(c) { cmds.set(c.id, c); return () => cmds.delete(c.id); },
  list: () => [...cmds.values()].filter(c => !c.when || c.when()),
  run(id) { const c = cmds.get(id); if (c) return c.run(); },
};

export const app = {
  api, bus, keys, ui, theme, commands, routes,
  session: null,
  route: null,
  // Navigate: app.go('/board/ABC/12?issue=ABC-1') or app.go('board', {project:'ABC'}).
  go(to) { location.hash = '#' + (to.startsWith('/') ? to : '/' + to); },
  // Query params of the current hash route, updatable without remounting: app.setQuery({issue:'ABC-1'}).
  query() { return Object.fromEntries(new URLSearchParams((location.hash.split('?')[1] || ''))); },
  setQuery(patch, { replace = true } = {}) {
    const [path, qs] = location.hash.slice(1).split('?');
    const q = new URLSearchParams(qs || '');
    for (const [k, v] of Object.entries(patch)) (v == null || v === '') ? q.delete(k) : q.set(k, v);
    const s = q.toString();
    const url = '#' + path + (s ? '?' + s : '');
    replace ? history.replaceState(null, '', url) : (location.hash = url);
  },
  // The issue panel. Board/list/etc. call app.panel.open(key); the issue module renders it.
  panel: {
    key: null, cleanup: null,
    async open(key, opts = {}) {
      const el = $('#panel'); if (!key) return app.panel.close();
      if (app.panel.key === key && !el.hidden && !opts.force) return;
      app.panel.key = key; el.hidden = false; el.dataset.key = key;
      document.body.classList.add('has-panel');
      app.panel.cleanup && app.panel.cleanup(); app.panel.cleanup = null;
      clear(el).append(h('div.loading', 'Loading ' + key + '…'));
      try {
        const m = await import('./views/issue.js');
        if (app.panel.key !== key) return;
        clear(el);
        app.panel.cleanup = m.mountIssue(el, key, { app, full: false }) || null;
      } catch (e) { clear(el).append(h('div.empty', 'Issue panel not available: ' + e.message)); }
      bus.emit('panel', { key });
    },
    close() {
      const el = $('#panel'); app.panel.cleanup && app.panel.cleanup(); app.panel.cleanup = null;
      app.panel.key = null; el.hidden = true; clear(el); document.body.classList.remove('has-panel');
      bus.emit('panel', { key: null });
    },
  },
  // Lazy entry points owned by the editing module (views/fields.js, views/create.js).
  actions: {
    edit: (key, field, anchor) => import('./views/fields.js').then(m => m.editField(app, key, field, anchor)),       // field: status|priority|assignee|points|labels|summary|…
    transition: key => import('./views/fields.js').then(m => m.editField(app, key, 'status')),
    create: opts => import('./views/create.js').then(m => m.openCreate(app, opts || {})),                          // {project, parent, type, summary}
    palette: (mode) => import('./views/palette.js').then(m => m.openPalette(app, mode)),                            // mode: '' | ':' commands | '/' search | 'g' goto
    jump: () => import('./views/palette.js').then(m => m.openPalette(app, 'g')),
    bulk: keysArr => import('./views/bulk.js').then(m => m.openBulk(app, keysArr)),
  },
  // Prefs are small per-site settings kept on the server store (and mirrored locally for instant reads).
  prefs: {
    data: {},
    get: (k, d) => (k in app.prefs.data ? app.prefs.data[k] : d),
    set(k, v) { app.prefs.data[k] = v; api.put('/prefs/' + encodeURIComponent(k), { Value: String(v) }).catch(() => {}); try { localStorage.setItem('lw:p:' + k, String(v)); } catch (e) { /* ignore */ } },
  },
};
window.laneway = app;

// ---- router
const compile = r => {
  const names = [];
  const re = new RegExp('^' + r.path.replace(/\/:(\w+)(\?)?/g, (_, n, opt) => { names.push(n); return opt ? '(?:/([^/?]+))?' : '/([^/?]+)'; }) + '/?$');
  return { ...r, re, names };
};
const table = routes.map(compile);
let current = null, viewToken = 0;

async function navigate() {
  const hash = location.hash.slice(1) || '/board';
  const [path, qs] = hash.split('?');
  let hit = null, params = {};
  for (const r of table) { const m = path.match(r.re); if (m) { hit = r; r.names.forEach((n, i) => { if (m[i + 1]) params[n] = decodeURIComponent(m[i + 1]); }); break; } }
  const view = $('#view');
  if (!hit) { clear(view).append(h('div.empty', 'Nothing at ' + path)); return; }
  const token = ++viewToken;
  if (current && current.cleanup) { try { current.cleanup(); } catch (e) { console.error(e); } }
  if (current && current.scope) current.scope.dispose();
  clear($('#toolbar'));
  current = { name: hit.name, scope: keys.scope(hit.name) };
  app.route = { name: hit.name, params, query: Object.fromEntries(new URLSearchParams(qs || '')) };
  document.title = hit.title + ' · laneway';
  markNav(hit.name);
  clear(view).append(h('div.loading', 'Loading…'));
  let mod;
  try { mod = await hit.load(); } catch (e) {
    if (token !== viewToken) return;
    clear(view).append(h('div.empty', h('h2', hit.title), h('p', 'This view is not built yet.'), h('pre.dim', String(e.message))));
    return;
  }
  if (token !== viewToken) return;
  clear(view);
  try {
    current.cleanup = await (mod.default || mod.mount)(view, { app, params, query: app.route.query, scope: current.scope, toolbar: $('#toolbar') });
  } catch (e) { console.error(e); clear(view).append(h('div.empty', h('h2', 'Something broke'), h('pre', e.stack || e.message))); }
  bus.emit('route', app.route);
}
function markNav(name) { document.querySelectorAll('#nav a').forEach(a => a.classList.toggle('on', a.dataset.name === name)); }

// ---- global keys
function globalKeys() {
  const g = keys.scope('global');
  g.bind(':', () => app.actions.palette(':'), 'command palette', { group: 'Global' });
  g.bind('ctrl+k', () => app.actions.palette(':'), 'command palette', { group: 'Global', input: true, hidden: true });
  g.bind('/', () => app.actions.palette('/'), 'search issues', { group: 'Global' });
  g.bind('g g', () => app.actions.jump(), 'jump to issue by key', { group: 'Global' });
  g.bind('?', () => import('./views/help.js').then(m => m.openHelp(app)), 'show keys', { group: 'Global' });
  g.bind('c', () => app.actions.create({ project: app.route && app.route.params.project }), 'create issue', { group: 'Global' });
  g.bind('T', () => { const t = theme.next(); ui.toast('Theme: ' + t); }, 'next theme', { group: 'Global' });
  g.bind('Escape', () => { if (app.panel.key) app.panel.close(); }, 'close panel', { group: 'Global', hidden: true });
  for (const r of routes) if (r.key) g.bind('g ' + r.key, () => app.go('/' + r.name), 'go to ' + r.title.toLowerCase(), { group: 'Go' });
  g.bind('g ,', () => app.go('/settings'), 'go to settings', { group: 'Go' });
  for (const r of routes) commands.register({ id: 'go:' + r.name, title: 'Go to ' + r.title, group: 'Go', run: () => app.go('/' + r.name) });
  commands.register({ id: 'theme:next', title: 'Theme: next', group: 'Theme', run: () => ui.toast('Theme: ' + theme.next()) });
  for (const p of theme.presets) commands.register({ id: 'theme:' + p.id, title: 'Theme: ' + p.name, group: 'Theme', run: () => theme.set(p.id) });
  commands.register({ id: 'create', title: 'Create issue', group: 'Issue', run: () => app.actions.create({}) });
  commands.register({ id: 'reload', title: 'Reload data (drop caches)', group: 'App', run: () => { api.forget(); navigate(); ui.toast('Reloaded'); } });
  commands.register({ id: 'help', title: 'Keyboard help', group: 'App', run: () => import('./views/help.js').then(m => m.openHelp(app)) });
}

function chrome() {
  const nav = $('#nav');
  for (const r of routes) if (r.nav !== false) nav.append(h('a', { href: '#/' + r.name, dataset: { name: r.name } }, r.title, r.key && h('kbd', 'g' + r.key)));
  $('#search-btn').addEventListener('click', () => app.actions.palette(':'));
  const chord = $('#chord');
  keys.onChange(() => { const p = keys.pending(); chord.textContent = p ? kbd(p).join(' ') + ' …' : ''; });
  document.addEventListener('visibilitychange', () => { if (!document.hidden) bus.emit('focus'); });
  window.addEventListener('online', () => $('#conn').textContent = '');
  window.addEventListener('offline', () => $('#conn').textContent = 'offline');
}

async function boot() {
  chrome(); globalKeys();
  try {
    app.session = await api.get('/session');
    api.setSite(app.session.site);
    const p = await api.get('/prefs'); app.prefs.data = p || {};
  } catch (e) { clear($('#view')).append(h('div.empty', h('h2', 'Cannot reach Jira'), h('pre', e.message))); return; }
  $('.brand').title = app.session.baseURL + (app.session.demo ? ' (demo)' : '');
  if (app.session.demo) $('.brand').append(h('span.demo-badge', 'demo'));
  window.addEventListener('hashchange', navigate);
  await navigate();
}
boot();
