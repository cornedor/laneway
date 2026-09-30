// The palette: one overlay, one input. The mode comes from the first character:
//   ':' commands · '/' or plain text search issues · 'g' jump to a key · '#' JQL.
// Backspace on an empty input returns to search. Also exports recordRecent().
import { h, clear, debounce } from '../lib/dom.js';
import { css } from '../lib/css.js';
import { fuzzy } from '../lib/fuzzy.js';
import { kbd } from '../lib/keys.js';
import bus from '../lib/bus.js';
import api from '../lib/api.js';

css('palette');

const site = () => (window.laneway && window.laneway.session && window.laneway.session.site) || '';
const readLS = (k, d) => { try { return JSON.parse(localStorage.getItem('lw:' + k + ':' + site())) || d; } catch (e) { return d; } };
const writeLS = (k, v) => { try { localStorage.setItem('lw:' + k + ':' + site(), JSON.stringify(v)); } catch (e) { /* quota */ } };

// ---- recent issues: any card-like object {Key, Summary, Status, Done, InProgress, Assignee, AvatarURL}
const RECENT_MAX = 30;
export function recordRecent(card) {
  if (!card || !card.Key) return;
  const list = readLS('recent', []);
  const i = list.findIndex(c => c.Key === card.Key);
  const prev = i >= 0 ? list.splice(i, 1)[0] : {};
  list.unshift({ ...prev, ...Object.fromEntries(Object.entries(card).filter(([, v]) => v !== undefined && v !== '')) });
  writeLS('recent', list.slice(0, RECENT_MAX));
}
const fromIssue = i => ({ Key: i.Key, Summary: i.Summary, Status: i.Status, Done: i.StatusCategory === 'done', InProgress: i.StatusCategory === 'indeterminate', Assignee: i.Assignee });
bus.on('panel', ({ key }) => {
  if (!key) return;
  recordRecent({ Key: key });
  api.get('/issues/' + key).then(i => recordRecent(fromIssue(i)), () => {});
});

const KEY_RE = /^[A-Za-z][A-Za-z0-9]+-\d+$/;
const cat = c => (c.Done ? 'done' : c.InProgress ? 'indeterminate' : 'new');

// ---- JQL completion (the TUI's rules, internal/ui/jql.go)
const OPS = ['=', '!=', '~', '!~', '>', '>=', '<', '<=', 'in', 'is', 'was', 'changed'];
function jqlContext(s) {
  let start = Math.max(s.lastIndexOf(' '), s.lastIndexOf('('), s.lastIndexOf(',')) + 1;
  if (((s.match(/"/g) || []).length) % 2 === 1) start = s.lastIndexOf('"');
  const prefix = s.slice(start).replace(/^"+|"+$/g, '');
  let head = s.slice(0, start);
  const open = head.lastIndexOf('(');
  if (open >= 0 && !head.slice(open).includes(')')) head = head.slice(0, open);
  const w = head.replace(/,/g, ' ').trim().split(/\s+/).filter(Boolean), n = w.length;
  const lw = i => w[i].toLowerCase(), isOp = i => OPS.includes(lw(i));
  if (n >= 3 && lw(n - 1) === 'in' && lw(n - 2) === 'not') return { field: w[n - 3], prefix, start, value: true };
  if (n >= 3 && lw(n - 1) === 'not' && isOp(n - 2)) return { field: w[n - 3], prefix, start, value: true };
  if (n >= 2 && isOp(n - 1)) return { field: w[n - 2], prefix, start, value: true };
  return { field: '', prefix, start, value: false };
}
function jqlComplete(s, word) {
  const { start } = jqlContext(s);
  if (word.includes(' ') && !word.startsWith('"')) word = '"' + word + '"';
  return s.slice(0, start) + word + ' ';
}
function jqlMatches(words, prefix) {
  const p = prefix.toLowerCase(), head = [], rest = [];
  for (const w of words) {
    const l = w.replace(/^"|"$/g, '').toLowerCase();
    if (l.startsWith(p)) head.push(w); else if (p && l.includes(p)) rest.push(w);
  }
  return head.concat(rest);
}

const MODES = {
  search: { label: 'search', hint: 'Search issues…' },
  cmd: { label: 'command', hint: 'Run a command…' },
  jump: { label: 'jump', hint: 'Issue key, e.g. ABC-123' },
  jql: { label: 'jql', hint: 'assignee = currentUser() AND resolution = Unresolved' },
};
const PREFIX = { ':': 'cmd', '/': 'search', '#': 'jql' };

function hl(text, idx) {
  if (!idx || !idx.length) return text;
  const out = []; let last = 0;
  for (const i of idx) { if (i > last) out.push(text.slice(last, i)); out.push(h('mark', text[i])); last = i + 1; }
  if (last < text.length) out.push(text.slice(last));
  return out;
}

let active = null;

export function openPalette(app, mode = '') {
  const m0 = mode === ':' ? 'cmd' : mode === 'g' ? 'jump' : mode === '#' ? 'jql' : 'search';
  if (active) return active.setMode(m0);

  let cur = m0, q = '', items = [], sel = 0, phase = 'edit', seq = 0, ctl = null, busy = false;
  let words = null, sugg = [], results = null, cmdCache = [];
  const badge = h('button.pal-mode', { type: 'button', title: 'Backspace on an empty input returns to search', onclick: () => setMode('search') });
  const input = h('input.pal-input', { type: 'text', spellcheck: false, autocomplete: 'off', autocapitalize: 'off', 'aria-label': 'Palette' });
  const list = h('div.pal-list', { role: 'listbox' });
  const foot = h('div.pal-foot');
  const spin = h('span.pal-spin', { hidden: true });
  const tabs = h('div.pal-tabs', [['search', '/'], ['cmd', ':'], ['jump', 'g'], ['jql', '#']].map(([mm, k]) =>
    h('button.pal-tab', { type: 'button', dataset: { m: mm }, onclick: () => setMode(mm) }, MODES[mm].label, h('kbd', k))));
  const box = h('div.pal', h('div.pal-head', badge, input, spin), tabs, list, foot);
  const modal = app.ui.modal(box, { className: 'palette-modal', onClose: () => { active = null; if (ctl) ctl.abort(); } });
  const close = () => modal.close();

  // ---- requests: only the newest counts, older ones are aborted
  function fetchJSON(path, done) {
    if (ctl) ctl.abort();
    ctl = new AbortController();
    const my = ++seq; busy = true; spin.hidden = false;
    api.get(path, { signal: ctl.signal }).then(d => { if (my === seq) { busy = false; spin.hidden = true; done(d, null); } },
      e => { if (my === seq && e.name !== 'AbortError') { busy = false; spin.hidden = true; done(null, e); } });
  }
  const stopFetch = () => { if (ctl) ctl.abort(); seq++; busy = false; spin.hidden = true; };

  // ---- rows
  const issueItem = c => ({ type: 'issue', key: c.Key, card: c });
  function build() {
    items = [];
    const v = q.trim();
    if (cur === 'cmd') buildCmds(v);
    else if (cur === 'jump') buildJump(v);
    else if (cur === 'jql') buildJQL(v);
    else buildSearch(v);
    if (!items.some(selectable)) items.push({ type: 'msg', text: cur === 'cmd' ? 'No commands match' : cur === 'jql' && phase === 'edit' ? 'Enter runs the query' : 'Nothing found' });
    sel = Math.max(0, items.findIndex(selectable));
    draw();
  }
  const selectable = it => it.type !== 'hdr' && it.type !== 'msg';

  function localMatches(v) {
    const out = [];
    for (const c of readLS('recent', [])) {
      const m = fuzzy(v, c.Key + ' ' + (c.Summary || ''));
      if (m) out.push([m.score, c]);
    }
    return out.sort((a, b) => b[0] - a[0]).map(x => x[1]);
  }

  let found = null, foundFor = '';
  function buildSearch(v) {
    if (!v) {
      const rec = readLS('recent', []).filter(c => c.Summary).slice(0, 12);
      if (rec.length) { items.push({ type: 'hdr', text: 'Recent' }); rec.forEach(c => items.push(issueItem(c))); }
      else items.push({ type: 'msg', text: 'Type to search issues. : commands · g jump · # JQL' });
      return;
    }
    const seen = new Set();
    if (KEY_RE.test(v)) { seen.add(v.toUpperCase()); items.push({ type: 'jump', key: v.toUpperCase() }); }
    const local = localMatches(v).filter(c => !seen.has(c.Key)).slice(0, 6);
    if (local.length) { items.push({ type: 'hdr', text: 'Recent' }); local.forEach(c => { seen.add(c.Key); items.push(issueItem(c)); }); }
    if (found && foundFor === v) {
      const rest = found.filter(c => !seen.has(c.Key));
      if (rest.length) { items.push({ type: 'hdr', text: 'Issues' }); rest.forEach(c => items.push(issueItem(c))); }
    }
  }
  const searchRemote = debounce(() => {
    const v = q.trim();
    if (cur !== 'search' || v.length < 2) { stopFetch(); return; }
    fetchJSON('/find?n=25&q=' + encodeURIComponent(v), (d, e) => {
      if (e) { items.push({ type: 'msg', text: e.message, err: true }); draw(); return; }
      found = d.cards || []; foundFor = v; build();
    });
  }, 140);

  function buildCmds(v) {
    const all = cmdCache;
    if (!v) {
      const recent = readLS('cmdrecent', []).map(id => all.find(c => c.id === id)).filter(Boolean).slice(0, 5);
      if (recent.length) { items.push({ type: 'hdr', text: 'Recent' }); recent.forEach(c => items.push({ type: 'cmd', cmd: c })); }
      const groups = new Map();
      for (const c of all) (groups.get(c.group || 'Other') || groups.set(c.group || 'Other', []).get(c.group || 'Other')).push(c);
      for (const [g, cs] of [...groups].sort((x, y) => (y[0] === 'Pinned') - (x[0] === 'Pinned'))) { items.push({ type: 'hdr', text: g }); cs.forEach(c => items.push({ type: 'cmd', cmd: c })); }
      return;
    }
    const rows = [];
    for (const c of all) {
      const m = fuzzy(v, c.title), g = m ? null : fuzzy(v, (c.group || '') + ' ' + c.title);
      if (m || g) rows.push({ c, s: m ? m.score + 10 : g.score, idx: m ? m.idx : null });
    }
    rows.sort((a, b) => b.s - a.s).forEach(r => items.push({ type: 'cmd', cmd: r.c, idx: r.idx }));
  }

  function defaultProject() {
    const r = app.route && app.route.params && app.route.params.project;
    return r || (app.session && app.session.projects && app.session.projects[0]) || '';
  }
  function jumpKey(v) {
    if (/^\d+$/.test(v) && defaultProject()) return defaultProject().toUpperCase() + '-' + v;
    return KEY_RE.test(v) ? v.toUpperCase() : '';
  }
  let peek = null;
  function buildJump(v) {
    const key = jumpKey(v);
    const seen = new Set();
    if (key) { seen.add(key); items.push({ type: 'jump', key, card: peek && peek.Key === key ? peek : null, missing: peek && peek.missing === key }); }
    const local = (v ? localMatches(v) : readLS('recent', []).filter(c => c.Summary)).filter(c => !seen.has(c.Key)).slice(0, 10);
    if (local.length) { items.push({ type: 'hdr', text: 'Recent' }); local.forEach(c => items.push(issueItem(c))); }
    else if (!key) items.push({ type: 'msg', text: 'Type an issue key, or a number for ' + (defaultProject() || 'your project') });
  }
  const jumpRemote = debounce(() => {
    const key = jumpKey(q.trim());
    if (cur !== 'jump' || !key) { peek = null; stopFetch(); return; }
    if (peek && peek.Key === key) return;
    fetchJSON('/issues/' + key, (d, e) => { peek = e ? { missing: key } : fromIssue(d); build(); });
  }, 120);

  function buildJQL(v) {
    if (phase === 'results') {
      if (results.err) { items.push({ type: 'msg', text: results.err, err: true }); return; }
      items.push({ type: 'hdr', text: results.cards.length + ' issue' + (results.cards.length === 1 ? '' : 's') + ' · ctrl+s saves as a filter' });
      results.cards.forEach(c => items.push(issueItem(c)));
      return;
    }
    if (!v) {
      const hist = readLS('jqlhist', []);
      const saved = app.jqlFilters || [];
      if (saved.length) { items.push({ type: 'hdr', text: 'Saved filters' }); saved.forEach(f => items.push({ type: 'fill', text: f.JQL, label: f.Name })); }
      if (hist.length) { items.push({ type: 'hdr', text: 'Recent searches' }); hist.forEach(t => items.push({ type: 'fill', text: t })); }
      return;
    }
    sugg.forEach(s => items.push({ type: 'sugg', text: s.text, kind: s.kind }));
  }

  function wordsLoaded() {
    if (words) return;
    words = { Fields: [], Functions: [], Reserved: [] };
    api.swr('/jql/words', d => { words = d; if (cur === 'jql') suggest(); }).catch(() => {});
    api.swr('/filters', d => { app.jqlFilters = d; if (cur === 'jql' && !q) build(); }).catch(() => {});
  }
  function suggest() {
    const v = q;
    if (!v.trim()) { sugg = []; build(); return; }
    const c = jqlContext(v);
    if (!c.value) {
      stopFetch();
      const tag = (arr, kind) => arr.map(text => ({ text, kind }));
      const all = [...tag(words.Fields, 'field'), ...tag(words.Functions, 'function'), ...tag(words.Reserved, 'keyword')];
      const ok = new Set(jqlMatches(all.map(a => a.text), c.prefix));
      sugg = all.filter(a => ok.has(a.text)).sort((a, b) => [...ok].indexOf(a.text) - [...ok].indexOf(b.text)).slice(0, 40);
      build(); return;
    }
    const fns = jqlMatches(words.Functions, c.prefix).map(text => ({ text, kind: 'function' }));
    sugg = fns; build();
    valuesRemote(c);
  }
  const valuesRemote = debounce(c => {
    if (cur !== 'jql' || phase !== 'edit') return;
    fetchJSON('/jql/values?field=' + encodeURIComponent(c.field) + '&prefix=' + encodeURIComponent(c.prefix), (d) => {
      if (phase !== 'edit' || !d) return;
      const fns = jqlMatches(words.Functions, c.prefix).map(text => ({ text, kind: 'function' }));
      sugg = d.map(text => ({ text, kind: 'value' })).concat(fns).slice(0, 40); build();
    });
  }, 150);

  function runJQL(text) {
    const v = text.trim(); if (!v) return;
    results = null; phase = 'results'; sugg = [];
    const hist = readLS('jqlhist', []).filter(t => t !== v); hist.unshift(v); writeLS('jqlhist', hist.slice(0, 20));
    items = [{ type: 'msg', text: 'Searching…' }]; sel = 0; draw();
    fetchJSON('/search?jql=' + encodeURIComponent(v), (d, e) => {
      results = e ? { err: e.message } : { cards: d.cards || [] };
      build();
    });
  }
  async function saveFilter() {
    const v = q.trim(); if (cur !== 'jql' || !v) return;
    const name = await app.ui.prompt({ title: 'Save filter as', placeholder: 'Name', ok: 'Save' });
    if (!name || !name.trim()) return;
    try { await api.post('/filters', { Name: name.trim(), JQL: v }); app.ui.toast('Saved filter “' + name.trim() + '”', { kind: 'ok' }); api.get('/filters', { fresh: true }).then(d => { app.jqlFilters = d; }, () => {}); }
    catch (e) { app.ui.errToast(e); }
    input.focus();
  }

  // ---- drawing
  function draw() {
    const frag = document.createDocumentFragment();
    items.forEach((it, i) => frag.append(row(it, i)));
    clear(list).append(frag);
    scrollSel();
    const hints = {
      search: '⏎ open · ⌃⏎ full page · ↑↓ move',
      cmd: '⏎ run · ↑↓ move',
      jump: '⏎ open · ⌃⏎ full page',
      jql: phase === 'results' ? '⏎ open · ⌃⏎ full page · ⌃s save filter · type to edit' : 'tab completes · ⏎ runs · ⌃s save filter',
    };
    foot.textContent = hints[cur] + ' · esc closes';
  }
  function row(it, i) {
    if (it.type === 'hdr') return h('div.pal-hdr', it.text);
    if (it.type === 'msg') return h('div.pal-msg' + (it.err ? '.err' : ''), it.text);
    const on = i === sel ? '.sel' : '';
    const ev = { role: 'option', dataset: { i }, onmousemove: () => { if (sel !== i) { sel = i; mark(); } }, onclick: e => activate(i, e.ctrlKey || e.metaKey) };
    if (it.type === 'cmd') {
      const c = it.cmd;
      return h('div.pal-row' + on, ev, h('span.pal-title', hl(c.title, it.idx)), c.group && h('span.chip', c.group),
        c.keys && h('span.pal-keys', [].concat(c.keys).map(k => kbd(k).map(x => h('kbd', x)))));
    }
    if (it.type === 'sugg') return h('div.pal-row' + on, ev, h('span.pal-title.mono', it.text), h('span.chip', it.kind));
    if (it.type === 'fill') return h('div.pal-row' + on, ev, h('span.pal-title.mono', it.label ? it.label : it.text), it.label && h('span.pal-sub.mono', it.text));
    const c = it.card || {};
    const key = it.key || c.Key;
    return h('div.pal-row.issue' + on, ev,
      h('span.pal-key.mono', key),
      c.Status ? app.ui.statusPill(c.Status, cat(c)) : it.type === 'jump' && !it.card ? h('span.faint', it.missing ? 'not found' : 'open') : null,
      h('span.pal-title', c.Summary),
      c.Assignee ? app.ui.avatar(c.Assignee, c.AvatarURL, 18) : null);
  }
  const mark = () => { [...list.children].forEach((r, i) => r.classList.toggle('sel', i === sel)); scrollSel(); };
  const scrollSel = () => { const r = list.children[sel]; if (r && r.scrollIntoView) r.scrollIntoView({ block: 'nearest' }); };
  function move(d) {
    const idx = items.map((it, i) => selectable(it) ? i : -1).filter(i => i >= 0);
    if (!idx.length) return;
    const p = idx.indexOf(sel);
    sel = idx[(p + d + idx.length) % idx.length]; mark();
  }

  // ---- actions
  function activate(i = sel, full = false) {
    const it = items[i]; if (!it || !selectable(it)) return;
    if (it.type === 'cmd') {
      const ids = readLS('cmdrecent', []).filter(id => id !== it.cmd.id); ids.unshift(it.cmd.id); writeLS('cmdrecent', ids.slice(0, 8));
      close(); setTimeout(() => it.cmd.run(), 0); return;
    }
    if (it.type === 'fill') { setInput(it.text); runJQL(it.text); return; }
    if (it.type === 'sugg') { accept(); return; }
    const key = it.key || (it.card && it.card.Key);
    if (!key) return;
    if (it.card) recordRecent(it.card);
    close();
    import('./issue.js').then(m => m.follow(key)).catch(() => {}).then(() => (full ? app.go('/issue/' + key) : app.panel.open(key)));
  }
  function accept() {
    const it = items[sel];
    if (cur !== 'jql' || phase !== 'edit') return false;
    if (it && it.type === 'sugg') { setInput(jqlComplete(q, it.text)); suggest(); return true; }
    if (it && it.type === 'fill') { setInput(it.text); suggest(); return true; }
    return false;
  }
  function setInput(v) { input.value = v; q = v; }
  function setMode(mm) {
    cur = mm; phase = 'edit'; stopFetch(); setInput(''); found = null; peek = null; sugg = []; results = null;
    if (mm === 'cmd') cmdCache = app.commands.list();
    if (mm === 'jql') wordsLoaded();
    badge.textContent = MODES[mm].label; badge.dataset.m = mm;
    input.placeholder = MODES[mm].hint;
    for (const t of tabs.children) t.classList.toggle('on', t.dataset.m === mm);
    build(); input.focus();
  }

  input.addEventListener('input', () => {
    const v = input.value;
    if (!q && v.length === 1 && PREFIX[v] && !(cur === 'jql' && v === '#')) { setMode(PREFIX[v]); return; }
    if (!q && v === 'g ' && cur === 'search') { setMode('jump'); return; }
    q = v;
    if (cur === 'jql') { phase = 'edit'; results = null; stopFetch(); suggest(); return; }
    build();
    if (cur === 'search') searchRemote(); else if (cur === 'jump') jumpRemote();
  });
  input.addEventListener('keydown', e => {
    if (e.key === 'Backspace' && !input.value && cur !== 'search') { e.preventDefault(); setMode('search'); }
  });
  const k = modal.scope;
  k.bind(['ArrowDown', 'ctrl+n'], () => move(1), '', { input: true, hidden: true });
  k.bind(['ArrowUp', 'ctrl+p'], () => move(-1), '', { input: true, hidden: true });
  k.bind('Tab', () => { if (!accept()) move(1); }, '', { input: true, hidden: true });
  k.bind('shift+Tab', () => move(-1), '', { input: true, hidden: true });
  k.bind('Enter', () => {
    if (cur === 'jql' && phase === 'edit') {
      const it = items[sel];
      if (!q.trim() && it && it.type === 'fill') return activate();
      return runJQL(q);
    }
    activate();
  }, '', { input: true, hidden: true });
  k.bind('ctrl+Enter', () => activate(sel, true), '', { input: true, hidden: true });
  k.bind('ctrl+s', saveFilter, '', { input: true, hidden: true });

  active = { setMode };
  setMode(m0);
}
