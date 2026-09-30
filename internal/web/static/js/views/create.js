// Create dialog: openCreate(app, {project, parent, type, summary, sprint}).
// Several lines in the summary make several issues. ctrl+Enter creates.
import { h } from '../lib/dom.js';
import { css } from '../lib/css.js';
import { fieldInput, formRow } from './fields.js';
import { mdEdit } from '../lib/mdedit.js';

css('forms');

const SKIP = new Set(['summary', 'description', 'project', 'issuetype', 'reporter', 'attachment', 'issuelinks', 'comment']);
const COMMON = new Set(['priority', 'assignee', 'labels', 'parent']);
const bullet = /^(?:[-*+•]\s+)?(?:\[[ xX]\]\s+)?(?:\d+[.)]\s+)?/;

// Summaries in the text: one per line, list markers dropped when there are several.
export function summaries(text) {
  const lines = text.split('\n').map(l => l.trim()).filter(Boolean);
  return lines.length > 1 ? lines.map(l => l.replace(bullet, '').trim()).filter(Boolean) : lines;
}

let draft = null; // a closed, unsent form comes back next time

export async function openCreate(app, opts = {}) {
  const prefs = app.prefs;
  const restore = !opts.summary && draft && (!opts.project || opts.project === draft.project) ? draft : null;
  let project = opts.project || (restore && restore.project) || prefs.get('create.project', '') || ((app.session.projects || [])[0] || '');
  let type = opts.type || (restore && restore.type) || '';
  let widgets = new Map(), fields = [], pseq = 0, fseq = 0, busy = false, submitted = false;
  const kept = new Map(); // values typed, kept across a type change

  const projectSel = h('select.input', { onchange: () => { project = projectSel.value; type = ''; loadProject(); } }, h('option', { value: project }, project || '…'));
  const typeSel = h('select.input', { onchange: () => { type = typeSel.value; loadFields(); } });
  const summary = h('textarea.input', { rows: 2, placeholder: 'Summary. One per line makes several issues.', value: opts.summary || (restore && restore.summary) || '', oninput: count });
  const files = [];
  const fileBar = h('div.ed-files');
  const paintFiles = () => fileBar.replaceChildren(...files.map((f, i) => h('span.chip', f.name || 'image', ' ', h('button.btn.ghost.sm', { type: 'button', onclick: () => { files.splice(i, 1); paintFiles(); } }, '✕'))));
  const ed = mdEdit(app, { value: restore ? restore.description : '', rows: 5, placeholder: 'Description (markdown). / formats, @ mentions, drop files to attach', noCancel: true,
    hint: 'files attach after creating', project: () => project, onFiles: fs => { files.push(...fs); paintFiles(); } });
  const description = ed.ta;
  const sprintSel = h('select.input', h('option', { value: '' }, 'None (backlog)'));
  const sprintRow = h('div.form-sprint', { hidden: true });
  const count$ = h('div.faint.form-hint');
  const extra = h('div.form');
  const more = h('details.form-more', { hidden: true }, h('summary', 'More fields'), h('div.form'));
  const err = h('div.form-err', { role: 'alert' });
  const another = h('input', { type: 'checkbox', checked: prefs.get('create.another', '') === '1', onchange: () => prefs.set('create.another', another.checked ? '1' : '0') });
  const okBtn = h('button.btn.primary', { type: 'submit' }, 'Create');

  const form = h('form.form-dialog', { onsubmit: e => { e.preventDefault(); submit(); } },
    h('div.form', formRow('Project', projectSel), formRow('Type', typeSel), formRow('Summary', [summary, count$], true), formRow('Description', [ed.el, fileBar], true)),
    extra, sprintRow, more, err,
    h('div.row.end', h('label.check', another, ' Create another'), h('span.spacer'), h('span.faint.form-hint', 'ctrl+⏎ creates'),
      h('button.btn', { type: 'button', onclick: () => m.close() }, 'Cancel'), okBtn));
  const m = app.ui.modal(form, { title: 'Create issue', wide: true, onClose: () => { if (!submitted && summary.value.trim()) draft = { project, type, summary: summary.value, description: description.value }; } });
  m.scope.bind('ctrl+Enter', () => submit(), 'create', { input: true, hidden: true });
  summary.focus();
  count();

  function count() {
    const n = summaries(summary.value).length;
    count$.textContent = n > 1 ? `${n} issues will be created` : '';
  }

  // The first project key is shown at once; the creatable ones fill in.
  app.api.get('/projects/creatable').then(ps => {
    if (!ps.length) return;
    const was = project;
    if (!ps.some(p => p.Key === project)) project = ps[0].Key;
    projectSel.replaceChildren(...ps.map(p => h('option', { value: p.Key, selected: p.Key === project }, `${p.Key} ${p.Name}`)));
    if (project !== was) loadProject();
  }).catch(() => {});
  if (project) loadProject();

  async function loadProject() {
    const my = ++pseq;
    if (!project) return;
    try {
      const t = await app.api.get(`/projects/${project}/issuetypes`);
      if (my !== pseq) return;
      const all = [...t.Types, ...(opts.parent ? t.Subtasks || [] : [])];
      if (!all.some(x => x.Name === type)) type = prefs.get('create.type.' + project, '');
      if (!all.some(x => x.Name === type)) type = (all.find(x => /^(task|story)$/i.test(x.Name)) || all[0] || {}).Name || '';
      typeSel.replaceChildren(...all.map(x => h('option', { value: x.Name, selected: x.Name === type }, x.Name)));
      loadFields();
      loadSprints();
    } catch (e) { err.textContent = e.message; }
  }

  async function loadSprints() {
    const p = project;
    sprintRow.hidden = true;
    try {
      const r = await app.api.get(`/projects/${p}/sprints`);
      if (p !== project || !r.Sprints.length) return;
      const want = opts.sprint ? String(opts.sprint) : '';
      sprintSel.replaceChildren(h('option', { value: '' }, 'None (backlog)'), ...r.Sprints.map(s => h('option', { value: s.ID, selected: String(s.ID) === want }, `${s.Name} (${s.State})`)));
      sprintRow.replaceChildren(h('div.form', formRow('Sprint', sprintSel)));
      sprintRow.hidden = false;
    } catch (e) { /* no board: no sprint */ }
  }

  async function loadFields() {
    const my = ++fseq;
    for (const [id, w] of widgets) if (w.touched()) kept.set(id, w.get());
    try {
      const fs = await app.api.get(`/projects/${project}/createfields?type=${encodeURIComponent(type)}`);
      if (my !== fseq) return;
      fields = fs.filter(f => !SKIP.has(f.ID) && f.Kind !== 'other' && f.Kind !== 'comment' && f.Kind !== 'sprint');
      widgets = new Map();
      const req = [], common = [], rest = [];
      for (const f of fields) {
        const value = kept.get(f.ID) || (f.ID === 'parent' && opts.parent ? { Text: opts.parent } : undefined);
        const w = fieldInput(app, f, { project, value, required: f.Required });
        if (kept.has(f.ID)) w.touch();
        widgets.set(f.ID, w);
        (f.Required ? req : COMMON.has(f.ID) ? common : rest).push(formRow(f.Name, w.el, f.Required));
      }
      extra.replaceChildren(...req.flat(), ...common.flat());
      more.lastChild.replaceChildren(...rest.flat());
      more.firstChild.textContent = `More fields (${rest.length})`;
      more.hidden = !rest.length;
    } catch (e) { err.textContent = e.message; }
  }

  async function attach(key) {
    for (const f of files.splice(0)) {
      const fd = new FormData(); fd.append('file', f, f.name || 'pasted-' + Date.now() + '.png');
      try { const res = await fetch('/api/issues/' + key + '/attachments', { method: 'POST', body: fd }); if (!res.ok) throw new Error(res.statusText); }
      catch (e) { app.ui.toast('Could not attach ' + (f.name || 'file') + ' to ' + key, { kind: 'err' }); }
    }
    paintFiles();
  }

  async function submit() {
    if (busy) return;
    const sums = summaries(summary.value);
    if (!sums.length) { err.textContent = 'Summary is required'; summary.focus(); return; }
    const missing = fields.filter(f => f.Required && widgets.get(f.ID).empty()).map(f => f.Name);
    if (missing.length) { err.textContent = 'Fill in ' + missing.join(', '); return; }
    const Fields = [];
    for (const f of fields) {
      const w = widgets.get(f.ID);
      if (w.touched() && !w.empty()) Fields.push({ ID: f.ID, Kind: f.Kind, Value: w.get() });
      else if (f.ID === 'parent' && !w.empty()) Fields.push({ ID: f.ID, Kind: f.Kind, Value: w.get() });
    }
    const parent = opts.parent && !fields.some(f => f.ID === 'parent') ? opts.parent : '';
    busy = true; okBtn.disabled = true; err.textContent = '';
    const made = [], warn = [];
    let failed = null;
    for (const [i, s] of sums.entries()) {
      okBtn.textContent = sums.length > 1 ? `Creating ${i + 1}/${sums.length}…` : 'Creating…';
      try {
        const r = await app.api.post('/issues', { Project: project, Type: type, Summary: s, Description: description.value, Parent: parent, Sprint: Number(sprintSel.value) || 0, Fields });
        made.push(r.Key);
        if (files.length) await attach(r.Key);
        if (r.Warning) warn.push(r.Warning);
        app.bus.emit('issue:changed', { key: r.Key, created: true });
      } catch (e) { failed = e; summary.value = sums.slice(i).join('\n'); break; }
    }
    busy = false; okBtn.disabled = false; okBtn.textContent = 'Create';
    if (made.length) {
      prefs.set('create.project', project); prefs.set('create.type.' + project, type);
      const first = made[0];
      app.ui.toast(made.length === 1 ? `Created ${first}` : `Created ${made.length} issues: ${made.join(', ')}`, { kind: 'ok', ms: 6000, action: { label: 'Open', run: () => app.panel.open(first) } });
      for (const w of warn) app.ui.toast(w, { kind: 'err' });
    }
    if (failed) {
      err.textContent = (made.length ? `${made.length} created, then: ` : '') + failed.message;
      return;
    }
    submitted = true; draft = null;
    if (another.checked) {
      summary.value = ''; description.value = ''; count(); summary.focus(); submitted = false;
      return;
    }
    m.close();
    if (made.length === 1) app.panel.open(made[0]);
  }
}
