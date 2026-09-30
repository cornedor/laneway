// Agents: every herdr agent on an issue, grouped by state (waiting on you first), with its recent
// terminal output beside it, read-only. Tab adds the worktrees that have no agent. Live over /api/agents/events.
import { h, clear, delegate } from '../lib/dom.js';
import { css } from '../lib/css.js';
import { GLYPH, LABEL, RANK } from '../lib/agents.js';

const GROUPS = ['blocked', 'working', 'done', 'idle', 'worktree'];
const GROUP_NAME = { blocked: 'Waiting on you', working: 'Working', done: 'Done', idle: 'Idle', unknown: 'Unknown', worktree: 'Worktrees without an agent' };
const home = p => { const m = p && p.match(/^\/(?:home|Users)\/[^/]+/); return m ? '~' + p.slice(m[0].length) : p || ''; };

export default async function mount(el, { app, scope, toolbar }) {
  css('agents');
  for (let i = 0; !app.agents && i < 60; i++) await new Promise(r => setTimeout(r, 50));
  const { api, ui, bus } = app;
  let rows = [], sel = 0, bare = app.prefs.get('agents_bare', 'false') === 'true', stopAsk = '';
  const cards = new Map(); // key → card, looked up once
  const asked = new Set();
  let dead = false, termSeq = 0, termPane = '', termTimer = 0;

  const list = h('div.ag-scroll'), head = h('div.ag-head'), detail = h('div.ag-detail');
  const empty = h('div.empty', { hidden: true });
  el.append(h('div.agents', h('div.ag-list', head, list, empty), detail));

  const snap = () => app.agents.snapshot;
  const cur = () => rows[sel] || null;

  function build() {
    const keep = cur() && (cur().pane || cur().key);
    const s = snap();
    rows = s.Agents.filter(a => a.Key).map(a => ({ key: a.Key, pane: a.PaneID, agent: a, group: GROUPS.includes(a.Status) ? a.Status : 'idle', path: a.CWD }));
    const have = new Set(rows.map(r => r.key));
    if (bare) for (const [k, p] of Object.entries(s.Worktrees || {})) if (!have.has(k)) rows.push({ key: k, pane: '', agent: null, group: 'worktree', path: p });
    rows.sort((a, b) => GROUPS.indexOf(a.group) - GROUPS.indexOf(b.group) || a.key.localeCompare(b.key, undefined, { numeric: true }) || (a.agent ? a.agent.Name : '').localeCompare(b.agent ? b.agent.Name : ''));
    const i = rows.findIndex(r => (r.pane || r.key) === keep);
    sel = i >= 0 ? i : Math.min(sel, Math.max(rows.length - 1, 0));
    lookUp();
    paintList();
    paintDetail();
  }

  async function lookUp() {
    const miss = [...new Set(rows.map(r => r.key))].filter(k => !cards.has(k) && !asked.has(k));
    if (!miss.length) return;
    miss.forEach(k => asked.add(k));
    try {
      const r = await api.get('/search?jql=' + encodeURIComponent('key in (' + miss.join(', ') + ')'));
      for (const c of r.cards || []) cards.set(c.Key, c);
      if (!dead) paintList();
    } catch (e) { /* summaries are optional; one unknown key fails the whole search */ }
  }

  function paintList() {
    const s = snap();
    const n = s.Agents.filter(a => a.Key).length, blocked = s.Agents.filter(a => a.Status === 'blocked').length;
    clear(head).append(h('span', n + (n === 1 ? ' agent' : ' agents')), blocked ? h('span.chip', { style: { color: 'var(--warn)' } }, blocked + ' waiting on you') : '',
      h('span.spacer'), h('button.btn.ghost.sm', { title: 'Tab', onclick: toggleBare }, bare ? 'Hide worktrees' : 'Show worktrees'));
    const kids = [];
    let g = '';
    rows.forEach((r, i) => {
      if (r.group !== g) { g = r.group; kids.push(h('div.ag-group.st-' + g, GROUP_NAME[g] || g)); }
      const c = cards.get(r.key);
      kids.push(h('div.ag-row.st-' + r.group + (i === sel ? '.sel' : ''), { dataset: { key: r.key, i } },
        h('span.g', GLYPH[r.group] || '?'), h('span.mono.ag-key', r.key), h('span.sum', c ? c.Summary : ''), h('span.name', r.agent ? r.agent.Agent : ''),
        h('span.sub', (r.agent && r.agent.Title ? r.agent.Title + ' · ' : '') + home(r.path))));
    });
    list.replaceChildren(...kids);
    empty.hidden = rows.length > 0;
    empty.textContent = !s.Available ? 'herdr is not running.' : bare ? 'No agents or worktrees for an issue.' : 'No agent works on an issue. S starts one, Tab lists worktrees.';
    const r = list.querySelector('.ag-row.sel'); if (r) r.scrollIntoView({ block: 'nearest' });
  }

  function mark() {
    list.querySelectorAll('.ag-row').forEach(r => r.classList.toggle('sel', +r.dataset.i === sel));
    const r = list.querySelector('.ag-row.sel'); if (r) r.scrollIntoView({ block: 'nearest' });
    stopAsk = '';
    paintDetail();
  }

  // ---- detail and terminal
  const pre = h('pre.ag-term.mono', { tabindex: 0 });
  let shownPane = '';
  function paintDetail() {
    const r = cur();
    if (!r) { clear(detail); shownPane = ''; stopTerm(); return; }
    const c = cards.get(r.key), a = r.agent;
    const btn = (label, key, fn, cls = '') => h('button.btn' + cls, { onclick: fn }, label, h('kbd', key));
    const sig = (r.pane || r.key) + '|' + (a ? a.Status + a.Title : '') + '|' + (c ? c.Summary : '');
    if (detail._sig !== sig) {
      detail._sig = sig;
      clear(detail).append(
        h('h2', h('a.issue-ref', { href: '#/issue/' + r.key, onclick: e => { e.preventDefault(); app.panel.open(r.key); } }, r.key), c ? ' ' + c.Summary : ''),
        h('div.ag-meta', h('span.chip', (GLYPH[r.group] || '') + ' ' + (LABEL[r.group] || r.group)), a && h('span', a.Agent + ' · ' + a.Name), h('span.mono', home(r.path)), a && a.Title && h('span', a.Title)),
        h('div.ag-actions',
          a && btn('Focus in herdr', 'f', () => act('focus')), a && btn('Prompt', 'p', () => act('prompt')), a && btn('New agent here', 'N', () => act('new')),
          btn(a ? 'Another agent' : 'Start agent', 'S', () => app.agents.startWork(r.key, { force: true })),
          a && btn('Stop', 'd', () => act('stop'), '.danger')),
        a ? pre : h('div.dim', 'A worktree without an agent. S starts one in it.'));
    }
    if (a) startTerm(a.PaneID); else stopTerm();
  }

  function stopTerm() { clearTimeout(termTimer); termTimer = 0; termPane = ''; termSeq++; }
  function startTerm(pane) {
    if (termPane !== pane) { termPane = pane; pre.textContent = ''; pre._text = null; pre.dataset.pane = pane; }
    clearTimeout(termTimer);
    readTerm();
  }
  async function readTerm() {
    const pane = termPane; if (!pane || dead) return;
    const seq = ++termSeq;
    if (!document.hidden || pre._text == null) {
      try {
        const r = await api.get('/agents/' + encodeURIComponent(pane) + '/output?lines=150', { fresh: true });
        if (seq === termSeq && pane === termPane && r.Text !== pre._text) {
          const atEnd = pre.scrollHeight - pre.scrollTop - pre.clientHeight < 40 || !pre._text;
          pre._text = r.Text; pre.textContent = r.Text;
          if (atEnd) pre.scrollTop = pre.scrollHeight;
        }
      } catch (e) { if (seq === termSeq) { pre._text = ''; pre.textContent = e.message; } }
    }
    if (seq === termSeq) termTimer = setTimeout(readTerm, 2000);
  }

  // ---- actions
  async function act(what) {
    const r = cur(); if (!r || !r.agent) return r && ui.toast(r.key + ' has no agent here, only its worktree');
    const a = r.agent;
    try {
      if (what === 'focus') { await api.post('/agents/' + a.PaneID + '/focus'); ui.toast('Focused ' + a.Name + ' in herdr'); }
      else if (what === 'prompt') {
        const text = await ui.prompt({ title: 'Prompt for ' + a.Name, multiline: true, placeholder: 'What should it do next?', ok: 'Send' });
        if (text && text.trim()) { await api.post('/agents/' + a.PaneID + '/prompt', { Text: text }); ui.toast('Prompt sent'); }
      } else if (what === 'new') {
        const text = await ui.prompt({ title: 'New agent in ' + home(a.CWD), multiline: true, value: '', placeholder: 'Prompt (empty starts it without one)', ok: 'Start' });
        if (text != null) { await api.post('/agents/' + a.PaneID + '/new', { Prompt: text.trim() }); ui.toast('Agent started'); }
      } else if (what === 'stop') {
        if (stopAsk !== a.PaneID) { stopAsk = a.PaneID; ui.toast('Stop ' + a.Name + ' and close its tab? d again'); return; }
        stopAsk = '';
        await api.post('/agents/' + a.PaneID + '/stop'); ui.toast('Stopped ' + a.Name);
      }
    } catch (e) { ui.errToast(e); }
    app.agents.refresh();
  }

  function toggleBare() { bare = !bare; app.prefs.set('agents_bare', bare); build(); }
  const move = d => { if (!rows.length) return; sel = Math.max(0, Math.min(rows.length - 1, sel + d)); mark(); };

  const G = 'Agents';
  scope.bind(['j', 'ArrowDown'], () => move(1), 'next', { group: G });
  scope.bind(['k', 'ArrowUp'], () => move(-1), 'previous', { group: G });
  scope.bind('Home', () => move(-rows.length), 'first', { group: G, hidden: true });
  scope.bind('End', () => move(rows.length), 'last', { group: G, hidden: true });
  scope.bind('Tab', toggleBare, 'show / hide worktrees without an agent', { group: G, when: () => !app.panel.key });
  scope.bind('Enter', () => { const r = cur(); if (r) app.panel.open(r.key); }, 'open issue', { group: G });
  scope.bind('o', () => { const r = cur(); if (r) window.open(app.session.baseURL + '/browse/' + r.key, '_blank', 'noopener'); }, 'open in Jira', { group: G });
  scope.bind('f', () => act('focus'), 'focus the agent in herdr', { group: G });
  scope.bind('p', () => act('prompt'), 'send the agent a prompt', { group: G });
  scope.bind('N', () => act('new'), 'start another agent in its directory', { group: G });
  scope.bind('d', () => act('stop'), 'stop the agent (twice)', { group: G });
  scope.bind('y', () => { const r = cur(); if (r && navigator.clipboard) navigator.clipboard.writeText(r.key).then(() => ui.toast('Copied ' + r.key)); }, 'copy key', { group: G });
  scope.bind('r', () => { app.agents.refresh(); ui.toast('Refreshed'); }, 'refresh', { group: G });

  delegate(list, 'click', '.ag-row', (e, t) => { sel = +t.dataset.i; mark(); });
  delegate(list, 'dblclick', '.ag-row', (e, t) => app.panel.open(t.dataset.key));

  const off = bus.on('agents', () => { if (!dead) build(); });
  const offDone = bus.on('issue:changed', () => { asked.clear(); });
  build();
  return () => { dead = true; off(); offDone(); stopTerm(); };
}
