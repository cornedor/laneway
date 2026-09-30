// The issue's agent terminal in the panel and on the issue page, as the TUI's agent panel (ui.agent_view: panel):
// a Terminal tab while a herdr agent works on the issue. lib/term.js is made on first show and attached only while
// the tab shows, after a short rest (tabbing through reconnects nothing); hidden or unmounted it detaches, the agent
// keeps running. One browser terminal per pane: attaching here takes it over from the agents screen and back.
//
//   const t = mountTerm(key, {app, back, repaint});   t.el · t.has() · t.show() · t.hide() · t.type({takeover})
//   t.next() (several agents: the next one) · t.scroll(lines) · t.status() · t.dispose()
import { h } from '../lib/dom.js';
import { kbd } from '../lib/keys.js';
import { GLYPH, LABEL } from '../lib/agents.js';
import { terminal, LEAVE } from '../lib/term.js';

const LIVE = ['open', 'connecting', 'retry']; // terminal states that keep keys in it
const REST = 150; // ms the tab shows before the terminal attaches

export function mountTerm(key, { app, back, repaint }) {
  const host = h('div.ag-term'), hint = h('div.ag-hint', { 'aria-live': 'polite' });
  const meta = h('div.it-meta'), box = h('div.ag-termbox', host, hint), none = h('div.it-none', { hidden: true });
  const el = h('section.pane.terminal', { tabindex: -1, hidden: true }, meta, box, none);
  let tc = null, making = null, dead = false, on = false, pane = '', timer = 0, seen = '';
  let ts = { state: 'closed', text: '', typing: false };

  const demo = () => !!(app.session && app.session.demo);
  const agents = () => (app.agents && app.agents.available && !demo() && (app.agents.stateFor(key) || {}).agents) || [];
  const agent = () => agents().find(a => a.PaneID === pane) || null;
  // Keep the chosen pane while it lives, else the issue's first agent (waiting on you first).
  const choose = () => { const as = agents(); if (!as.some(a => a.PaneID === pane)) pane = as.length ? as[0].PaneID : ''; return pane; };
  const sig = () => { const a = agent(); return a ? a.PaneID + '|' + a.Status + '|' + a.Agent : ''; };

  async function ensure() {
    if (tc || dead) return tc;
    making = making || terminal(host, { app, onState, onLeave: () => back() }).then(t => { if (dead) { t.dispose(); return null; } tc = t; return t; });
    try { return await making; } catch (e) { making = null; ts = { state: 'closed', text: 'the terminal did not load: ' + e.message }; paint(); return null; }
  }
  function onState(s) {
    ts = s;
    if (s.typing && tc && !LIVE.includes(s.state)) { tc.blur(); el.focus({ preventScroll: true }); }
    paint();
  }
  async function attach(o = {}) {
    clearTimeout(timer);
    if (!on || !choose()) { if (tc) tc.detach(); paint(); return null; }
    const t = await ensure();
    if (!t || dead || !on || !pane) return null;
    seen = sig();
    t.attach(pane, o);
    return t;
  }

  const leaveKey = () => { const b = app.keys.registry().find(x => x.id === 'terminal:' + LEAVE); return kbd((b && b.specs[0]) || LEAVE).join(' '); };
  const K = s => h('kbd', s);
  function paint() {
    if (dead) return;
    const as = agents(), a = agent();
    none.hidden = !!a; box.hidden = !a;
    if (!a) {
      const start = app.agents && app.agents.available && !demo();
      none.replaceChildren(h('p.dim', (demo() ? 'Agents are not available in demo.' : !app.agents || !app.agents.available ? 'herdr is not running.' : 'No agent works on ' + key + ' now.')),
        start ? h('button.btn', { onclick: () => app.agents.start(key) }, 'Start work', h('kbd', 'S')) : '');
      meta.replaceChildren();
      return;
    }
    // Which agent: a line for one, buttons for several (4 again steps through them).
    meta.replaceChildren(...as.map(x => h('button.it-agent.st-' + x.Status + (x.PaneID === pane ? '.on' : ''), { title: x.Name + (x.Title ? ' · ' + x.Title : ''), onclick: () => pick(x.PaneID), disabled: as.length === 1 },
      h('span.g', GLYPH[x.Status] || '?'), h('span', x.Agent + ' · ' + (LABEL[x.Status] || x.Status)), x.Title ? h('span.dim.clip', x.Title) : '')),
      as.length > 1 ? h('span.dim.it-more', K('4'), ' next agent') : '');
    const s = ts, parts = [];
    const add = (...xs) => { if (parts.length) parts.push(h('span.dim', ' · ')); parts.push(...xs); };
    const dot = h('span.ag-dot.st-' + s.state);
    if (s.state === 'open') {
      add(dot, s.typing ? h('b', 'typing') : 'attached');
      if (s.typing) { add(K(leaveKey()), ' back to the issue'); add(K('⌃⇧C'), ' copy'); add('shift+drag selects'); }
      else { add(K('⏎'), ' or ', K(leaveKey()), ' or click to type'); add(K('t'), ' take over input'); add(K('1'), ' details'); }
    } else if (LIVE.includes(s.state)) add(dot, s.text || 'connecting…');
    else {
      add(dot, s.text || (s.state === 'closed' ? 'detached' : s.state));
      if (s.state !== 'gone') add(K('⏎'), s.state === 'taken' ? ' take it back' : ' attach');
      if (s.state === 'exited' || s.state === 'closed') add(K('t'), ' take over input');
    }
    hint.replaceChildren(...parts);
    el.closest('.iss')?.classList.toggle('term-typing', !!s.typing);
  }
  hint.addEventListener('click', e => { if (!e.target.closest('kbd, button')) type(); });

  function pick(p) {
    if (p === pane) return;
    pane = p;
    if (tc) tc.detach();
    attach();
    paint();
  }
  // Type into it: attach (again) where needed, then focus. A click in the terminal focuses it as well.
  async function type(o = {}) {
    if (!on) return;
    clearTimeout(timer);
    if (!choose()) return app.ui.toast(key + ' has no agent: S starts one');
    const t = await ensure();
    if (!t || dead || !on) return;
    if (!LIVE.includes(t.state) || t.pane !== pane || o.takeover) { seen = sig(); t.attach(pane, { again: true, takeover: o.takeover }); }
    t.focus();
  }

  // The snapshot changed: follow the agent (a restart is a new pane, or the same pane alive again).
  const offAgents = app.bus.on('agents', () => {
    if (dead) return;
    const was = pane;
    choose();
    repaint();
    if (!on) return paint();
    if (!pane) { if (tc) tc.detach(); return paint(); }
    if (pane !== was || (tc && !LIVE.includes(tc.state) && tc.state !== 'taken' && sig() !== seen)) attach();
    else paint();
  });

  return {
    el,
    has: () => agents().length > 0,
    status: () => { const a = agents()[0]; return a ? a.Status : ''; },
    get typing() { return !!ts.typing; },
    show() {
      if (on) return;
      on = true; el.hidden = false;
      choose(); paint();
      clearTimeout(timer);
      if (pane) timer = setTimeout(() => attach(), REST);
    },
    hide() {
      clearTimeout(timer);
      if (!on) return;
      on = false; el.hidden = true;
      if (tc) { tc.blur(); tc.detach(); }
    },
    type,
    next() { const as = agents(); if (as.length > 1) pick(as[(as.findIndex(a => a.PaneID === pane) + 1) % as.length].PaneID); },
    scroll(n) { if (tc) tc.term.scrollLines(n); },
    dispose() { dead = true; on = false; clearTimeout(timer); offAgents(); if (tc) tc.dispose(); }, // one still loading disposes itself
  };
}
