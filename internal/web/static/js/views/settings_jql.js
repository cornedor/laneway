// The JQL editors on the settings page: ui.quick_filters and ui.views as rows of a name and a JQL clause, and
// ui.my_work_jql as one query; each JQL input completes as you type (lib/jqlinput.js) and shows how many issues it
// finds. Each change writes the config file (settings_config.js saver); a row without a name or JQL waits.
// Keys: enter on the row starts editing, tab walks the inputs, esc closes the completions, then leaves.
import { h, debounce } from '../lib/dom.js';
import { icon } from '../lib/icons.js';
import { jqlInput, dismiss } from '../lib/jqlinput.js';
import { saver } from './settings_config.js';
import { T, Tn } from '../lib/i18n.js';

const LISTS = {
  quick_filters: { field: 'QuickFilters', add: T('Add quick filter'), removeThis: T('Remove this quick filter'), noun: T('quick filter'), empty: T("No quick filters yet. Each is a name and a JQL clause, ANDed with the board's query when on.") },
  views: { field: 'Views', add: T('Add view'), removeThis: T('Remove this view'), noun: T('view'), empty: T("No views yet. Each is a name and a JQL clause, ANDed with the board's query.") },
};

const countText = n => Tn(n, '%s issue', '%s issues', n.toLocaleString());

// designJQL gives the settings rows of ui.quick_filters, ui.views and ui.my_work_jql their editors.
export function designJQL(app, host, options) {
  const ui = () => (app.session && app.session.ui) || {};
  const mine = options.filter(o => LISTS[o.name] || o.name === 'my_work_jql');
  if (!mine.length) return () => {};

  // ---- keyboard: a scope over the settings page's own while editing (as the card designer's)
  let keys = null, active = null;
  const outside = e => { if (!e.target.closest || !e.target.closest('.jq, .modal, .pick')) leave(); };
  const leave = () => {
    if (!keys) return;
    keys.dispose(); keys = null;
    document.removeEventListener('mousedown', outside, true);
    host.end();
    const o = active; active = null;
    if (o) host.redraw(o);
  };
  const editing = o => {
    if (keys && active === o) return;
    if (keys) leave();
    active = o;
    host.begin({ o, multi: true, commit: () => {}, cancel: leave });
    document.addEventListener('mousedown', outside, true);
    keys = app.keys.scope('jql', { layer: 2, covers: () => true });
    keys.bind('Escape', () => { if (!dismiss()) leave(); }, T('close the completions, then leave'), { group: 'JQL', bar: T('leave'), input: true });
  };
  const focusFirst = o => { const i = o.el && o.el.querySelector('.jqi-in, .jq-name, .jq-new'); if (i) i.focus(); };

  // count shows how many issues r.jql finds in r.countEl, or Jira's complaint in r.errEl.
  function counter(r) {
    let seq = 0;
    const paint = () => {
      if (r.countEl) { r.countEl.textContent = r.err ? '' : r.count === undefined ? '…' : r.count == null ? '' : countText(r.count); }
      if (r.errEl) { r.errEl.textContent = r.err; r.errEl.hidden = !r.err; }
    };
    const run = debounce(() => {
      const q = r.jql.trim(), n = ++seq;
      if (!q) { r.count = null; r.err = ''; paint(); return; }
      app.api.get('/jql/count?jql=' + encodeURIComponent(q)).then(d => { if (n === seq) { r.count = d.Count; r.err = ''; paint(); } },
        e => { if (n === seq) { r.count = null; r.err = e.message; paint(); } });
    }, 500);
    return { run: () => { r.count = undefined; paint(); run(); }, paint };
  }
  const row = (name, jql) => { const r = { name, jql, count: null, err: '' }; r.counter = counter(r); if (jql.trim()) r.counter.run(); return r; };
  const jqlField = (r, onchange, label) => jqlInput({ value: r.jql, placeholder: 'status = "In Progress" AND assignee = currentUser()', label,
    oninput: v => { r.jql = v; r.counter.run(); onchange(); } }).el;
  const hits = r => (r.countEl = h('span.cd-hits.faint.jq-count', { title: T('Across the whole site; a board narrows it further') }));
  const errLine = r => (r.errEl = h('div.st-err', { hidden: !r.err }, r.err));

  // ---- quick_filters and views: rows of a name and a clause
  for (const o of mine.filter(o => LISTS[o.name])) {
    const L = LISTS[o.name];
    const rows = (ui()[L.field] || []).map(f => row(f.Name || '', f.JQL || ''));
    const ready = r => r.name.trim() && r.jql.trim();
    const save = debounce(saver(app, o, () => { const v = rows.filter(ready).map(r => ({ name: r.name.trim(), jql: r.jql.trim() })); return v.length ? v : null; }), 400);
    const waitText = () => { const n = rows.filter(r => !ready(r) && (r.name.trim() || r.jql.trim())).length; return n ? T('%d without a name or JQL, not saved', n) : ''; };
    const changed = () => {
      save();
      const w = o.el && o.el.querySelector('.jq-wait');
      if (w) w.textContent = waitText();
    };
    const add = (name = '', jql = '') => {
      rows.push(row(name, jql));
      host.redraw(o); changed();
      const ns = o.el.querySelectorAll(name ? '.jqi-in' : '.jq-name');
      if (ns.length) ns[ns.length - 1].focus();
    };
    const fromJira = async () => {
      let fs = [];
      try { fs = await app.api.get('/filters'); } catch (e) { app.ui.toast(e.message, { kind: 'err' }); return; }
      if (!fs.length) { app.ui.toast(T('No starred Jira filters'), { kind: 'err' }); return; }
      const f = await app.ui.pick({ title: T('Your starred Jira filters'), items: fs, label: f => f.Name, placeholder: T('Filter…') });
      if (f) add(f.Name, f.JQL);
    };
    let drag = -1;
    const rule = (r, i) => h('div.cd-rule.jq-rule', {
      ondragover: e => { if (drag >= 0) { e.preventDefault(); e.currentTarget.classList.add('over'); } },
      ondragleave: e => e.currentTarget.classList.remove('over'),
      ondrop: e => { if (drag < 0) return; e.preventDefault(); const [x] = rows.splice(drag, 1); rows.splice(i, 0, x); drag = -1; host.redraw(o); changed(); },
    },
    h('div.cd-rhead',
      h('span.cd-grip', { draggable: true, title: T('Drag to reorder'), ondragstart: e => { drag = i; e.dataTransfer.setData('text/plain', String(i)); e.dataTransfer.effectAllowed = 'move'; }, ondragend: () => { drag = -1; } }, icon('grip-vertical')),
      h('input.input.jq-name', { type: 'text', value: r.name, placeholder: T('Name'), spellcheck: false, autocomplete: 'off', 'aria-label': T('Name'),
        oninput: e => { r.name = e.target.value; changed(); } }),
      jqlField(r, changed, r.name ? T('JQL of %s', r.name) : 'JQL'),
      hits(r),
      h('button.btn.ghost.st-x', { type: 'button', title: L.removeThis, 'aria-label': T('Remove %s', r.name || L.noun), onclick: () => { rows.splice(i, 1); host.redraw(o); changed(); } }, '×')),
    errLine(r));
    o.wide = true;
    o.activate = o.change = () => { editing(o); focusFirst(o); };
    o.reset = () => { rows.length = 0; host.redraw(o); changed(); };
    o.render = () => {
      const el = h('div.cd.jq', { onfocusin: () => editing(o) },
        rows.length ? rows.map(rule) : h('div.faint', L.empty),
        h('div.row',
          h('button.btn.jq-new', { type: 'button', onclick: () => add() }, icon('plus'), L.add),
          h('button.btn.ghost', { type: 'button', title: T('Copy one of your starred Jira filters'), onclick: fromJira }, T('From a Jira filter')),
          h('span.spacer'), h('span.faint.jq-wait', waitText()), h('span.cd-status'),
          rows.length > 0 && h('button.btn.ghost', { type: 'button', onclick: () => o.reset() }, T('Remove all'))));
      rows.forEach(r => r.counter.paint());
      return el;
    };
  }

  // ---- my_work_jql: one query
  const M = mine.find(o => o.name === 'my_work_jql');
  if (M) {
    const r = row('', typeof M.st.Value === 'string' ? M.st.Value : '');
    const save = debounce(saver(app, M, () => r.jql.trim() || null), 600);
    M.wide = true;
    M.activate = M.change = () => { editing(M); focusFirst(M); };
    M.reset = () => { r.jql = ''; r.counter.run(); host.redraw(M); save(); };
    M.render = () => {
      const el = h('div.cd.jq', { onfocusin: () => editing(M) },
        h('div.cd-rhead', jqlField(r, save, T('My work JQL')), hits(r)),
        errLine(r),
        h('div.row', h('span.faint', r.jql.trim() ? '' : T('Empty: %s', M.st.Default || T('the default'))), h('span.spacer'), h('span.cd-status')));
      r.counter.paint();
      return el;
    };
  }
  return () => leave();
}
