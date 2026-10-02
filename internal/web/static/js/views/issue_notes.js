// Private notes on an issue (TUI: N). They live on the machine running laneway, beside the
// state file, where the TUI keeps them, so both see the same. mountNotes(parent, key, {app, el, full}) → cleanup.
import { h, clear } from '../lib/dom.js';
import { css } from '../lib/css.js';
import { render } from '../lib/md.js';

export function mountNotes(parent, key, { app, el, full }) {
  css('ask');
  const { api, ui } = app;
  const box = h('section.notes');
  parent.append(box);
  let text = '', dead = false, editing = false, open = false;
  const scope = app.keys.scope('issue-notes');
  const inPanel = () => full || app.panel.focused();
  scope.bind('N', () => edit(), 'edit your private notes', { group: 'Issue', when: () => inPanel() && !el.querySelector('.iss-find:not([hidden])') });

  async function save(v) {
    try { await api.put('/issues/' + key + '/notes', { Text: v }); } catch (e) { return ui.errToast(e); }
    text = v.trim(); editing = false; paint();
    app.bus.emit('notes:changed', { key, has: !!text });
    ui.toast(text ? 'Notes saved, on this machine only' : 'Notes removed');
  }
  function edit() {
    if (editing) return;
    editing = true; paint();
    const ta = box.querySelector('textarea'); ta && ta.focus();
  }
  function paint() {
    clear(box);
    box.hidden = false;
    if (!text && !editing) { box.append(h('button.btn.ghost.sm', { title: 'Private notes, on this machine (N)', onclick: edit }, '+ Private notes')); return; }
    if (editing) {
      const ta = h('textarea.input', { value: text, placeholder: 'Private notes in markdown. Only on this machine.', spellcheck: true });
      const done = () => save(ta.value), cancel = () => { editing = false; paint(); };
      const ed = h('div.ed', ta);
      ed._save = done; ed._cancel = cancel; // the panel's esc and ctrl+enter find these
      box.append(h('div.notes-head', h('b', 'Notes (local)'), h('span.dim', 'ctrl+enter saves, esc cancels')), ed,
        h('div.row.end', h('button.btn', { onclick: cancel }, 'Cancel'), h('button.btn.primary', { onclick: done }, 'Save')));
      return;
    }
    const long = text.split('\n').length > 4;
    const body = h('div.notes-body.md' + (long && !open ? '.folded' : ''), render(text));
    box.append(h('div.notes-head', h('b', 'Notes (local)'), h('span.spacer'),
      long && h('button.btn.ghost.sm', { onclick: () => { open = !open; paint(); } }, open ? 'Fold' : 'Unfold'),
      h('button.btn.ghost.sm', { title: 'Post as a comment', onclick: post }, 'Post as comment'),
      h('button.btn.ghost.sm', { title: 'Edit (N)', onclick: edit }, 'Edit')), body);
  }
  async function post() {
    if (!await ui.confirm({ title: 'Post notes', text: 'Post your private notes on ' + key + ' as a comment?', ok: 'Post' })) return;
    try { await api.post('/issues/' + key + '/comments', { Markdown: text }); app.bus.emit('issue:changed', { key }); ui.toast('Comment posted'); } catch (e) { ui.errToast(e); }
  }
  api.get('/issues/' + key + '/notes', { fresh: true }).then(r => { if (!dead) { text = (r.Text || '').trim(); paint(); } }).catch(() => {});
  return () => { dead = true; scope.dispose(); box.remove(); };
}
