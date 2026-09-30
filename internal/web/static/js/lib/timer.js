// The work timer: T starts it on the selected issue, stops it into the log-work dialog, or
// switches it. It lives in localStorage (survives reloads, shared by tabs) and shows in the header.
import { h } from './dom.js';
import { duration } from './fmt.js';
import { logDialog, parseDuration, hm } from './worktime.js';
import { installBadge } from './inbox.js';

const LS = 'lw:timer';
const MIN5 = 5 * 60000;

function load() {
  try { const t = JSON.parse(localStorage.getItem(LS)); return t && t.key && t.start ? t : null; } catch (e) { return null; }
}
function save(t) {
  try { t ? localStorage.setItem(LS, JSON.stringify(t)) : localStorage.removeItem(LS); } catch (e) { /* private mode */ }
}

// The issue `T` and `w` act on: the selected row of the view, else the open panel, else the route's issue.
export function target(app) {
  const row = document.querySelector('#view [data-key].sel, #view [data-key].selected, #view [data-key].focused, #view [data-key].cursor, #view [data-key] > .sel, #view [data-key].cur');
  const k = row && row.closest('[data-key]').dataset.key;
  if (k) return k;
  return app.panel.key || (app.route && ((app.route.params && app.route.params.key) || (app.route.query && app.route.query.issue))) || '';
}

export function install(app) {
  let timer = load();
  const chip = app.chrome.add(h('button.timer-chip.accent', { hidden: true, onclick: () => toggle('') }), 30);
  let tick = 0;

  const elapsed = () => Date.now() - timer.start;
  const since = () => (elapsed() < 60000 ? '<1m' : duration(Math.floor(elapsed() / 1000)));
  const label = () => [h('span.tk', timer.key), since()];
  function paint() {
    chip.hidden = !timer;
    if (timer) { chip.replaceChildren(...label()); chip.title = 'Timer on ' + timer.key + ' since ' + hm(new Date(timer.start)) + ' · T stops it'; }
    arm();
  }
  // Redraw while the tab is visible and a timer runs; nothing otherwise.
  function arm() {
    clearInterval(tick); tick = 0;
    if (timer && !document.hidden) tick = setInterval(() => { chip.replaceChildren(...label()); }, 20000);
  }
  document.addEventListener('visibilitychange', () => { if (!document.hidden && timer) chip.replaceChildren(...label()); arm(); });
  window.addEventListener('storage', e => { if (e.key === LS) { timer = load(); paint(); app.bus.emit('timer', timer); } });

  function set(t) { timer = t; save(t); paint(); app.bus.emit('timer', timer); }

  function round(ms) {
    const step = parseDuration(String(app.prefs.get('timer_round', (app.session && app.session.ui && app.session.ui.TimerRound) || '')) || '');
    const secs = ms / 1000;
    return step > 0 ? Math.max(Math.ceil(secs / step), 1) * step : Math.max(Math.round(secs / 60) * 60, 60);
  }

  function start(key) {
    set({ key, start: Date.now() });
    app.ui.toast('Timer started on ' + key);
  }

  // Stop into the log dialog. The timer keeps running until the work is in; `next` starts afterwards.
  async function stop(next) {
    if (!timer) return;
    const t = timer;
    const res = await logDialog(app, { key: t.key, seconds: round(Date.now() - t.start), started: new Date(t.start), discard: true, note: next && 'Then the timer starts on ' + next });
    if (res === 'discard') {
      if (Date.now() - t.start >= MIN5 && !(await app.ui.confirm({ title: 'Discard timer', text: duration(Math.floor((Date.now() - t.start) / 1000)) + ' on ' + t.key + ' will not be logged.', ok: 'Discard', danger: true }))) return;
    } else if (res !== 'logged') return;
    if (timer && timer.start === t.start) set(null);
    if (next) start(next);
  }

  function toggle(key) {
    if (!timer) return key ? start(key) : app.ui.toast('No timer running · T on an issue starts one');
    if (!key || key === timer.key) return stop();
    return stop(key);
  }

  const k = app.keys.scope('timer');
  k.bind('T', () => toggle(target(app)), 'start / stop timer', { group: 'Time', when: () => !!timer || !!target(app) });
  k.bind('w', () => { const key = target(app); key && logDialog(app, { key }); }, 'log work', { group: 'Time', when: () => !!target(app) });

  const cmd = (id, title, run, when) => app.commands.register({ id, group: 'Time', get title() { return title(); }, run, when });
  cmd('timer:toggle', () => !timer ? 'Start timer on ' + target(app) : target(app) && target(app) !== timer.key ? 'Switch timer to ' + target(app) : 'Stop timer and log ' + timer.key,
    () => toggle(target(app)), () => !!timer || !!target(app));
  cmd('timer:stop', () => 'Stop timer on ' + timer.key, () => toggle(''), () => !!timer && !!target(app) && target(app) !== timer.key);
  cmd('worklog:add', () => 'Log work on ' + target(app), () => logDialog(app, { key: target(app) }), () => !!target(app));

  app.timer = { get current() { return timer; }, toggle, start, stop, elapsed: () => (timer ? elapsed() : 0) };
  paint();
  installBadge(app);
}
