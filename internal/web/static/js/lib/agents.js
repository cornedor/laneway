// Coding agents (herdr): the live snapshot, agent state on cards and in the panel, and the keys
// that start work. app.agents.stateFor(key) → {status, count, agents, worktree} | null; bus 'agents' fires on change.
// Everything hides itself while herdr isn't running.
import { h } from './dom.js';
import { css } from './css.js';
import { target } from './timer.js';

export const GLYPH = { working: '⚙', blocked: '✋', done: '✓', idle: '○', unknown: '?', worktree: '◌' };
export const LABEL = { working: 'working', blocked: 'waiting on you', done: 'done', idle: 'idle', unknown: 'unknown', worktree: 'worktree, no agent' };
export const RANK = { blocked: 0, working: 1, done: 2, idle: 3, unknown: 4 };

export function install(app) {
  css('agents');
  const { api, bus, ui } = app;
  let snap = { Available: false, Agents: [], Worktrees: {} }, sig = '', byKey = new Map(), first = true;
  const tool = { GH: false, GLab: false };

  function index() {
    byKey = new Map();
    for (const a of snap.Agents) if (a.Key) (byKey.get(a.Key) || byKey.set(a.Key, []).get(a.Key)).push(a);
    for (const as of byKey.values()) as.sort((a, b) => (RANK[a.Status] ?? 9) - (RANK[b.Status] ?? 9));
  }
  function stateFor(key) {
    const as = byKey.get(key);
    if (as) return { status: as[0].Status, count: as.length, agents: as, worktree: snap.Worktrees[key] || '' };
    return snap.Worktrees && snap.Worktrees[key] ? { status: 'worktree', count: 0, agents: [], worktree: snap.Worktrees[key] } : null;
  }

  // One chip: glyph, a count beyond one; title says which agents.
  function paintChip(el, key) {
    const s = stateFor(key);
    el.hidden = !s;
    if (!s) return;
    el.className = 'agent-chip st-' + s.status;
    el.textContent = GLYPH[s.status] + (s.count > 1 ? s.count : '');
    el.title = (s.count ? s.agents.map(a => a.Name + ': ' + (LABEL[a.Status] || a.Status)).join('\n') : LABEL.worktree + ' ' + s.worktree) + '\nclick: show in Agents';
  }
  const chip = key => { const el = h('span.agent-chip', { dataset: { agentKey: key } }); paintChip(el, key); return el; };
  // A card's or row's key element carries the chip as its last child.
  function stamp(keyEl, key) {
    let el = keyEl.querySelector(':scope > .agent-chip');
    if (!stateFor(key)) { if (el) el.remove(); return; }
    if (!el) { el = h('span.agent-chip'); keyEl.append(el); }
    paintChip(el, key);
  }
  function paintAll() {
    for (const el of document.querySelectorAll('.agent-chip[data-agent-key]')) paintChip(el, el.dataset.agentKey);
    for (const row of document.querySelectorAll('#view [data-key]')) {
      const k = row.querySelector('.ckey, .l-key');
      if (k) stamp(k, row.dataset.key);
    }
    const nav = document.querySelector('#nav a[data-name=agents]');
    if (nav) nav.hidden = !snap.Available;
  }

  function set(next) {
    const s = JSON.stringify(next);
    if (s === sig) return;
    const was = new Map(snap.Agents.map(a => [a.PaneID, a.Status]));
    sig = s; snap = next; index(); paintAll();
    if (!first) for (const a of snap.Agents) {
      if (a.Status === 'blocked' && was.get(a.PaneID) !== 'blocked' && a.Key) {
        ui.toast((a.Key) + ': the agent waits on you', { action: { label: 'Open', run: () => app.go('/agents') }, ms: 8000 });
      }
    }
    first = false;
    bus.emit('agents', snap);
  }

  // Server-Sent Events, with a poll while the stream is down.
  let poll = 0;
  const fetchOnce = () => api.get('/agents', { fresh: true }).then(set).catch(() => set({ Available: false, Agents: [], Worktrees: {} }));
  const stopPoll = () => { clearInterval(poll); poll = 0; };
  function connect() {
    if (!window.EventSource) { poll = setInterval(fetchOnce, 5000); fetchOnce(); return; }
    const es = new EventSource('/api/agents/events');
    es.onmessage = e => { stopPoll(); try { set(JSON.parse(e.data)); } catch (err) { console.error(err); } };
    es.onerror = () => { if (!poll) { fetchOnce(); poll = setInterval(fetchOnce, 5000); } };
  }
  connect();

  api.get('/agents/status').then(s => {
    tool.GH = s.GH; tool.GLab = s.GLab;
    const nav = document.querySelector('#nav a[data-name=review]');
    if (nav) nav.hidden = !(s.GH || s.GLab);
  }).catch(() => {});

  // start(key, {another, path}): the one start work modal (views/agent_start.js openStartAgent).
  const start = (key, opts) => import('../views/agent_start.js').then(m => m.openStartAgent(app, key, opts)).catch(e => ui.errToast(e));
  app.agents = {
    get available() { return snap.Available; },
    get snapshot() { return snap; },
    get tools() { return tool; },
    stateFor, chip, stamp, refresh: fetchOnce, start, startWork: (key, o = {}) => start(key, { another: !!o.force }),
    worktree: key => snap.Worktrees[key] || '',
  };

  // ---- keys and commands
  const demo = () => !!(app.session && app.session.demo);
  const need = fn => () => { if (demo()) return ui.toast('Not available in demo'); const k = target(app); if (k) fn(k); else ui.toast('Select an issue first'); };
  const copyBranch = async key => {
    try {
      const { Name } = await api.get('/issues/' + key + '/branch');
      await navigator.clipboard.writeText(Name);
      ui.toast('Copied ' + Name);
    } catch (e) { ui.errToast(e); }
  };
  const draftPR = async key => {
    if (!await ui.confirm({ title: 'Draft pull request', text: 'Push the branch of ' + key + ' and open a draft pull request?', ok: 'Open draft' })) return;
    const close = ui.toast('Pushing ' + key + ' and opening a draft…', { ms: 120000 });
    try {
      const r = await api.post('/issues/' + key + '/pr');
      close();
      ui.toast('Draft opened: ' + r.URL, { action: { label: 'Open', run: () => window.open(r.URL, '_blank', 'noopener') }, ms: 10000 });
    } catch (e) { close(); ui.errToast(e); }
  };
  // A chip (card, row, panel) opens the agents screen on that issue's agent.
  document.addEventListener('click', e => {
    const c = e.target.closest && e.target.closest('.agent-chip');
    if (!c || c.hidden) return;
    const key = c.dataset.agentKey || (c.closest('[data-key]') || {}).dataset?.key;
    if (!key) return;
    e.preventDefault(); e.stopPropagation();
    app.go('/agents?agent=' + encodeURIComponent(key));
  }, true);
  const k = app.keys.scope('agents-global');
  k.bind('S', need(key => start(key)), 'start work: the form (agent, branch, prompt); focuses its agent if one runs', { group: 'Agents', when: () => !demo() });
  k.bind('alt+s', need(key => start(key, { another: true })), 'start another agent in the issue\'s worktree', { group: 'Agents', when: () => !demo() });
  k.bind('ctrl+y', need(copyBranch), 'copy branch name', { group: 'Agents', when: () => !demo() });
  const cmd = (id, title, run, when, keys) => app.commands.register({ id, group: 'Agents', get title() { return title(); }, keys, run, when });
  cmd('agents:start', () => { const s = stateFor(target(app)); return (s && s.count ? 'Focus the agent of ' : 'Start work on ') + target(app); }, need(key => start(key)), () => !demo() && !!target(app), 'S');
  cmd('agents:another', () => 'Start another agent on ' + target(app), need(key => start(key, { another: true })), () => !demo() && !!target(app) && snap.Available, 'alt+s');
  cmd('agents:show', () => 'Show the agent of ' + target(app) + ' in Agents', need(key => app.go('/agents?agent=' + encodeURIComponent(key))), () => !demo() && !!target(app) && !!stateFor(target(app)));
  cmd('agents:branch', () => 'Copy branch name of ' + target(app), need(copyBranch), () => !demo() && !!target(app), 'ctrl+y');
  cmd('agents:pr', () => 'Open draft pull request for ' + target(app), need(draftPR), () => !demo() && !!target(app));
  app.agents.draftPR = demo() ? () => ui.toast('Not available in demo') : draftPR;
}
