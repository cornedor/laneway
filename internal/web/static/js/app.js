// laneway web shell: session, hash router, panel host, global keys, command registry.
// Feature views live in js/views/*.js and talk to the shell only through `app`.
import { h, clear, $ } from './lib/dom.js';
import api from './lib/api.js';
import bus from './lib/bus.js';
import { keys } from './lib/keys.js';
import theme from './lib/theme.js';
import { onChange as onMetrics } from './lib/metrics.js';
import * as ui from './lib/ui.js';
import { routes } from './views/index.js';
import * as chromeBars from './lib/chrome.js';
import * as store from './lib/store.js';

// ---- commands: the palette lists these. register({id, title, keys?, group?, run, when?}) → unregister
const cmds = new Map();
const commands = {
  register(c) { cmds.set(c.id, c); return () => cmds.delete(c.id); },
  list: () => [...cmds.values()].filter(c => !c.when || c.when()),
  run(id) { const c = cmds.get(id); if (c) return c.run(); },
};

// Look-and-feel prefs are mirrored unscoped (boot.js and fonts.js read them before the site is known); the rest per site.
const GLOBAL_PREF = /^(font|terminal)\./;

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
  // The issue panel. Board/list/etc. call app.panel.open(key[, {card}]); the issue module renders it,
  // its head from `card` (the view's copy) until its own data arrives.
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
        app.panel.cleanup = m.mountIssue(el, key, { app, full: false, card: opts.card }) || null;
      } catch (e) { if (reloadOnce(e)) return; clear(el).append(h('div.empty', 'Issue panel not available: ' + e.message)); }
      bus.emit('panel', { key });
    },
    close() {
      const el = $('#panel'); app.panel.cleanup && app.panel.cleanup(); app.panel.cleanup = null;
      app.panel.key = null; el.hidden = true; clear(el); document.body.classList.remove('has-panel');
      if (viewTitle) document.title = viewTitle;
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
    set(k, v) { app.prefs.data[k] = v; api.put('/prefs/' + encodeURIComponent(k), { Value: String(v) }).catch(() => {}); if (GLOBAL_PREF.test(k)) { try { localStorage.setItem('lw:p:' + k, String(v)); } catch (e) { /* ignore */ } } else store.set('p:' + k, v); },
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
let current = null, viewToken = 0, viewTitle = '';
// A module that fails to load after a binary upgrade (old page, new files): reload once to get a matching set.
function reloadOnce(e) {
  try {
    const at = +sessionStorage.getItem('lw:reloaded') || 0;
    if (Date.now() - at < 30000) return false;
    sessionStorage.setItem('lw:reloaded', String(Date.now()));
  } catch (x) { return false; }
  console.warn('reloading after a failed import', e);
  location.reload(); return true;
}

async function navigate() {
  const hash = location.hash.slice(1) || '/board';
  const [path, qs] = hash.split('?');
  let hit = null, params = {};
  for (const r of table) { const m = path.match(r.re); if (m) { hit = r; r.names.forEach((n, i) => { if (m[i + 1]) params[n] = decodeURIComponent(m[i + 1]); }); break; } }
  const view = $('#view');
  const token = ++viewToken;
  if (current && current.cleanup) { try { current.cleanup(); } catch (e) { console.error(e); } }
  if (current && current.scope) current.scope.dispose();
  current = null; app.marked = app.listed = null;
  $('#viewbar').classList.toggle('hold', !!hit && hit.bar !== false);
  clear($('#toolbar')); clear($('#context'));
  if (!hit) {
    app.route = { name: '', params: {}, query: {} };
    viewTitle = 'Not found · laneway'; document.title = viewTitle;
    app.chrome.mark('');
    clear(view).append(h('div.empty', h('h2', 'Nothing at ' + path), h('p', h('a', { href: '#/board' }, 'Back to the board'))));
    bus.emit('route', app.route);
    return;
  }
  current = { name: hit.name, scope: keys.scope(hit.name) };
  app.route = { name: hit.name, params, query: Object.fromEntries(new URLSearchParams(qs || '')) };
  viewTitle = hit.title + ' · laneway'; document.title = viewTitle;
  app.chrome.mark(hit.name);
  clear(view).append(h('div.loading', 'Loading…'));
  let mod;
  try { mod = await hit.load(); } catch (e) {
    if (token !== viewToken) return;
    if (reloadOnce(e)) return;
    clear(view).append(h('div.empty', h('h2', hit.title), h('p', 'This view is not built yet.'), h('pre.dim', String(e.message))));
    return;
  }
  if (token !== viewToken) return;
  clear(view);
  try {
    current.cleanup = await (mod.default || mod.mount)(view, { app, params, query: app.route.query, scope: current.scope, context: $('#context'), toolbar: $('#toolbar') });
  } catch (e) { console.error(e); clear(view).append(h('div.empty', h('h2', 'Something broke'), h('pre', e.stack || e.message))); }
  if (token === viewToken) $('#viewbar').classList.remove('hold');
  bus.emit('route', app.route);
}

// ---- global keys
function globalKeys() {
  const g = keys.scope('global');
  g.bind(':', () => app.actions.palette(':'), 'command palette', { group: 'Global' });
  g.bind('ctrl+k', () => app.actions.palette(':'), 'command palette', { group: 'Global', input: true, hidden: true });
  g.bind('/', () => app.actions.palette('/'), 'search issues', { group: 'Global' });
  g.bind('g g', () => app.actions.jump(), 'jump to issue by key', { group: 'Global' });
  g.bind('?', () => import('./views/help.js').then(m => m.openHelp(app)), 'show keys', { group: 'Global' });
  g.bind('n', () => app.actions.create({ project: app.route && app.route.params.project }), 'new issue', { group: 'Global' });
  g.bind('Q', () => app.actions.palette('#'), 'JQL search', { group: 'Global' });
  g.bind('ctrl+e', () => import('./views/refine.js').then(m => m.startRefine(app)), 'refine: the view\'s issues one at a time', { group: 'Global' });
  g.bind('g t', () => { const t = theme.next(); ui.toast('Theme: ' + t); }, 'next theme', { group: 'Global' });
  g.bind('Tab', () => { const p = $('#panel'); if (p.contains(document.activeElement)) { $('#view').focus(); } else { p.tabIndex = -1; p.focus(); } }, 'focus panel / view', { group: 'Global', when: () => app.panel.key });
  g.bind('Escape', () => { if (app.panel.key) app.panel.close(); }, 'close panel', { group: 'Global', hidden: true });
  for (const r of routes) if (r.key) g.bind('g ' + r.key, () => app.go('/' + r.name), 'go to ' + r.title.toLowerCase(), { group: 'Go' });
  g.bind(['g ,', ','], () => app.go('/settings'), 'go to settings', { group: 'Go' });
  // The TUI's own view keys; a view's key of the same name wins there (board O sorts, My work W switches day/week).
  g.bind('W', () => app.go('/work?tab=day'), "today's worklogs", { group: 'Go' });
  g.bind('O', () => app.go('/work?tab=issues'), 'my work, every project', { group: 'Go' });
  g.bind('I', () => app.go('/inbox'), 'go to inbox', { group: 'Go' });
  g.bind('U', () => app.go('/standup'), 'go to standup', { group: 'Go' });
  g.bind('ctrl+g', () => app.go('/agents'), 'go to agents', { group: 'Go' });
  for (const r of routes) commands.register({ id: 'go:' + r.name, title: 'Go to ' + r.title, group: 'Go', run: () => app.go('/' + r.name) });
  commands.register({ id: 'theme:next', title: 'Theme: next', group: 'Theme', run: () => ui.toast('Theme: ' + theme.next()) });
  for (const p of theme.presets) commands.register({ id: 'theme:' + p.id, title: 'Theme: ' + p.name, group: 'Theme', run: () => theme.set(p.id) });
  commands.register({ id: 'refine', title: 'Refine the view\'s issues one at a time', group: 'Issue', run: () => import('./views/refine.js').then(m => m.startRefine(app)) });
  commands.register({ id: 'create', title: 'Create issue', group: 'Issue', run: () => app.actions.create({}) });
  commands.register({ id: 'reload', title: 'Reload data (drop caches)', group: 'App', run: () => { api.forget(); navigate(); ui.toast('Reloaded'); } });
  commands.register({ id: 'help', title: 'Keyboard help', group: 'App', run: () => import('./views/help.js').then(m => m.openHelp(app)) });
}

function chrome() {
  chromeBars.install(app);
  $('#search-btn').addEventListener('click', () => app.actions.palette('/'));
  document.addEventListener('visibilitychange', () => { if (!document.hidden) bus.emit('focus'); });
}

async function boot() {
  chrome(); globalKeys();
  import('./lib/agents.js').then(m => m.install(app)).catch(e => console.error('agents', e));
  import('./views/plan_cmds.js').then(m => m.register(app));
  import('./lib/pwa.js').then(m => m.install(app)).catch(e => console.error('pwa', e));
  try {
    app.session = await api.get('/session');
    api.setSite(app.session.site); store.setSite(app.session.site);
    const p = await api.get('/prefs'); app.prefs.data = p || {};
    onMetrics(kind => { bus.emit(kind); bus.emit('metrics', kind); });
    theme.fonts.attach(app.prefs); theme.fonts.refreshFiles(api).catch(() => {});
    try { keys.configure({ user: JSON.parse(app.prefs.get('keymap', '{}')) || {}, conf: app.session.ui.Keys || {} }); } catch (e) { console.error('keymap', e); }
  } catch (e) { $('#top').classList.remove('boot'); $('#viewbar').classList.remove('hold'); clear($('#view')).append(h('div.empty', h('h2', 'Cannot reach Jira'), h('pre', e.message))); return; }
  import('./lib/timer.js').then(m => m.install(app)).catch(e => console.error('timer', e));
  import('./lib/tools.js').then(m => m.install(app)).catch(e => console.error('tools', e));
  import('./lib/sites.js').then(m => m.install(app)).catch(e => console.error('sites', e)).finally(() => $('#top').classList.remove('boot'));
  $('.brand').title = 'laneway · ' + app.session.baseURL + (app.session.demo ? ' (demo)' : '');
  if (app.session.demo) $('#site').append(h('span.demo-badge', { title: 'Demo data, no Jira behind it' }, 'demo'));
  await import('./views/plan_ctx.js').then(m => m.sanitize(app)).catch(e => console.warn('ctx', e));
  window.addEventListener('hashchange', navigate);
  await navigate();
  (window.requestIdleCallback || setTimeout)(() => import('./views/palette.js')); // ready before the first ':'
}
boot();
