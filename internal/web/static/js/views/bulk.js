// Bulk edit: openBulk(app, keys) asks what to change, then applies it to every key a few
// at a time with progress, and lists the ones that failed.
import { h } from '../lib/dom.js';
import { css } from '../lib/css.js';
import { pickUser, pickLabels, bulkUndo, undo } from './fields.js';

css('forms');

const ACTIONS = [
  { id: 'status', label: 'Status' },
  { id: 'assignee', label: 'Assignee' },
  { id: 'priority', label: 'Priority' },
  { id: 'labels+', label: 'Add labels' },
  { id: 'labels-', label: 'Remove labels' },
  { id: 'sprint', label: 'Sprint / backlog' },
  { id: 'flag+', label: 'Flag' },
  { id: 'flag-', label: 'Remove flag' },
  { id: 'points', label: 'Story points' },
];

// What to change: {edit, what}, or null when cancelled.
async function ask(app, id, keys, project) {
  const n = keys.length;
  switch (id) {
    case 'status': {
      const ts = await app.api.get(`/projects/${project}/statuses`);
      const names = [...new Set(ts.flatMap(t => (t.Statuses || []).map(s => s.Name)))];
      const s = await app.ui.pick({ title: `Move ${n} issues to`, items: names, label: x => x });
      return s && { edit: { Field: 'status', To: s }, what: `${n} moved to ${s}` };
    }
    case 'assignee': {
      const u = await pickUser(app, { project, title: `Assign ${n} issues`, specials: true });
      return u && { edit: { Field: 'assignee', ID: u.AccountID }, what: u.none ? `${n} unassigned` : `${n} assigned to ${u.DisplayName}` };
    }
    case 'priority': {
      const list = await app.api.get('/priorities');
      const p = await app.ui.pick({ title: `Priority of ${n} issues`, items: list, label: x => x.Name });
      return p && { edit: { Field: 'priority', ID: p.ID }, what: `${n} priority → ${p.Name}` };
    }
    case 'labels+': {
      const ls = await pickLabels(app, project, [], `Add labels to ${n} issues`);
      return ls && ls.length ? { edit: { Field: 'labels', Add: ls }, what: `${n} +${ls.join(' +')}` } : null;
    }
    case 'labels-': {
      const issues = await Promise.all(keys.slice(0, 100).map(k => app.api.get('/issues/' + k).catch(() => null)));
      const have = [...new Set(issues.flatMap(i => (i && i.Labels) || []))];
      if (!have.length) { app.ui.toast('None of them has labels'); return null; }
      const ls = await app.ui.pick({ title: `Remove labels from ${n} issues`, items: have, multi: true, label: x => x });
      return ls && ls.length ? { edit: { Field: 'labels', Remove: ls }, what: `${n} -${ls.join(' -')}` } : null;
    }
    case 'sprint': {
      const s = await app.api.get(`/projects/${project}/sprints`);
      const o = await app.ui.pick({ title: `Move ${n} issues to`, items: [{ ID: 0, Name: 'Backlog' }, ...s.Sprints], label: x => x.Name, detail: x => x.State || '' });
      return o && { edit: { Field: 'sprint', Sprint: o.ID }, what: `${n} → ${o.Name}` };
    }
    case 'flag+': return { edit: { Field: 'flag', On: true }, what: `${n} flagged` };
    case 'flag-': return { edit: { Field: 'flag', On: false }, what: `${n} unflagged` };
    case 'points': {
      const v = await app.ui.prompt({ title: `Story points for ${n} issues (empty clears)` });
      if (v === null) return null;
      if (v.trim() && isNaN(Number(v))) { app.ui.toast('Story points must be a number', { kind: 'err' }); return null; }
      return { edit: { Field: 'points', Text: v.trim() }, what: `${n} points → ${v.trim() || 'none'}` };
    }
  }
  return null;
}

export async function openBulk(app, keys) {
  keys = [...new Set(keys || [])];
  if (!keys.length) return app.ui.toast('Mark issues with x first');
  const project = keys[0].split('-')[0];
  try {
    const a = await app.ui.pick({ title: `Edit ${keys.length} issue${keys.length === 1 ? '' : 's'}: ${keys.slice(0, 4).join(', ')}${keys.length > 4 ? '…' : ''}`, items: ACTIONS, label: x => x.label });
    if (!a) return;
    const q = await ask(app, a.id, keys, project);
    if (!q) return;
    return await run(app, keys, q);
  } catch (e) { app.ui.errToast(e); }
}

const CHUNK = 4;

async function run(app, keys, { edit, what }) {
  let stop = false;
  const bar = h('progress', { max: keys.length, value: 0 });
  const label = h('div.dim', `0 / ${keys.length}`);
  const list = h('ul.bulk-failed');
  const closeBtn = h('button.btn', { type: 'button', onclick: () => m.close() }, 'Stop');
  const m = app.ui.modal(h('div.bulk', bar, label, list, h('div.row.end', closeBtn)), { title: what, onClose: () => { stop = true; } });
  const res = { Done: [], Undo: {}, Failed: {} };
  let n = 0;
  for (let i = 0; i < keys.length && !stop; i += CHUNK) {
    const chunk = keys.slice(i, i + CHUNK);
    try {
      const r = await app.api.post('/bulk', { Keys: chunk, Edit: edit });
      res.Done.push(...r.Done); Object.assign(res.Undo, r.Undo); Object.assign(res.Failed, r.Failed);
      for (const k of r.Done) app.bus.emit('issue:changed', { key: k, what: `${k}: ${what}` });
    } catch (e) { for (const k of chunk) res.Failed[k] = e.message; }
    n += chunk.length;
    bar.value = n; label.textContent = `${n} / ${keys.length}`;
    list.replaceChildren(...Object.entries(res.Failed).map(([k, msg]) => h('li', h('b', k), ' ', msg)));
  }
  const failed = Object.keys(res.Failed);
  const step = bulkUndo(app, what, res);
  if (!failed.length) {
    m.close();
    app.ui.toast(`${what}`, { kind: 'ok', action: step && { label: 'Undo', run: () => undo(app, step) } });
  } else if (!stop) {
    label.textContent = `${res.Done.length} done, ${failed.length} failed`;
    closeBtn.textContent = 'Close'; closeBtn.focus();
  }
  return res;
}
