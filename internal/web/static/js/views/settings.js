// Settings: topic headings, `/` filters by name. j/k move, enter/space/→ change, ← back, esc leaves.
//
// Board prefs (app.prefs, per site) for the board views:
//   board.mode     'lanes' | 'list'                  (default: session.ui.DefaultMode or 'lanes')
//   board.fields   comma list of type,priority,status,points,assignee,parent   (default: all)
//   board.refresh  seconds between idle refetches, '0' = off (default 120)
//   board.limit    cards fetched per view (default 500)
// Changing one emits bus 'prefs' {key, value}.
import { h, clear } from '../lib/dom.js';
import { css } from '../lib/css.js';
import theme from '../lib/theme.js';
import api from '../lib/api.js';
import { kbd } from '../lib/keys.js';

css('settings');

const FIELDS = ['type', 'priority', 'status', 'points', 'assignee', 'parent'];
const cycle = (list, cur, d) => list[(Math.max(0, list.indexOf(cur)) + d + list.length) % list.length];

export default function mount(el, { app, scope }) {
  const pref = (k, d) => app.prefs.get(k, d);
  const setPref = (k, v) => { app.prefs.set(k, v); app.bus.emit('prefs', { key: k, value: String(v) }); };
  const ui = (app.session && app.session.ui) || {};
  const s = app.session || {};

  // An option: {name, desc, section, render() → control node, change(dir)?}
  const choice = (name, desc, section, list, get, set, label = x => x) => ({
    name, desc, section,
    render: () => h('span.st-val', h('button.btn.ghost', { tabindex: -1, onclick: () => set(cycle(list, get(), 1)) }, label(get()))),
    change: d => set(cycle(list, get(), d)),
  });
  const info = (name, value, section, desc = '') => ({ name, desc, section, render: () => h('span.st-val.mono', value) });

  const options = [
    { name: 'Theme', desc: 'T cycles through them anywhere', section: 'Appearance',
      render: () => h('span.st-swatches', theme.presets.map(p => swatch(p))),
      change: d => { theme.set(cycle(theme.presets.map(p => p.id), theme.current, d)); refresh(); } },
    { name: 'Accent', desc: 'highlights, focus, primary buttons', section: 'Appearance',
      render: () => h('span.st-swatches',
        h('button.st-dot.none', { tabindex: -1, title: 'Theme default', class: theme.accent ? '' : 'on', onclick: () => { theme.setAccent(''); refresh(); } }, '∅'),
        theme.accents.map(c => h('button.st-dot', { tabindex: -1, title: c, class: theme.accent === c ? 'on' : '', style: { background: c }, onclick: () => { theme.setAccent(c); refresh(); } })),
        h('input.st-color', { type: 'color', title: 'Custom colour', value: /^#[0-9a-f]{6}$/i.test(theme.accent) ? theme.accent : '#5b8def', tabindex: -1, oninput: e => { theme.setAccent(e.target.value); }, onchange: refresh })),
      change: d => { const l = ['', ...theme.accents]; theme.setAccent(cycle(l, theme.accent, d)); refresh(); } },
    choice('Density', 'spacing of rows and panels', 'Appearance', ['compact', 'normal', 'roomy'], () => theme.density, v => { theme.setDensity(v); refresh(); }),
    choice('Font size', 'base text size', 'Appearance', [0, 12, 13, 14, 15, 16, 18], () => theme.fontSize, v => { theme.setFontSize(v); refresh(); }, v => (v ? v + 'px' : 'from density')),
    choice('Motion', 'animations and transitions', 'Appearance', ['auto', 'reduce'], () => theme.motion, v => { theme.setMotion(v); refresh(); }, v => (v === 'reduce' ? 'reduced' : 'system')),
    { name: 'Custom tokens', desc: 'CSS variables as JSON, e.g. {"--bg": "#101010", "--radius": "2px"}; ctrl+enter applies', section: 'Appearance', wide: true, render: customEditor, change: () => editor && editor.focus() },

    { name: 'Keyboard', section: 'Keyboard', desc: 'Bindings available on this page. Each view lists its own under ?. Remapping is not available in the browser yet.', wide: true, static: true, render: () => h('span') },
    ...app.keys.active().map(b => ({ name: b.desc, section: 'Keyboard', mono: true, render: () => h('span.st-val', kbd(b.spec).map(k => h('kbd', k))), meta: b.group })),

    choice('Default mode', 'how the board opens', 'Board', ['lanes', 'list'], () => pref('board.mode', ui.DefaultMode || 'lanes'), v => { setPref('board.mode', v); refresh(); }),
    { name: 'Card fields', desc: 'what cards and list rows show', section: 'Board',
      render: () => h('span.st-val', h('button.btn.ghost', { tabindex: -1, onclick: editFields }, fields().join(', ') || 'none')),
      change: () => editFields() },
    choice('Auto refresh', 'refetch an idle board', 'Board', ['0', '30', '60', '120', '300'], () => pref('board.refresh', '120'), v => { setPref('board.refresh', v); refresh(); }, v => (v === '0' ? 'off' : v >= 60 ? v / 60 + 'm' : v + 's')),
    choice('Card limit', 'cards fetched per view', 'Board', ['100', '200', '500', '1000'], () => pref('board.limit', '500'), v => { setPref('board.limit', v); refresh(); }),

    info('Site', s.site || '-', 'Data'), info('Jira', s.baseURL || '-', 'Data'),
    info('Signed in as', (s.me && s.me.DisplayName) || '-', 'Data'), info('Version', s.version || 'dev', 'Data'),
    action('Clear cached data', 'the browser copy of API answers; reloaded on demand', 'Data', () => { api.forget(); app.ui.toast('Caches cleared', { kind: 'ok' }); }),
    action('Clear recent issues', 'the palette’s recent list and search history', 'Data', () => {
      try { for (const k of Object.keys(localStorage)) if (/^lw:(recent|jqlhist|cmdrecent):/.test(k)) localStorage.removeItem(k); } catch (e) { /* ignore */ }
      app.ui.toast('Recents cleared', { kind: 'ok' });
    }),
  ];
  const fields = () => { const v = pref('board.fields', ''); return v ? v.split(',').filter(f => FIELDS.includes(f)) : FIELDS.slice(); };
  async function editFields() {
    const r = await app.ui.pick({ title: 'Card fields', items: FIELDS, multi: true, selected: fields(), placeholder: 'Toggle with space, confirm with enter' });
    if (r) { setPref('board.fields', r.join(',')); refresh(); }
  }
  function action(name, desc, section, run) {
    return { name, desc, section, render: () => h('span.st-val', h('button.btn', { tabindex: -1, onclick: run }, 'Run')), change: run };
  }
  function swatch(p) {
    const t = theme.tokens(p.id);
    return h('button.st-sw', { tabindex: -1, class: theme.current === p.id ? 'on' : '', title: p.name, onclick: () => { theme.set(p.id); refresh(); } },
      h('span.st-sw-box', { style: { background: t.bg, borderColor: t.border } },
        h('i', { style: { background: t['bg-2'] } }), h('i', { style: { background: t.fg } }), h('i', { style: { background: t.accent } }), h('i', { style: { background: t.ok } })),
      h('span.st-sw-name', p.name));
  }
  let editor = null;
  function customEditor() {
    const cur = theme.custom;
    const ta = editor = h('textarea.input.st-json', { rows: 5, spellcheck: false, placeholder: '{\n  "--bg": "#101010"\n}', value: Object.keys(cur).length ? JSON.stringify(cur, null, 2) : '' });
    const err = h('div.st-err');
    const apply = () => {
      try {
        const v = ta.value.trim() ? JSON.parse(ta.value) : {};
        if (!v || typeof v !== 'object' || Array.isArray(v)) throw new Error('Expected an object of "--token": "value"');
        theme.setCustom(v); err.textContent = ''; app.ui.toast('Tokens applied', { kind: 'ok' });
      } catch (e) { err.textContent = e.message; }
    };
    ta.addEventListener('keydown', e => {
      if (e.key === 'Enter' && (e.ctrlKey || e.metaKey)) { e.preventDefault(); apply(); }
    });
    return h('div.st-edit', ta, err, h('div.row', h('button.btn', { tabindex: -1, onclick: apply }, 'Apply'),
      h('button.btn.ghost', { tabindex: -1, onclick: () => { ta.value = ''; apply(); } }, 'Reset')));
  }

  // ---- view
  let q = '', sel = 0, rows = [];
  const filter = h('input.input.st-filter', { type: 'search', placeholder: 'Filter settings  (/)', spellcheck: false, oninput: e => { q = e.target.value.trim().toLowerCase(); sel = 0; draw(); } });
  const list = h('div.st-list');
  el.append(h('div.st', h('div.st-head', h('h2', 'Settings'), filter), list, h('div.st-foot', 'j/k move · enter/space/→ change · ← back · / filter · esc leaves')));

  const shown = () => options.filter(o => !q || (o.name + ' ' + o.desc + ' ' + o.section + ' ' + (o.meta || '')).toLowerCase().includes(q));
  function rowFor(o) {
    return h('div.st-row' + (o.wide ? '.wide' : ''), { onclick: e => { const i = rows.indexOf(o); if (i >= 0) { sel = i; mark(); } } },
      h('div.st-name', h('div', o.name, o.meta && h('span.chip', o.meta)), o.desc && h('div.st-desc', o.desc)), o.render());
  }
  function draw() {
    const vis = shown();
    rows = vis.filter(o => !o.static);
    sel = Math.min(sel, Math.max(0, rows.length - 1));
    clear(list);
    let sec = null, body = null;
    for (const o of vis) {
      if (o.section !== sec) { sec = o.section; list.append(h('h3.st-h', sec)); body = h('div.st-sec'); list.append(body); }
      o.el = rowFor(o);
      body.append(o.el);
    }
    if (!vis.length) list.append(h('div.empty', 'No settings match “' + q + '”'));
    mark();
  }
  function mark() {
    for (const o of options) if (o.el) o.el.classList.toggle('sel', rows[sel] === o);
    const o = rows[sel]; if (o && o.el && o.el.scrollIntoView) o.el.scrollIntoView({ block: 'nearest' });
  }
  function refresh() { // re-render the rows in place: a value changed
    for (const o of options) if (o.el && o.el.isConnected) { const n = rowFor(o); o.el.replaceWith(n); o.el = n; }
    mark();
  }
  const change = d => { const o = rows[sel]; if (o && o.change) o.change(d); };
  const go = d => { if (rows.length) { sel = (sel + d + rows.length) % rows.length; mark(); } };

  scope.bind(['j', 'ArrowDown'], () => go(1), 'next option', { group: 'Settings' });
  scope.bind(['k', 'ArrowUp'], () => go(-1), 'previous option', { group: 'Settings' });
  scope.bind(['Enter', 'Space', 'ArrowRight', 'l'], () => change(1), 'change option', { group: 'Settings' });
  scope.bind(['ArrowLeft', 'h'], () => change(-1), 'change back', { group: 'Settings' });
  scope.bind('/', () => { filter.focus(); filter.select(); }, 'filter settings', { group: 'Settings' });
  scope.bind('Escape', () => { if (q) { filter.value = ''; q = ''; draw(); } else history.length > 1 ? history.back() : app.go('/board'); }, 'leave settings', { group: 'Settings' });
  scope.bind('Escape', () => { filter.blur(); el.focus(); }, '', { input: true, hidden: true });
  scope.bind('Enter', () => { filter.blur(); el.focus(); }, '', { input: true, hidden: true });
  scope.bind('ArrowDown', () => { filter.blur(); el.focus(); go(1); }, '', { input: true, hidden: true });

  draw();
  el.focus();
  return () => { editor = null; };
}
