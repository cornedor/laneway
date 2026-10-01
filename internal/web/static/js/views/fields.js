// Editing an issue's fields: editField(app, key, field) opens the right picker or
// prompt, writes optimistically, toasts with Undo and pushes onto the session undo stack.
// Also the form widgets (fieldInput, formDialog) that create, bulk and the transition form share.
import { h } from '../lib/dom.js';
import { css } from '../lib/css.js';
import { keys } from '../lib/keys.js';

css('forms');

// ---- undo: a session stack of {what, run}; `u` takes the latest back.
const stack = [];
let scope = null;

export function installUndo(app) {
  if (scope) return;
  scope = keys.scope('undo');
  scope.bind('u', () => undo(app), 'undo last edit', { group: 'Edit', when: () => stack.length > 0 });
}

// pushUndo(app, what, run): run() is an async function that takes the change back.
// Anything that writes (a board drag, say) can push here so `u` covers it.
export function pushUndo(app, what, run) {
  installUndo(app);
  const step = { what, run };
  stack.push(step);
  if (stack.length > 50) stack.shift();
  return step;
}

export function undo(app, step) {
  step = step || stack[stack.length - 1];
  if (!step) { app.ui.toast('Nothing to undo'); return Promise.resolve(false); }
  const i = stack.indexOf(step);
  if (i >= 0) stack.splice(i, 1);
  return step.run().then(() => { app.ui.toast('Undid ' + step.what); return true; }, e => { app.ui.errToast(e); return false; });
}

const undoAction = (app, step) => ({ label: 'Undo', run: () => undo(app, step) });

// write: PUT one field. Views listening for 'issue:patch' {key, field, value} can show the
// change at once; everything refetches on 'issue:changed'. Resolves true on success.
async function write(app, key, field, body, what, patch) {
  app.bus.emit('issue:patch', { key, field, value: patch });
  try {
    const r = await app.api.put(`/issues/${key}/field/${field}`, body);
    app.bus.emit('issue:changed', { key, what });
    const u = r && r.Undo;
    const step = u && pushUndo(app, what, async () => {
      await app.api.put(`/issues/${key}/field/${u.Field}`, u);
      app.bus.emit('issue:changed', { key });
    });
    app.ui.toast(what, { kind: 'ok', action: step && undoAction(app, step) });
    return true;
  } catch (e) {
    app.ui.errToast(e);
    app.bus.emit('issue:changed', { key });
    return false;
  }
}

// bulkUndo pushes one step that reverts a bulk result's per-issue edits.
export function bulkUndo(app, what, res) {
  if (!res.Undo || !Object.keys(res.Undo).length) return null;
  return pushUndo(app, what, async () => {
    const r = await app.api.post('/bulk', { Each: res.Undo });
    for (const k of r.Done || []) app.bus.emit('issue:changed', { key: k });
    const bad = Object.keys(r.Failed || {});
    if (bad.length) throw new Error(`${bad.length} not undone: ${bad.join(', ')}`);
  });
}

const projectOf = key => key.split('-')[0];
const issueOf = (app, key) => app.api.get('/issues/' + key);

// ---- entry point
const editors = { status, priority, assignee, reporter, points, labels, summary, duedate, issuetype, flag, sprint, delete: remove };

export async function editField(app, key, field, anchor) {
  installUndo(app);
  try {
    const f = editors[field === 'due' ? 'duedate' : field === 'type' ? 'issuetype' : field === 'estimate' ? 'points' : field];
    if (f) return await f(app, key, anchor);
    return await custom(app, key, field);
  } catch (e) { app.ui.errToast(e); }
}

async function priority(app, key) {
  const [iss, list] = await Promise.all([issueOf(app, key), app.api.get('/priorities')]);
  const p = await app.ui.pick({ title: key + ' priority', items: list, label: p => p.Name, detail: p => (p.ID === iss.PriorityID ? 'current' : ''), placeholder: 'Priority…' });
  if (!p || p.ID === iss.PriorityID) return;
  await write(app, key, 'priority', { ID: p.ID }, `${key} priority → ${p.Name}`, p.Name);
}

async function assignee(app, key) {
  const u = await pickUser(app, { issue: key, title: key + ' assignee', specials: true });
  if (!u) return;
  await write(app, key, 'assignee', { ID: u.AccountID }, u.none ? `${key} unassigned` : `${key} → ${u.DisplayName}`, u.none ? null : { AccountID: u.AccountID, DisplayName: u.DisplayName });
}

async function reporter(app, key) {
  const u = await pickUser(app, { issue: key, title: key + ' reporter' });
  if (!u) return;
  await write(app, key, 'reporter', { ID: u.AccountID }, `${key} reporter → ${u.DisplayName}`, u.DisplayName);
}

async function points(app, key) {
  const iss = await issueOf(app, key);
  const v = await app.ui.prompt({ title: key + ' story points (empty clears)', value: iss.StoryPoints || '', placeholder: '5' });
  if (v === null || v.trim() === (iss.StoryPoints || '')) return;
  if (v.trim() && isNaN(Number(v))) return app.ui.toast('Story points must be a number', { kind: 'err' });
  await write(app, key, 'points', { Text: v.trim() }, `${key} points → ${v.trim() || 'none'}`, v.trim());
}

async function summary(app, key) {
  const iss = await issueOf(app, key);
  const v = await app.ui.prompt({ title: key + ' summary', value: iss.Summary });
  if (v === null || !v.trim() || v.trim() === iss.Summary) return;
  await write(app, key, 'summary', { Text: v.trim() }, key + ' summary changed', v.trim());
}

async function duedate(app, key) {
  const m = await app.api.get(`/issues/${key}/editmeta`, { fresh: true });
  const v = await app.ui.prompt({ title: key + ' due date (2026-10-01, today, +3d, fri; empty clears)', value: m.Due || '' });
  if (v === null || v.trim() === (m.Due || '')) return;
  await write(app, key, 'duedate', { Text: v.trim() }, `${key} due → ${v.trim() || 'none'}`, v.trim());
}

async function labels(app, key) {
  const iss = await issueOf(app, key);
  const cur = iss.Labels || [];
  const next = await pickLabels(app, projectOf(key), cur, key + ' labels');
  if (!next) return;
  const Add = next.filter(l => !cur.includes(l)), Remove = cur.filter(l => !next.includes(l));
  if (!Add.length && !Remove.length) return;
  await write(app, key, 'labels', { Add, Remove }, `${key} labels ${[...Add.map(l => '+' + l), ...Remove.map(l => '-' + l)].join(' ')}`, next);
}

async function issuetype(app, key) {
  const [m, t] = await Promise.all([app.api.get(`/issues/${key}/editmeta`, { fresh: true }), app.api.get(`/projects/${projectOf(key)}/issuetypes`)]);
  const sub = (t.Subtasks || []).some(x => x.Name === m.Type);
  const list = (sub ? t.Subtasks : t.Types).filter(x => x.Name !== m.Type);
  if (!list.length) return app.ui.toast('No other issue type to change to');
  const o = await app.ui.pick({ title: `${key} type (now ${m.Type})`, items: list, label: x => x.Name });
  if (o) await write(app, key, 'issuetype', { ID: o.ID }, `${key} type → ${o.Name}`, o.Name);
}

async function flag(app, key) {
  const m = await app.api.get(`/issues/${key}/editmeta`, { fresh: true });
  if (m.Flagged === undefined) return app.ui.toast('This Jira has no Flagged field', { kind: 'err' });
  await write(app, key, 'flag', { On: !m.Flagged }, m.Flagged ? `${key} unflagged` : `${key} flagged`, !m.Flagged);
}

async function sprint(app, key) {
  const [m, s] = await Promise.all([app.api.get(`/issues/${key}/editmeta`, { fresh: true }), app.api.get(`/projects/${projectOf(key)}/sprints`)]);
  const items = [{ ID: 0, Name: 'Backlog' }, ...s.Sprints];
  const cur = m.Sprint && m.Sprint.ID ? m.Sprint.ID : 0;
  const o = await app.ui.pick({ title: `${key} sprint`, items, label: x => x.Name, detail: x => (x.ID === cur ? 'current' : x.State || '') });
  if (!o || o.ID === cur) return;
  await write(app, key, 'sprint', { Sprint: o.ID }, o.ID ? `${key} → ${o.Name}` : `${key} → backlog`, o.Name);
}

async function remove(app, key) {
  const iss = await issueOf(app, key);
  if (!(await app.ui.confirm({ title: 'Delete ' + key + '?', text: iss.Summary + '. This cannot be undone.', ok: 'Delete', danger: true }))) return;
  let sub = false;
  for (;;) {
    try {
      await app.api.del(`/issues/${key}${sub ? '?subtasks=1' : ''}`);
      break;
    } catch (e) {
      if (sub || !/subtask/i.test(e.message)) throw e;
      if (!(await app.ui.confirm({ title: key + ' has subtasks', text: 'Delete them with it?', ok: 'Delete all', danger: true }))) return;
      sub = true;
    }
  }
  if (app.panel.key === key) app.panel.close();
  app.bus.emit('issue:changed', { key, deleted: true });
  app.ui.toast(key + ' deleted', { kind: 'ok' });
}

// Any other field id: the edit screen's own field, written from its kind.
async function custom(app, key, id) {
  const m = await app.api.get(`/issues/${key}/editmeta`, { fresh: true });
  const fm = (m.Fields || []).find(f => f.ID === id || f.Name.toLowerCase() === id.toLowerCase());
  if (!fm) return app.ui.toast(`${key} has no editable “${id}”`, { kind: 'err' });
  const w = fieldInput(app, fm, { issue: key, project: projectOf(key), value: (m.Values || {})[fm.ID] });
  formDialog(app, {
    title: `${key} ${fm.Name}`, rows: [{ label: fm.Name, widget: w }],
    submit: async () => {
      const r = await app.api.put(`/issues/${key}/field/${fm.ID}`, { Kind: fm.Kind, Value: w.get() });
      app.bus.emit('issue:changed', { key, what: `${key} ${fm.Name} changed` });
      const u = r && r.Undo;
      const step = u && pushUndo(app, `${key} ${fm.Name}`, async () => { await app.api.put(`/issues/${key}/field/${u.Field}`, u); app.bus.emit('issue:changed', { key }); });
      app.ui.toast(`${key} ${fm.Name} changed`, { kind: 'ok', action: step && undoAction(app, step) });
    },
  });
}

// ---- status: a picker of the moves on offer; a move the workflow wants fields for opens a form
async function status(app, key) {
  const metaP = app.api.get(`/issues/${key}/transitionmeta`, { fresh: true });
  metaP.catch(() => {});
  // Open at once; the moves fill in when Jira answers.
  const list = app.api.get(`/issues/${key}/transitions`, { fresh: true });
  const t = await app.ui.pick({ title: `Move ${key}`, items: list, label: t => t.Name, placeholder: 'Move to…', empty: 'No moves from here' });
  if (!t) return;
  let meta;
  try { meta = ((await metaP).Transitions || []).find(x => x.ID === t.ID); } catch (e) { meta = null; }
  if (!meta || !meta.NeedsInput) {
    await write(app, key, 'status', { ID: t.ID }, `${key} → ${t.Name}`, t.Name);
    return;
  }
  moveForm(app, key, meta);
}

function moveForm(app, key, t) {
  const rows = t.Fields.map(f => ({ label: f.Name, required: f.Required && f.Kind !== 'other', widget: fieldInput(app, f, { issue: key, project: projectOf(key), value: f.Value, required: f.Required }) }));
  formDialog(app, {
    title: `Move ${key} → ${t.ToName}`, intro: t.Message, rows, ok: 'Move',
    submit: async () => {
      const out = [];
      rows.forEach((r, i) => { const f = t.Fields[i]; if (f.Kind !== 'other' && r.widget.touched()) out.push({ ID: f.ID, Kind: f.Kind, Value: r.widget.get() }); });
      app.bus.emit('issue:patch', { key, field: 'status', value: t.ToName });
      let r;
      try { r = await app.api.post(`/issues/${key}/transitionwith`, { ID: t.ID, Fields: out }); } catch (e) { app.bus.emit('issue:changed', { key }); throw e; }
      app.bus.emit('issue:changed', { key, what: `${key} → ${t.ToName}` });
      const u = r && r.Undo;
      const step = u && pushUndo(app, `${key} → ${t.ToName}`, async () => { await app.api.put(`/issues/${key}/field/status`, u); app.bus.emit('issue:changed', { key }); });
      app.ui.toast(`${key} → ${t.ToName}`, { kind: 'ok', action: step && undoAction(app, step) });
    },
  });
}

// ---- pickers

// pickUser → Promise<{AccountID, DisplayName, me?, none?} | null>. `specials` adds Me and Unassigned.
export function pickUser(app, { issue, project, title = 'Person', specials = false } = {}) {
  const me = app.session && app.session.me;
  const scopeQ = issue ? 'issue=' + encodeURIComponent(issue) : 'project=' + encodeURIComponent(project);
  const head = () => {
    if (!specials) return [];
    const out = [];
    if (me && me.AccountID) out.push({ AccountID: me.AccountID, DisplayName: me.DisplayName, me: true });
    out.push({ AccountID: '', DisplayName: 'Unassigned', none: true });
    return out;
  };
  const search = async q => {
    const us = await app.api.get(`/users?${scopeQ}&q=${encodeURIComponent(q)}`, { fresh: true });
    return [...head(), ...us.filter(u => !(specials && me && u.AccountID === me.AccountID))];
  };
  return app.ui.pick({
    title, items: head(), search, placeholder: 'Search people…',
    label: u => (u.me ? 'Me · ' + u.DisplayName : u.DisplayName),
    render: u => h('span.pick-user', u.none ? app.ui.avatar(null) : app.ui.avatar(u.DisplayName), h('span.pick-label', u.me ? 'Me · ' + u.DisplayName : u.DisplayName)),
  });
}

// pickLabels → Promise<string[] | null>: toggle suggestions, type to create a new one.
export async function pickLabels(app, project, current = [], title = 'Labels') {
  let recent = [];
  try { recent = (await app.api.get(`/projects/${project}/labels`)) || []; } catch (e) { /* suggestions are optional */ }
  const items = [...new Set([...current, ...recent])];
  return app.ui.pick({ title, items, multi: true, selected: current, create: q => q.trim().replace(/\s+/g, '-'), placeholder: 'Filter or type a new label…', empty: 'No labels yet: type one' });
}

// ---- form widgets

export function formRow(label, el, required) {
  return [h('label.form-label' + (required ? '.req' : ''), label), h('div.form-field', el)];
}

// formDialog({title, intro, rows:[{label, required, widget}], ok, submit}): a modal form. submit()
// may throw; its message shows in the form and the dialog stays. ctrl+Enter submits.
export function formDialog(app, { title, intro, rows, ok = 'Save', submit, wide = false }) {
  const err = h('div.form-err', { role: 'alert' });
  const okBtn = h('button.btn.primary', { type: 'submit' }, ok);
  const form = h('form.form-dialog', { onsubmit: e => { e.preventDefault(); go(); } },
    intro && h('p.dim', intro),
    h('div.form', rows.map(r => formRow(r.label, r.widget.el, r.required))), err,
    h('div.row.end', h('span.faint.form-hint', 'ctrl+⏎ saves'), h('button.btn', { type: 'button', onclick: () => m.close() }, 'Cancel'), okBtn));
  const m = app.ui.modal(form, { title, wide });
  m.scope.bind('ctrl+Enter', () => go(), 'submit', { input: true, hidden: true });
  const firstEmpty = rows.find(r => r.widget.empty());
  const target = firstEmpty && firstEmpty.widget.el.querySelector ? firstEmpty.widget.el : null;
  const focus = target && (target.matches('input,textarea,select,button') ? target : target.querySelector('input,textarea,select,button'));
  if (focus) focus.focus();
  async function go() {
    const missing = rows.filter(r => r.required && r.widget.empty()).map(r => r.label);
    if (missing.length) { err.textContent = 'Fill in ' + missing.join(', '); return; }
    okBtn.disabled = true; err.textContent = '';
    try { await submit(); m.close(); } catch (e) { err.textContent = e.message; okBtn.disabled = false; }
  }
  return m;
}

const HINT = { number: '5', date: '2026-10-01, today, +3d, fri', time: 'fri 14:00', issue: 'ABC-123' };

// fieldInput(app, fieldMeta, {project, issue, value, required}) → {el, get() → Value, empty(), touched()}.
// The Value has the shape the API encodes for fm.Kind (see jira.EncodeValue).
export function fieldInput(app, fm, { project, issue, value, required } = {}) {
  const v = value || {};
  let dirty = false;
  const touch = () => { dirty = true; };
  const w = { el: null, touch, touched: () => dirty, get: () => ({}), empty: () => true };
  // A button-like control redrawn from state.
  const control = (render, choose) => {
    const box = h('div.field-btn');
    const draw = () => { box.replaceChildren(...[render()].flat().filter(Boolean), h('button.btn', { type: 'button', onclick: async () => { await choose(); touch(); draw(); } }, 'Choose…')); };
    draw();
    return box;
  };
  switch (fm.Kind) {
    case 'text': case 'number': case 'date': case 'time': case 'issue': {
      const i = h('input.input', { type: 'text', value: v.Text || '', placeholder: HINT[fm.Kind] || '', oninput: touch });
      if (fm.Kind === 'number') i.setAttribute('inputmode', 'decimal');
      w.el = i; w.get = () => ({ Text: i.value }); w.empty = () => !i.value.trim();
      break;
    }
    case 'doc': case 'comment': {
      const t = h('textarea.input', { rows: 4, value: v.Text || '', oninput: touch });
      w.el = t; w.get = () => ({ Text: t.value }); w.empty = () => !t.value.trim();
      break;
    }
    case 'strings':
      if (fm.Clause === 'labels') {
        let ls = (v.Text || '').split(/\s+/).filter(Boolean);
        w.el = control(() => [ls.length ? h('span.chips', ls.map(l => app.ui.chip(l))) : h('span.faint', 'none')], async () => {
          const next = await pickLabels(app, project || projectOf(issue || ''), ls, fm.Name);
          if (next) ls = next;
        });
        w.get = () => ({ Text: ls.join(' ') }); w.empty = () => !ls.length;
      } else {
        const i = h('input.input', { type: 'text', value: v.Text || '', placeholder: 'words, separated by spaces', oninput: touch });
        w.el = i; w.get = () => ({ Text: i.value }); w.empty = () => !i.value.trim();
      }
      break;
    case 'user': {
      let u = (v.Users || [])[0] || null;
      w.el = control(() => [u ? h('span.pick-user', app.ui.avatar(u.DisplayName), u.DisplayName) : h('span.faint', 'none')], async () => {
        const p = await pickUser(app, { issue, project, title: fm.Name, specials: true });
        if (p) u = p.none ? null : p;
      });
      w.get = () => ({ Users: u ? [{ AccountID: u.AccountID, DisplayName: u.DisplayName }] : [] }); w.empty = () => !u;
      break;
    }
    case 'users': {
      let us = [...(v.Users || [])];
      w.el = control(() => [us.length ? h('span.chips', us.map(u => app.ui.chip(u.DisplayName))) : h('span.faint', 'none')], async () => {
        const p = await pickUser(app, { issue, project, title: fm.Name });
        if (p && !us.some(x => x.AccountID === p.AccountID)) us = [...us, p];
      });
      w.get = () => ({ Users: us.map(u => ({ AccountID: u.AccountID, DisplayName: u.DisplayName })) }); w.empty = () => !us.length;
      break;
    }
    case 'option': case 'sprint': {
      const s = h('select.input', { onchange: touch }, h('option', { value: '' }, '—'));
      const fill = opts => { for (const o of opts) s.append(h('option', { value: o.ID, selected: (v.Options || [])[0] && v.Options[0].ID === String(o.ID) }, o.Name)); };
      if (fm.Kind === 'sprint') {
        const p = project || projectOf(issue || '');
        app.api.get(`/projects/${p}/sprints`).then(r => fill(r.Sprints.map(x => ({ ID: String(x.ID), Name: x.Name })))).catch(() => {});
      } else fill(fm.Options || []);
      w.el = s; w.get = () => ({ Options: s.value ? [{ ID: s.value, Name: s.selectedOptions[0].text }] : [] }); w.empty = () => !s.value;
      break;
    }
    case 'options': {
      const sel = new Set((v.Options || []).map(o => o.ID));
      const boxes = (fm.Options || []).map(o => h('label.check', h('input', { type: 'checkbox', checked: sel.has(o.ID), onchange: touch }), ' ' + o.Name));
      w.el = h('div.checks', boxes);
      w.get = () => ({ Options: (fm.Options || []).filter((o, i) => boxes[i].firstChild.checked).map(o => ({ ID: o.ID, Name: o.Name })) });
      w.empty = () => !w.get().Options.length;
      break;
    }
    default:
      w.el = h('span.faint', 'Not editable here' + (required ? ': set it in Jira' : ''));
  }
  return w;
}
