// Writes that never reached Jira wait on the server (TUI: ⇡3). A header chip counts them;
// click or the palette lists them: send now, or drop one. The server retries on its own.
import { h, clear, $ } from './dom.js';
import { css } from './css.js';
import { ago } from './fmt.js';

export function install(app) {
  css('ask');
  const { api, ui, bus } = app;
  let list = [], timer = 0;
  const chip = h('button.btn.ghost.queue-chip', { hidden: true, onclick: () => open() });
  const anchor = $('#search-btn');
  anchor ? anchor.before(chip) : $('#top').append(chip);

  function paint() {
    chip.hidden = !list.length;
    chip.textContent = '⇡' + list.length;
    chip.title = list.length + ' write' + (list.length === 1 ? '' : 's') + ' waiting to reach Jira';
  }
  async function poll() {
    let next;
    try { next = await api.get('/queue', { fresh: true }); } catch (e) { return; }
    const gone = list.filter(w => !next.some(n => n.At === w.At && n.Path === w.Path));
    list = next; paint();
    if (gone.length) for (const k of new Set(gone.map(w => w.Key).filter(Boolean))) bus.emit('issue:changed', { key: k });
    if (gone.length && !next.length) ui.toast('Back online: queued writes sent');
  }
  const arm = () => { clearInterval(timer); timer = document.hidden ? 0 : setInterval(poll, 20000); };
  document.addEventListener('visibilitychange', () => { arm(); if (!document.hidden) poll(); });
  window.addEventListener('online', poll);
  window.addEventListener('lw:queued', () => { ui.toast('Offline: kept for later, sent when Jira is back'); poll(); });
  poll(); arm();

  async function open() {
    await poll();
    if (!list.length) return ui.toast('Nothing waiting to reach Jira');
    const body = h('div.queue');
    const m = ui.modal(body, { title: 'Offline writes', wide: true });
    const draw = () => {
      clear(body);
      if (!list.length) return m.close();
      body.append(
        h('ul.queue-list', list.map(w => h('li', h('b', w.What), ' ', h('span.dim', w.Method + ' ' + w.Key), h('span.spacer'), h('span.dim', ago(w.At)),
          h('button.btn.ghost.sm', { title: 'Drop this write', onclick: async () => {
            if (!await ui.confirm({ title: 'Drop write', text: w.What + ' on ' + w.Key + ' will never be sent.', ok: 'Drop', danger: true })) return;
            try { await api.del('/queue/' + w.Index); } catch (e) { return ui.errToast(e); }
            await poll(); draw();
          } }, 'Drop')))),
        h('div.row.end',
          h('button.btn', { onclick: () => send(false) }, 'Retry'),
          h('button.btn.primary', { title: 'Also over changes made in Jira since', onclick: () => send(true) }, 'Send now')));
    };
    async function send(force) {
      let r;
      try { r = await api.post('/queue/send', { Force: force }); } catch (e) { return ui.errToast(e); }
      for (const f of r.Failed || []) ui.toast('Refused, dropped: ' + f, { kind: 'err' });
      if (r.Conflict) ui.toast(r.Conflict + ' changed in Jira since you queued a write. Send now goes over it.', { kind: 'err' });
      else if (r.Error) ui.toast(r.Offline ? 'Still offline' : r.Error, { kind: 'err' });
      else if (r.Sent) ui.toast('Sent ' + r.Sent + ' queued write' + (r.Sent === 1 ? '' : 's'));
      await poll(); draw();
    }
    draw();
  }
  app.commands.register({ id: 'queue', title: 'Offline writes: send or drop', group: 'App', run: open, when: () => list.length > 0 });
}
