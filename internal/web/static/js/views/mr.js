// A GitLab merge request's diff (TUI: d on one in the panel): the changed files beside it, every file's lines
// highlighted (GET /api/gitlab/diff?url=, parsed and highlighted by the server) with the inline threads under
// their lines. j/k or ]/[ next/previous file, n/N next/previous thread, z folds the file, Z all, o GitLab,
// r reload, esc back. #/mr?url=LINK, reached from an unfolded merge request (d, or its Diff button).
import { h, clear, delegate } from '../lib/dom.js';
import { css } from '../lib/css.js';
import { ago, isZero } from '../lib/fmt.js';
import { render as md } from '../lib/md.js';

const raw = s => { const t = document.createElement('template'); t.innerHTML = s; return t.content; };
const safe = u => (/^https?:\/\//i.test(u || '') ? u : '');

export default function mount(el, { app, scope, query, toolbar, context }) {
  css('diff'); css('dev');
  const { api, ui } = app;
  const url = query.url || '';
  let data = null, dead = false, cur = 0, version = +query.version || 0;
  const folded = new Set();
  const whole = new Map(); // e: file index → that file with its unchanged lines filled in
  const files = h('nav.df-files', { 'aria-label': 'Changed files' }), body = h('div.df-body');
  el.append(h('div.df', files, body));
  const title = h('span.clip');
  context.append(title);

  function threadsAt(path) {
    const m = new Map();
    for (const t of (data.Threads || [])) if (t.Path === path) {
      const k = t.NewLine ? 'n' + t.NewLine : 'o' + t.OldLine;
      if (!m.has(k)) m.set(k, []);
      m.get(k).push(t);
    }
    return m;
  }
  const note = t => h('div.df-note' + (t.Resolved ? '.resolved' : ''), { dataset: { thread: t.ID } },
    (t.Notes || []).map(n => h('div', h('span.df-who', n.Author), isZero(n.Created) ? null : h('span.df-when', ago(n.Created)), h('div.md', md(n.Body)))),
    t.Resolved ? h('div.df-when', 'resolved') : null);

  function section(f, i) {
    const at = threadsAt(f.Path), n = [...at.values()].reduce((s, l) => s + l.length, 0);
    const head = h('div.df-head', { dataset: { file: i } }, h('b.clip', f.Path), f.Renamed && f.OldPath !== f.Path ? h('span.df-dim', '← ' + f.OldPath) : null,
      f.New ? h('span.df-dim', 'new') : null, f.Deleted ? h('span.df-dim', 'deleted') : null, f.Generated ? h('span.df-dim', 'generated') : null,
      h('span.spacer'), n ? h('span.df-threads', n + (n === 1 ? ' thread' : ' threads')) : null, h('span.df-add', '+' + f.Add), h('span.df-del', '−' + f.Del),
      f.New || f.Deleted || f.Binary || f.TooLarge ? null : h('button.btn.ghost.sm', { title: 'e', onclick: e => { e.stopPropagation(); expand(i); } }, whole.has(i) ? 'Changes only' : 'Whole file'));
    const sec = h('section.df-sec' + (folded.has(i) ? '.folded' : ''), { id: 'df-' + i }, head);
    if (f.Binary) { sec.append(h('div.df-msg', 'binary file, not shown')); return sec; }
    if (f.TooLarge) { sec.append(h('div.df-msg', 'too large for GitLab to send: ', h('a', { href: safe(data.WebURL) + '/diffs', target: '_blank', rel: 'noopener noreferrer' }, 'open it in GitLab'))); return sec; }
    const rows = h('div.df-rows');
    for (const l of f.Lines || []) {
      const cls = l.K === '+' ? '.add' : l.K === '-' ? '.del' : l.K === '@' ? '.hunk' : l.K === '\\' ? '.meta' : '';
      const code = h('span.df-code'); code.append(raw(l.H || ''));
      rows.append(h('div.df-row' + cls, h('span.df-no', l.O || ''), h('span.df-no', l.N || ''), h('span.df-mk', l.K === '+' || l.K === '-' ? l.K : ' '), code));
      const ts = (l.N && at.get('n' + l.N)) || (l.K === '-' && l.O && at.get('o' + l.O));
      if (ts && l.K !== '@') { for (const t of ts) rows.append(note(t)); at.delete(l.N ? 'n' + l.N : 'o' + l.O); }
    }
    sec.append(rows);
    return sec;
  }
  function paint() {
    if (!data) return;
    title.textContent = data.Label + (data.Title ? ' · ' + data.Title : '');
    files.replaceChildren(...(data.Files || []).map((f, i) => h('div.df-file-link' + (i === cur ? '.cur' : ''), { dataset: { file: i }, title: f.Path },
      h('span.df-path', f.Path), h('span.df-add', '+' + f.Add), h('span.df-del', '−' + f.Del))));
    const secs = (data.Files || []).map((f, i) => section(whole.get(i) || f, i));
    if (data.Truncated) secs.push(h('div.df-msg', 'GitLab truncated this diff: the rest is only on ', h('a', { href: safe(data.WebURL) + '/diffs', target: '_blank', rel: 'noopener noreferrer' }, 'GitLab')));
    if (!secs.length) secs.push(h('div.df-msg', 'This merge request has no diff.'));
    body.replaceChildren(...secs);
  }
  async function load(fresh) {
    body.replaceChildren(h('div.df-msg', 'Loading the diff…'));
    try {
      data = await api.get('/gitlab/diff?url=' + encodeURIComponent(url) + (version ? '&version=' + version : '') + (fresh ? '&fresh=1' : ''), { fresh: true });
      whole.clear();
      if (!dead) { paint(); versionPick(); }
    } catch (e) { if (!dead) body.replaceChildren(h('div.df-msg', e.message, ' ', safe(url) ? h('a', { href: safe(url), target: '_blank', rel: 'noopener noreferrer' }, 'Open in GitLab') : null)); }
  }
  function go(i) {
    const n = (data && data.Files || []).length; if (!n) return;
    cur = Math.max(0, Math.min(n - 1, i));
    for (const l of files.children) l.classList.toggle('cur', +l.dataset.file === cur);
    const s = body.querySelector('#df-' + cur); if (s) s.scrollIntoView({ block: 'start' });
  }
  async function expand(i) {
    const f = data && data.Files && data.Files[i]; if (!f) return;
    if (whole.has(i)) whole.delete(i);
    else {
      if (f.New || f.Deleted || f.Binary || f.TooLarge) return ui.toast(f.Path + ': nothing more to show');
      try { whole.set(i, await api.get('/gitlab/diff/file?url=' + encodeURIComponent(url) + '&path=' + encodeURIComponent(f.Path) + (version ? '&version=' + version : ''))); }
      catch (e) { return ui.errToast(e); }
      if (dead) return;
    }
    const old = body.querySelector('#df-' + i); if (old) old.replaceWith(section(whole.get(i) || f, i));
  }
  function fold(i, v) { if (v ?? !folded.has(i)) folded.add(i); else folded.delete(i); const s = body.querySelector('#df-' + i); if (s) s.classList.toggle('folded', folded.has(i)); }
  function thread(d) {
    const ns = [...body.querySelectorAll('.df-note')]; if (!ns.length) return ui.toast('No inline threads');
    const top = body.getBoundingClientRect().top + 8;
    const next = d > 0 ? ns.find(n => n.getBoundingClientRect().top > top + 1) : ns.reverse().find(n => n.getBoundingClientRect().top < top - 1);
    if (next) next.scrollIntoView({ block: 'start' }); else ui.toast(d > 0 ? 'No further threads' : 'No earlier threads');
  }
  const G = 'Diff';
  scope.bind(['j', ']'], () => go(cur + 1), 'next file', { group: G });
  scope.bind(['k', '['], () => go(cur - 1), 'previous file', { group: G });
  scope.bind('n', () => thread(1), 'next inline thread', { group: G });
  scope.bind('N', () => thread(-1), 'previous inline thread', { group: G });
  scope.bind('z', () => fold(cur), 'fold the file', { group: G });
  scope.bind('e', () => expand(cur), 'the whole file / the changes only', { group: G });
  scope.bind('Z', () => { const all = (data && data.Files || []).every((_, i) => folded.has(i)); (data && data.Files || []).forEach((_, i) => fold(i, !all)); }, 'fold / unfold every file', { group: G });
  scope.bind('o', () => safe(url) && window.open(url, '_blank', 'noopener'), 'open in GitLab', { group: G });
  scope.bind('r', () => load(true), 'reload', { group: G });
  scope.bind('Escape', () => (history.length > 1 ? history.back() : app.go('/mrs')), 'back', { group: G });
  delegate(files, 'click', '.df-file-link', (e, t) => go(+t.dataset.file));
  delegate(body, 'click', '.df-head', (e, t) => fold(+t.dataset.file));
  // v: the versions, newest first; an older one shows as it was pushed, without the threads.
  const picker = h('select.input.sm', { title: 'Version (v)', hidden: true, onchange: () => pickVersion(+picker.value) });
  function versionPick() {
    const vs = data.Versions || [];
    picker.hidden = vs.length < 2;
    picker.replaceChildren(...vs.map((v, i) => h('option', { value: i ? v.ID : 0, selected: (i ? v.ID : 0) === version },
      'version ' + (vs.length - i) + (i ? '' : ' (newest)') + (isZero(v.Created) ? '' : ' · ' + ago(v.Created)) + ' · ' + (v.HeadSHA || '').slice(0, 8))));
  }
  function pickVersion(v) {
    version = v;
    history.replaceState(null, '', '#/mr?url=' + encodeURIComponent(url) + (v ? '&version=' + v : ''));
    load(false);
  }
  scope.bind('v', () => { if (picker.hidden) return ui.toast('One version: nothing pushed since it opened'); picker.focus(); picker.showPicker && picker.showPicker(); }, 'pick a version', { group: G });
  clear(toolbar).append(h('span.spacer'), picker, h('button.btn.ghost.sm', { title: 'o', onclick: () => safe(url) && window.open(url, '_blank', 'noopener') }, 'GitLab'), h('button.btn.ghost.sm', { title: 'r', onclick: () => load(true) }, 'Reload'));
  load(false);
  return () => { dead = true; };
}
