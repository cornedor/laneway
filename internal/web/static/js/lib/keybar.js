// The key bar: one line at the bottom with the main keys of what has them now (the focused panel's, else the view's),
// then the global ones; a click presses one. Binds opt in with {bar: 'label'} (lib/keys.js), remapped keys show as bound.
// A plain toast shows in its place while it lasts, as the TUI's status line. Off with the `keybar` pref; hidden on phones,
// and hints that don't fit one line drop from the view's end.
import { h, $ } from './dom.js';
import { kbd } from './keys.js';
import { statusLine } from './ui.js';
import { T } from './i18n.js';

// The hints for keys.active() rows: [{label, keys: [{spec, run}], global}]. Binds sharing a label are one hint
// ("[ ] view"); the panel's or view's come before the global ones, a panel waiting for focus has none.
export function barHints(rows) {
  const by = new Map();
  for (const r of rows) {
    if (!r.bar || r.rank === 1) continue;
    const hint = by.get(r.bar) || by.set(r.bar, { label: r.bar, keys: [], global: r.rank === 2 }).get(r.bar);
    hint.keys.push({ spec: r.spec, run: r.run });
  }
  return [...by.values()].sort((a, b) => a.global - b.global);
}

export function install(app) {
  const bar = h('footer#keybar', { 'aria-label': T('Keys') });
  $('#body').after(bar);
  const on = () => app.prefs.get('keybar', 'show') !== 'hide';
  const phone = matchMedia('(max-width: 700px), (hover: none) and (pointer: coarse)');
  let msg = null, sig = null, raf = 0;

  const key = k => h('kbd', kbd(k.spec).join(' '));
  const press = run => e => { e.preventDefault(); run(); };
  const draw = hint => (hint.keys.length === 1
    ? h('button.kb-hint', { type: 'button', tabIndex: -1, onclick: press(hint.keys[0].run), class: hint.global && 'kb-global' }, key(hint.keys[0]), ' ', hint.label)
    : h('span.kb-hint', { class: hint.global && 'kb-global' }, hint.keys.map(k => h('button.kb-key', { type: 'button', tabIndex: -1, onclick: press(k.run) }, key(k))), ' ', hint.label));

  function paint() {
    raf = 0;
    bar.hidden = !on() || phone.matches;
    document.body.classList.toggle('has-keybar', !bar.hidden);
    if (bar.hidden || msg) return;
    const hints = barHints(app.keys.active());
    const now = bar.clientWidth + '|' + hints.map(x => x.label + x.keys.map(k => k.spec).join()).join('|');
    if (now === sig) return;
    sig = now;
    bar.replaceChildren(...hints.map(draw));
    // One line: drop the view's last hints, then the global ones, until it fits.
    const els = [...bar.children].reverse(), glob = el => el.classList.contains('kb-global');
    for (const el of [...els.filter(el => !glob(el)), ...els.filter(glob)]) {
      if (bar.scrollWidth <= bar.clientWidth + 1) break;
      el.remove();
    }
  }
  const later = () => { if (!raf) raf = requestAnimationFrame(paint); };

  // What applies changes with the scopes, the focus (panel or view), a pressed key (a tab, a mode) and the width.
  app.keys.onChange(later);
  for (const ev of ['focusin', 'pointerup', 'keyup']) document.addEventListener(ev, later, true);
  new ResizeObserver(later).observe(bar);
  phone.addEventListener('change', later);
  app.bus.on('prefs', p => { if (p && p.key === 'keybar') later(); });
  // Keeps the focus where it was, so a hint of the focused panel stays one.
  bar.addEventListener('mousedown', e => e.preventDefault());

  statusLine((text, kind, ms) => {
    if (bar.hidden || !bar.offsetParent) return null;
    const m = msg = h('span.kb-msg.' + kind, { role: kind === 'err' ? 'alert' : 'status', title: text }, text);
    bar.replaceChildren(m); sig = null;
    const close = () => { if (msg !== m) return; msg = null; paint(); };
    setTimeout(close, ms);
    return close;
  });

  const toggle = () => { app.prefs.set('keybar', on() ? 'hide' : 'show'); later(); };
  app.commands.register({ id: 'keybar', get title() { return on() ? T('Key bar: hide the keys at the bottom') : T('Key bar: show the keys at the bottom'); }, group: 'App', run: toggle });
  paint();
}
