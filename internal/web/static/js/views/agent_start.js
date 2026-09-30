// Start work on an issue (S): pick the agent, branch and prompt, then the server opens the worktree in
// herdr, starts the agent, and moves / assigns the issue as configured. The timer starts here.
// Another (alt+s): a further agent in the worktree of the issue, whatever runs there.
import { h } from '../lib/dom.js';
import { css } from '../lib/css.js';

const show = (app, key) => app.go('/agents?agent=' + encodeURIComponent(key));

export async function startWork(app, key, { force = false } = {}) {
  css('agents');
  const { api, ui } = app;
  let f;
  try { f = await api.get('/issues/' + key + '/work', { fresh: true }); } catch (e) { return ui.errToast(e); }
  const project = key.split('-')[0];
  if (!f.Herdr) return ui.toast('herdr is not running: start herdr (the agents need it), then try again', { kind: 'err', ms: 8000 });
  if (f.Running.length && !force) {
    try {
      await api.post('/agents/' + f.Running[0].PaneID + '/focus');
      ui.toast(key + ': focused its agent in herdr', { action: { label: 'Agents', run: () => show(app, key) } });
    } catch (e) { ui.errToast(e); }
    return;
  }
  if (!f.Repo) return ui.toast('No repo for ' + project + ': set jira.repos.' + project + ' to its git checkout in the config', { kind: 'err', ms: 10000 });

  const another = force && f.Running.length > 0;
  const agent = h('select.input', f.Agents.map(a => h('option', { value: a, selected: a === f.Agent }, a)));
  const branch = h('input.input.mono', { type: 'text', value: f.Branch, spellcheck: false });
  const prompt = h('textarea.input', { rows: 7, value: f.Prompt, placeholder: 'Empty starts the agent without a prompt' });
  const also = !another && f.Actions.length && h('input', { type: 'checkbox', checked: true });
  const err = h('div.form-err');
  const go = h('button.btn.primary', { type: 'submit' }, 'Start');
  let m, busy = false;
  const submit = async e => {
    e && e.preventDefault();
    if (busy) return;
    busy = true; go.disabled = true; err.textContent = '';
    go.textContent = 'Starting…';
    err.textContent = 'Opening the worktree and starting ' + agent.value + '…';
    err.classList.add('busy');
    try {
      const r = await api.post('/issues/' + key + '/work', { Agent: agent.value, Branch: another ? '' : branch.value.trim(), Prompt: prompt.value, Actions: !!(also && also.checked), Another: another });
      m.close();
      if (r.Running) { ui.toast(key + ': its agent runs already, focused'); api.post('/agents/' + r.Pane + '/focus').catch(() => {}); return; }
      const did = [...r.Did];
      if (r.Timer && !app.timer.current) { app.timer.start(key); did.push('timer started'); }
      ui.toast(key + ': ' + r.Agent + ' started on ' + r.Branch + (did.length ? ' · ' + did.join(', ') : ''), { ms: 8000, action: { label: 'Agents', run: () => show(app, key) } });
      if (r.Warn) ui.toast(key + ': ' + r.Warn, { kind: 'err', ms: 10000 });
      app.bus.emit('issue:changed', { key });
      app.agents.refresh();
    } catch (e2) {
      busy = false; go.disabled = false; go.textContent = 'Start';
      err.classList.remove('busy');
      err.textContent = e2.message || String(e2);
    }
  };
  const body = h('form.form-dialog.start-work', { onsubmit: submit },
    h('div.form',
      h('label.form-label', 'Agent'), h('div.form-field', agent),
      !another && h('label.form-label', 'Branch'), !another && h('div.form-field', branch, h('span.form-hint.dim', f.Repo)),
      another && h('label.form-label', 'Where'), another && h('div.form-field', h('span.form-hint.dim', 'a new tab in the worktree of ' + key + ' (' + f.Running.length + ' running)')),
      h('label.form-label', 'Prompt'), h('div.form-field', prompt),
      also && h('label.form-label', 'Also'), also && h('div.form-field', h('label.check', also, f.Actions.join(' · ')))),
    err,
    h('div.row.end', h('span.form-hint.dim', 'enter starts · ctrl+enter from the prompt'), h('span.spacer'), h('button.btn', { type: 'button', onclick: () => m.close() }, 'Cancel'), go));
  m = ui.modal(body, { title: (another ? 'Another agent on ' : 'Start work on ') + key, wide: true });
  m.scope.bind('ctrl+Enter', () => submit(), '', { input: true, hidden: true });
  go.focus();
}
