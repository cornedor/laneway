// The issue actions menu (TUI: A). issueActions(app, st, hooks) opens a picker of what else can be done with
// the issue; st is the panel's state ({issue, card}), hooks {key, changed(), open(key), upload(files), reloadExtras()}.
import { h } from '../lib/dom.js';

const enc = encodeURIComponent;

export async function issueActions(app, st, hk, only) {
  const { api, ui } = app, key = hk.key, iss = st.issue;
  if (!iss) return;
  const project = key.slice(0, key.lastIndexOf('-'));
  const epic = /^epic$/i.test(iss.Type);
  const flagged = !!(st.card && st.card.Flagged);
  const items = [
    { id: epic ? 'child' : 'subtask', label: epic ? 'New issue in this epic' : 'New subtask' },
    { id: 'link', label: 'Link to another issue' },
    { id: 'weblink', label: 'Add a web link' },
    { id: 'clone', label: 'Clone' },
    { id: 'type', label: 'Change the issue type' },
    { id: 'move', label: 'Move to another project' },
    { id: 'delete', label: 'Delete the issue' },
    { id: 'watch', label: 'Watch / stop watching' },
    { id: 'watchers', label: 'Add or remove watchers' },
    { id: 'vote', label: 'Vote / take back the vote' },
    { id: 'flag', label: flagged ? 'Clear the flag' : 'Flag as an impediment' },
    { id: 'upload', label: 'Upload files' },
    { id: 'unlink', label: 'Remove a link', skip: !(iss.Links || []).some(l => l.LinkID) },
    { id: 'delatt', label: 'Delete an attachment', skip: !(iss.Attachments || []).length },
    { id: 'history', label: 'Time in each status / history' },
  ].filter(x => !x.skip);
  const it = only ? { id: only } : await ui.pick({ title: 'Actions on ' + key, items, label: x => x.label, placeholder: 'Action…' });
  if (!it) return;
  try { await run(it.id); } catch (e) { ui.errToast(e); }

  async function run(id) {
    switch (id) {
      case 'subtask': case 'child': return app.actions.create({ project, parent: key });
      case 'link': return link();
      case 'weblink': {
        const url = await ui.prompt({ title: 'Link a web page', placeholder: 'https://…', ok: 'Add' });
        if (!url || !url.trim()) return;
        await api.post('/issues/' + key + '/weblinks', { URL: url.trim(), Title: '' });
        return hk.reloadExtras();
      }
      case 'clone': {
        const r = await api.post('/issues/' + key + '/clone');
        app.bus.emit('issue:changed', { key: r.Key, created: true });
        return ui.toast('Cloned as ' + r.Key, { kind: 'ok', ms: 6000, action: { label: 'Open', run: () => hk.open(r.Key) } });
      }
      case 'type': {
        const ts = await api.get('/issues/' + key + '/types?current=' + enc(iss.Type), { fresh: true });
        const t = await ui.pick({ title: key + ' is a ' + iss.Type + ': change to', items: ts, label: x => x.Name });
        if (!t) return;
        await api.post('/issues/' + key + '/type', { ID: t.ID });
        hk.changed(); return ui.toast(key + ' is now a ' + t.Name, { kind: 'ok' });
      }
      case 'move': return move();
      case 'delete': {
        const n = (iss.Links || []).filter(l => l.Rel === 'subtask').length;
        const ok = await ui.confirm({ title: 'Delete ' + key, text: 'Delete ' + key + ' ' + iss.Summary + (n ? ' and its ' + (n === 1 ? 'subtask' : n + ' subtasks') : '') + '? This cannot be undone.', ok: 'Delete', danger: true });
        if (!ok) return;
        await api.del('/issues/' + key + (n ? '?subtasks=1' : ''));
        app.bus.emit('issue:changed', { key, deleted: true });
        ui.toast('Deleted ' + key, { kind: 'ok' });
        return app.panel.close();
      }
      case 'watch': {
        const r = await api.post('/issues/' + key + '/watch');
        return ui.toast(r.On ? 'Watching ' + key : 'Stopped watching ' + key, { kind: 'ok' });
      }
      case 'watchers': return watchers();
      case 'vote': {
        const r = await api.post('/issues/' + key + '/vote');
        return ui.toast(r.On ? 'Voted for ' + key : 'Vote taken back', { kind: 'ok' });
      }
      case 'flag': await api.put('/issues/' + key + '/field/flag', { On: !flagged }); return hk.changed();
      case 'upload': {
        const inp = h('input', { type: 'file', multiple: true, onchange: () => { if (inp.files.length) hk.upload([...inp.files]); } });
        return inp.click();
      }
      case 'unlink': {
        const l = await ui.pick({ title: 'Remove a link from ' + key, items: (iss.Links || []).filter(x => x.LinkID), label: x => x.Rel + ' ' + x.Key + ' ' + x.Summary });
        if (!l) return;
        await api.del('/issues/' + key + '/links/' + enc(l.LinkID)); return hk.changed();
      }
      case 'delatt': {
        const a = await ui.pick({ title: 'Delete an attachment', items: iss.Attachments, label: x => x.Filename });
        if (!a || !await ui.confirm({ title: 'Delete attachment', text: a.Filename, ok: 'Delete', danger: true })) return;
        await api.del('/issues/' + key + '/attachments/' + enc(a.ID)); return hk.changed();
      }
      case 'history': return hk.tab('history');
    }
  }

  async function link() {
    const types = await api.get('/linktypes');
    const opts = types.flatMap(t => [{ label: t.Outward, Type: t.Name, Outward: true }, ...(t.Inward !== t.Outward ? [{ label: t.Inward, Type: t.Name, Outward: false }] : [])]);
    const t = await ui.pick({ title: key + ' …', items: opts, label: o => o.label, placeholder: 'Link type…' });
    if (!t) return;
    const other = await ui.pick({ title: key + ' ' + t.label + ' …', items: [], label: c => c.Key + ' ' + c.Summary, placeholder: 'Find an issue or type its key…', empty: 'Type to search',
      create: q => ({ Key: q.trim().toUpperCase(), Summary: '' }),
      search: async q => (q.trim().length > 1 ? (await api.get('/find?q=' + enc(q.trim()))).cards.filter(c => c.Key !== key) : []) });
    if (!other) return;
    await api.post('/issues/' + key + '/links', { Type: t.Type, Other: other.Key, Outward: t.Outward });
    hk.changed();
  }

  async function move() {
    const ps = (await api.get('/projects/creatable')).filter(p => p.Key !== project);
    const p = await ui.pick({ title: 'Move ' + key + ' to', items: ps, label: x => x.Key + '  ' + x.Name });
    if (!p) return;
    const ts = await api.get('/issues/' + key + '/movetypes?project=' + enc(p.Key) + '&current=' + enc(iss.Type), { fresh: true });
    const t = ts.find(x => x.Name.toLowerCase() === iss.Type.toLowerCase()) && ts.length === 1 ? ts[0] : await ui.pick({ title: 'Move ' + key + ' to ' + p.Key + ' as', items: ts, label: x => x.Name });
    if (!t) return;
    ui.toast('Moving ' + key + '…');
    const r = await api.post('/issues/' + key + '/move', { Project: p.Key, TypeID: t.ID });
    app.bus.emit('issue:changed', { key, moved: r.Key });
    ui.toast(key + ' is now ' + r.Key, { kind: 'ok', ms: 6000 });
    hk.open(r.Key);
  }

  async function watchers() {
    for (;;) {
      const ws = await api.get('/issues/' + key + '/watchers', { fresh: true });
      const on = new Set(ws.map(u => u.AccountID));
      const u = await ui.pick({ title: 'Watchers of ' + key + ' (enter adds or removes)', items: ws, label: x => (on.has(x.AccountID) ? '✓ ' : '   ') + x.DisplayName,
        placeholder: 'Find a person…', empty: 'No watchers',
        search: async q => {
          const others = await api.get('/issues/' + key + '/viewusers?q=' + enc(q), { fresh: true });
          const names = q.toLowerCase();
          return [...ws.filter(x => x.DisplayName.toLowerCase().includes(names)), ...others.filter(x => !on.has(x.AccountID))];
        } });
      if (!u) return;
      await api.put('/issues/' + key + '/watchers/' + enc(u.AccountID), { Watch: !on.has(u.AccountID) });
      ui.toast((on.has(u.AccountID) ? 'Removed ' : 'Added ') + u.DisplayName, { kind: 'ok' });
    }
  }
}
