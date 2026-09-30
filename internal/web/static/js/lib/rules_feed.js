// What the server's rules engine does, live: one event stream for the whole app.
// notify actions become toasts, and browser notifications when the user opted in.
import { notify, enabled, supported, permission, setEnabled } from './notify.js';

const subs = new Set();
let es = null, last = 0, asked = false;

export const onRuleEvent = fn => { subs.add(fn); return () => subs.delete(fn); };

export async function install(app) {
  let info;
  try { info = await app.api.get('/rules'); } catch (e) { return; }
  last = info.Last || 0;
  if (!info.Rules || !info.Rules.length) return;
  connect(app);
}

function connect(app) {
  es = new EventSource('/api/rules/stream?since=' + last);
  es.onmessage = m => {
    let ev; try { ev = JSON.parse(m.data); } catch (e) { return; }
    last = Math.max(last, ev.ID);
    for (const fn of subs) fn(ev);
    if (ev.Err) return app.ui.toast('Rule ' + (ev.Rule || '') + ': ' + ev.Err, { kind: 'err' });
    if (ev.Action !== 'notify') return;
    const title = ev.Title || 'laneway';
    if (!document.hidden) app.ui.toast(title + ': ' + ev.Text, { ms: 6000 });
    if (!notify(title, ev.Text, () => ev.Key && app.panel.open(ev.Key), 'rule:' + ev.ID)) optIn(app);
  };
}

// Once per page: offer browser notifications for notify actions.
function optIn(app) {
  if (asked || enabled() || !supported() || permission() === 'denied') return;
  asked = true;
  app.ui.toast('Rules want to notify you. Allow browser notifications?', { ms: 12000, action: { label: 'Enable', run: () => setEnabled(true).then(on => app.ui.toast(on ? 'Notifications on' : 'Notifications not allowed')) } });
}
