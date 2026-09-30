// Start work on an issue (S): pick the agent, branch and prompt, then the server opens the worktree in
// herdr, starts the agent, and moves / assigns the issue as configured. The timer starts here.
import { h } from '../lib/dom.js';
import { css } from '../lib/css.js';

export async function startWork(app, key, { force = false } = {}) {
  css('agents');
  const { api, ui } = app;
  let f;
  try { f = await api.get('/issues/' + key + '/work', { fresh: true }); } catch (e) { return ui.errToast(e); }
  if (!f.Herdr) return ui.toast('Start work needs herdr running', { kind: 'err' });
  if (f.Running.length && !force) {
    try { await api.post('/agents/' + f.Running[0].PaneID + '/focus'); ui.toast(key + ': focused its agent in herdr'); } catch (e) { ui.errToast(e); }
    return;
  }
  if (!f.Repo) return ui.toast('No jira.repos entry for ' + key.split('-')[0], { kind: 'err' });

  const agent = h('select.input', f.Agents.map(a => h('option', { value: a, selected: a === f.Agent }, a)));
  const branch = h('input.input.mono', { type: 'text', value: f.Branch, spellcheck: false });
  const prompt = h('textarea.input', { rows: 7, value: f.Prompt, placeholder: 'Empty starts the agent without a prompt' });
  const also = f.Actions.length && h('input', { type: 'checkbox', checked: true });
  const err = h('div.form-err');
  const go = h('button.btn.primary', { type: 'submit' }, 'Start');
  let m;
  const submit = async e => {
    e && e.preventDefault();
    go.disabled = true; err.textContent = '';
    const close = ui.toast(key + ': starting work…', { ms: 120000 });
    m.close();
    try {
      const r = await api.post('/issues/' + key + '/work', { Agent: agent.value, Branch: branch.value.trim(), Prompt: prompt.value, Actions: !!(also && also.checked) });
      close();
      if (r.Running) { ui.toast(key + ': its agent runs already, focused'); api.post('/agents/' + r.Pane + '/focus').catch(() => {}); return; }
      const did = [...r.Did];
      if (r.Timer && !app.timer.current) { app.timer.start(key); did.push('timer started'); }
      ui.toast(key + ': ' + r.Agent + ' started on ' + r.Branch + (did.length ? ' · ' + did.join(', ') : ''), { ms: 6000 });
      if (r.Warn) ui.toast(key + ': ' + r.Warn, { kind: 'err' });
      app.bus.emit('issue:changed', { key });
      app.agents.refresh();
    } catch (e2) { close(); ui.errToast(e2); }
  };
  const body = h('form.form-dialog.start-work', { onsubmit: submit },
    h('div.form',
      h('label.form-label', 'Agent'), h('div.form-field', agent),
      h('label.form-label', 'Branch'), h('div.form-field', branch, h('span.form-hint.dim', f.Repo)),
      h('label.form-label', 'Prompt'), h('div.form-field', prompt),
      also && h('label.form-label', 'Also'), also && h('div.form-field', h('label.check', also, f.Actions.join(' · ')))),
    err,
    h('div.row.end', h('span.form-hint.dim', 'ctrl+enter starts'), h('span.spacer'), h('button.btn', { type: 'button', onclick: () => m.close() }, 'Cancel'), go));
  m = ui.modal(body, { title: 'Start work on ' + key, wide: true });
  m.scope.bind('ctrl+Enter', () => submit(), '', { input: true, hidden: true });
  go.focus();
}
