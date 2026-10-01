// Markdown editor: a textarea with a formatting toolbar, a `/` menu for Jira
// formatting, `@` mentions, `:` emoji, ctrl+b/i/k, ctrl+p live preview and
// pasted/dropped files.
//
//   const e = mdEdit(app, {value, rows, placeholder, mono, issueKey, project, label, save(text, mentions), cancel,
//                          allowEmpty, noCancel, people() → Map(name → accountId), mdOpts() → render options, onFiles(files), hint,
//                          draft: an id ("comment:KEY", "desc:KEY", "desc:KEY:comment:ID") kept in the state file as the TUI's drafts})
//   e.el (with ._save ._cancel ._escape for the Escape/ctrl+Enter bindings), e.ta, e.mentions, e.size(), e.focus()
import { h, clear, debounce } from './dom.js';
import { css } from './css.js';
import { render as md } from './md.js';

css('mdedit');

const today = () => new Date().toISOString().slice(0, 10);
const PANELS = ['info', 'note', 'success', 'warning', 'error'];

// Slash commands: apply(ctl) runs with the typed "/query" already removed.
const SLASH = [
  { name: 'h1', label: 'Heading 1', run: c => c.line('# ') },
  { name: 'h2', label: 'Heading 2', run: c => c.line('## ') },
  { name: 'h3', label: 'Heading 3', run: c => c.line('### ') },
  { name: 'bullet', label: 'Bulleted list', run: c => c.line('- ') },
  { name: 'numbered', label: 'Numbered list', run: c => c.line('1. ') },
  { name: 'task', label: 'Task list', run: c => c.line('- [ ] ') },
  { name: 'code', label: 'Code block', run: c => c.block('```\n', '\n```', '') },
  { name: 'quote', label: 'Quote', run: c => c.line('> ') },
  { name: 'rule', label: 'Horizontal rule', run: c => c.insert('---\n\n') },
  { name: 'table', label: 'Table', run: c => c.insert('| Header | Header |\n| --- | --- |\n| Cell | Cell |\n', 2, 8) },
  ...PANELS.map(p => ({ name: p, label: 'Panel: ' + p, run: c => c.block('<!-- panel:' + p + ' -->\n\n', '\n\n<!-- /panel -->', 'Text') })),
  { name: 'expand', label: 'Expand (collapsible)', run: c => c.block('<!-- expand: Title -->\n\n', '\n\n<!-- /expand -->', 'Text') },
  { name: 'mention', label: 'Mention someone', run: c => c.insert('@') },
  { name: 'issue', label: 'Issue reference', run: c => c.issueRef() },
  { name: 'emoji', label: 'Emoji', run: c => c.insert(':') },
  { name: 'link', label: 'Link', run: c => c.link() },
  { name: 'date', label: 'Today’s date', run: c => c.insert('<date>' + today() + '</date> ') },
  { name: 'decision', label: 'Decision', run: c => c.line('<> ') },
  ...[['grey', 'neutral'], ['purple', 'purple'], ['blue', 'blue'], ['red', 'red'], ['yellow', 'yellow'], ['green', 'green']]
    .map(([label, color]) => ({ name: 'status' + label, label: 'Status: ' + label, run: c => c.wrap('<status color="' + color + '">', '</status>', 'DONE') })),
  { name: 'underline', label: 'Underline', run: c => c.wrap('<u>', '</u>', 'text') },
  ...[['blue', '#0747a6'], ['teal', '#008da6'], ['green', '#006644'], ['orange', '#ff991f'], ['red', '#bf2600'], ['purple', '#403294'], ['grey', '#97a0af']]
    .map(([label, hex]) => ({ name: 'colour' + label, label: 'Text colour: ' + label, run: c => c.wrap('<span style="color:' + hex + '">', '</span>', 'text') })),
];

export function mdEdit(app, o) {
  const { api, ui } = app;
  const mentions = [];
  const glyphs = new Map(); // emoji taken here → glyph, for the preview
  const ta = h('textarea.input.ed-ta' + (o.mono ? '.mono' : ''), { rows: o.rows || 4, placeholder: o.placeholder || '', spellcheck: true });
  ta.value = o.value || '';
  const pop = h('div.mention-pop', { hidden: true, role: 'listbox' });
  const preview = h('div.md.ed-preview', { hidden: true });
  const fileIn = h('input', { type: 'file', multiple: true, hidden: true, onchange: () => { if (fileIn.files.length) files([...fileIn.files]); fileIn.value = ''; } });
  const go = h('button.btn.primary', { onclick: () => run() }, o.label || 'Save');

  // ---- text operations (execCommand keeps the browser's undo)
  const replace = (from, to, text, s0, s1) => {
    ta.focus({ preventScroll: true });
    ta.setSelectionRange(from, to);
    if (!document.execCommand('insertText', false, text)) { ta.setRangeText(text, from, to, 'end'); ta.dispatchEvent(new Event('input')); }
    if (s0 != null) ta.setSelectionRange(from + s0, from + (s1 == null ? s0 : s1));
  };
  const sel = () => [ta.selectionStart, ta.selectionEnd];
  const lineStart = i => ta.value.lastIndexOf('\n', i - 1) + 1;
  const ctl = {
    insert(text, s0, s1) { const [a, b] = sel(); replace(a, b, text, s0 != null ? s0 : text.length, s1); },
    // Prefix the current line(s); a line already carrying it loses it.
    line(prefix) {
      const [a, b] = sel(), v = ta.value, from = lineStart(a);
      let end = v.indexOf('\n', b > a ? b - 1 : b); if (end < 0) end = v.length;
      const lines = v.slice(from, end).split('\n');
      const all = lines.every(l => l.startsWith(prefix));
      const out = lines.map(l => (all ? l.slice(prefix.length) : prefix + l.replace(/^(#{1,6} |[-*] (\[[ x]\] )?|\d+\. |> )/, ''))).join('\n');
      replace(from, end, out, out.length);
    },
    wrap(pre, post, ph) {
      const [a, b] = sel(), t = ta.value.slice(a, b);
      if (t.startsWith(pre) && t.endsWith(post) && t.length >= pre.length + post.length) return replace(a, b, t.slice(pre.length, t.length - post.length), t.length - pre.length - post.length);
      const inner = t || ph;
      replace(a, b, pre + inner + post, pre.length, pre.length + inner.length);
    },
    block(pre, post, ph) {
      const [a, b] = sel(), t = ta.value.slice(a, b) || ph;
      const lead = a > 0 && ta.value[a - 1] !== '\n' ? '\n' : '';
      replace(a, b, lead + pre + t + post + '\n', lead.length + pre.length, lead.length + pre.length + t.length);
    },
    link() {
      const [a, b] = sel(), t = ta.value.slice(a, b);
      if (/^https?:\/\//.test(t)) return replace(a, b, '[text](' + t + ')', 1, 5);
      replace(a, b, '[' + (t || 'text') + '](https://)', t ? t.length + 3 : 7, t ? t.length + 11 : 7 + 8);
    },
    async issueRef() {
      const c = await ui.pick({ title: 'Issue', items: [], label: x => x.Key + ' ' + x.Summary, placeholder: 'Find an issue…', empty: 'Type to search',
        search: async q => (q.length > 1 ? (await api.get('/find?q=' + encodeURIComponent(q))).cards : []) });
      ta.focus();
      if (c) ctl.insert(c.Key + ' ');
    },
  };

  // ---- popup: mention, emoji, slash
  let items = [], pick = 0, from = -1, kind = '';
  const closePop = () => { pop.hidden = true; from = -1; kind = ''; items = []; };
  const paint = () => {
    clear(pop).append(...items.map((it, i) => h('div.mp' + (i === pick ? '.sel' : ''), { role: 'option', onmousedown: e => { e.preventDefault(); accept(i); } },
      kind === 'mention' ? [ui.avatar(it.DisplayName, null, 18), it.DisplayName]
        : kind === 'emoji' ? [h('span.mp-g', it.Glyph), ':' + it.Name + ':']
          : [h('b', '/' + it.name), h('span.dim', ' ' + it.label)])));
    pop.hidden = !items.length;
    const r = ta.getBoundingClientRect(), room = r.top > 260;
    Object.assign(pop.style, { position: 'fixed', left: Math.max(4, Math.min(r.left, window.innerWidth - 260)) + 'px', right: 'auto',
      top: room ? 'auto' : r.bottom + 4 + 'px', bottom: room ? window.innerHeight - r.top + 4 + 'px' : 'auto' });
    const s = pop.querySelector('.sel'); if (s) s.scrollIntoView({ block: 'nearest' });
  };
  function accept(i) {
    const it = items[i]; if (!it) return;
    const end = ta.selectionStart, at = from, k = kind;
    closePop();
    if (k === 'mention') {
      const name = '@' + it.DisplayName + ' ';
      replace(at, end, name, name.length);
      if (it.AccountID && !mentions.some(m => m.AccountID === it.AccountID)) mentions.push({ AccountID: it.AccountID, DisplayName: it.DisplayName });
    } else if (k === 'emoji') {
      const code = ':' + it.Name + ': ';
      replace(at, end, code, code.length);
      glyphs.set(it.Name, it.Glyph);
      api.post('/emoji/used', { Name: it.Name }).catch(() => {});
    } else {
      replace(at, end, '', 0);
      it.run(ctl);
    }
  }
  const people = () => (o.people ? [...o.people()] : []);
  const users = debounce(async (q, at) => {
    const local = people().filter(([n]) => n.toLowerCase().includes(q.toLowerCase())).map(([n, id]) => ({ DisplayName: n, AccountID: id }));
    let remote = [];
    try {
      const r = await api.get('/users?' + (o.issueKey ? 'issue=' + encodeURIComponent(o.issueKey) : 'project=' + encodeURIComponent((typeof o.project === 'function' ? o.project() : o.project) || '')) + '&q=' + encodeURIComponent(q));
      remote = (Array.isArray(r) ? r : []).map(u => ({ DisplayName: u.DisplayName, AccountID: u.AccountID }));
    } catch (e) { /* local names only */ }
    if (from !== at || kind !== 'mention') return;
    const seen = new Set();
    items = [...remote, ...local].filter(u => u.DisplayName && u.AccountID && !seen.has(u.AccountID) && seen.add(u.AccountID)).slice(0, 8);
    pick = 0; paint();
  }, 120);
  const emojis = debounce(async (q, at) => {
    let r = [];
    try { r = await api.get('/emoji?q=' + encodeURIComponent(q)); } catch (e) { /* none */ }
    if (from !== at || kind !== 'emoji') return;
    items = r; pick = 0; paint();
  }, 80);
  function trigger() {
    const before = ta.value.slice(0, ta.selectionStart);
    let m = /(?:^|[\s(])@([^\s@]{0,30})$/u.exec(before);
    if (m) { from = before.length - m[1].length - 1; kind = 'mention'; return users(m[1], from); }
    m = /(?:^|\s):([a-z0-9_+-]{2,})$/i.exec(before);
    if (m) { from = before.length - m[1].length - 1; kind = 'emoji'; return emojis(m[1].toLowerCase(), from); }
    m = /(?:^|\n|\s)\/([a-z0-9]{0,10})$/i.exec(before);
    if (m && !inFence(before)) {
      const q = m[1].toLowerCase();
      from = before.length - q.length - 1; kind = 'slash';
      items = SLASH.filter(s => s.name.startsWith(q) || (q.length > 1 && s.label.toLowerCase().includes(q))).slice(0, 8);
      pick = 0; return paint();
    }
    if (kind) closePop();
  }
  const inFence = t => (t.match(/^```/gm) || []).length % 2 === 1;

  // ---- preview
  const renderPreview = () => {
    if (preview.hidden) return;
    let text = ta.value;
    for (const [n, g] of glyphs) text = text.split(':' + n + ':').join(g);
    clear(preview).append(text.trim() ? md(text, o.mdOpts ? o.mdOpts() : {}) : h('p.faint', 'Nothing to preview.'));
  };
  const livePreview = debounce(renderPreview, 120);
  const togglePreview = () => { preview.hidden = !preview.hidden; bPrev.classList.toggle('on', !preview.hidden); renderPreview(); ta.focus({ preventScroll: true }); };

  // ---- files: onFiles uploads them and answers each one's attachment ({ID, Filename}, or null).
  // On an issue an image goes in the text where it was pasted: a line while it uploads, then ![name](attachment:ID).
  let uploads = 0;
  async function files(list) {
    if (!o.onFiles) return;
    const marks = list.map(f => o.issueKey && f.type.startsWith('image/') ? '![Uploading ' + (f.name || 'image') + ' ' + (++uploads) + '…]()' : '');
    const shown = marks.filter(Boolean);
    if (shown.length) {
      const [a] = sel();
      ctl.insert((a && ta.value[a - 1] !== '\n' ? '\n' : '') + shown.join('\n') + '\n');
    }
    const made = (await o.onFiles(list)) || [];
    marks.forEach((m, i) => {
      if (!m) return;
      const at = ta.value.indexOf(m);
      if (at < 0) return;
      const att = made[i];
      if (att && att.ID) ta.setRangeText('![' + att.Filename.replace(/[[\]\n]/g, ' ') + '](attachment:' + att.ID + ')', at, at + m.length, 'preserve');
      else ta.setRangeText('', at, at + m.length + (ta.value[at + m.length] === '\n' ? 1 : 0), 'preserve');
    });
    if (shown.length) ta.dispatchEvent(new Event('input'));
  }

  // ---- toolbar
  const btn = (label, title, fn, cls = '') => h('button.tb' + cls, { type: 'button', title, tabindex: -1, onmousedown: e => e.preventDefault(), onclick: fn }, label);
  const bPrev = btn('Preview', 'Preview (ctrl+p)', togglePreview, '.txt');
  const toolbar = h('div.ed-tools', { role: 'toolbar' },
    btn('B', 'Bold (ctrl+b)', () => ctl.wrap('**', '**', 'bold'), '.b'), btn('I', 'Italic (ctrl+i)', () => ctl.wrap('*', '*', 'italic'), '.i'),
    btn('S', 'Strikethrough', () => ctl.wrap('~~', '~~', 'text'), '.s'), btn('</>', 'Inline code', () => ctl.wrap('`', '`', 'code')),
    btn('Link', 'Link (ctrl+k)', () => ctl.link(), '.txt'), h('i.sep'),
    btn('H', 'Heading (/h2)', () => ctl.line('## ')), btn('•', 'Bulleted list', () => ctl.line('- ')), btn('1.', 'Numbered list', () => ctl.line('1. ')),
    btn('☐', 'Task list', () => ctl.line('- [ ] ')), btn('❝', 'Quote', () => ctl.line('> ')), btn('{ }', 'Code block', () => ctl.block('```\n', '\n```', '')), h('i.sep'),
    btn('/', 'Insert… (type / in the text)', () => { ctl.insert('/'); trigger(); }), btn('@', 'Mention', () => { ctl.insert('@'); trigger(); }),
    btn('Attach', 'Attach files (or paste, or drop them)', () => fileIn.click(), '.txt'),
    h('span.spacer'), bPrev);
  if (!o.onFiles) fileIn.remove();

  // ---- shell
  const node = h('div.ed', toolbar, ta, pop, preview, fileIn,
    h('div.ed-foot', h('span.dim.hint', o.hint || ('ctrl+⏎ ' + (o.label || 'Save').toLowerCase() + (o.noCancel ? '' : ' · esc cancels') + ' · / formats')), h('span.spacer'),
      !o.noCancel && o.cancel && h('button.btn.ghost', { onclick: () => node._cancel() }, 'Cancel'), o.save && go));
  const size = () => { ta.style.height = 'auto'; ta.style.height = Math.min(ta.scrollHeight + 2, window.innerHeight * 0.6) + 'px'; };
  let busy = false;
  async function run() {
    if (busy || !o.save || (!o.allowEmpty && !ta.value.trim())) return;
    busy = true; go.disabled = true; ta.readOnly = true;
    const text = ta.value;
    try { await o.save(text, mentions.filter(m => text.includes('@' + m.DisplayName))); draft.drop(); } catch (e) { ui.errToast(e); } finally { busy = false; go.disabled = false; ta.readOnly = false; }
  }
  node._save = run;
  // Cancelling with changes asks first (TUI: esc in the description editor); it drops the draft too.
  node._cancel = async () => {
    closePop();
    if (o.noCancel || !o.cancel) return ta.blur();
    if (ta.value !== (o.value || '') && !await ui.confirm({ title: 'Discard your changes?', text: 'What you typed here is lost.', ok: 'Discard', danger: true })) return ta.focus();
    draft.drop(); o.cancel();
  };

  // ---- drafts: what is typed is kept a moment after each change (TUI drafts.go: "unix\ntext" under jira_tab:draft:),
  // so a reload or the terminal brings it back; saving or discarding drops it.
  const draft = { save: () => {}, drop: () => {}, flush: () => {} };
  if (o.draft) {
    const path = '/drafts/' + encodeURIComponent(o.draft);
    let typed = false;
    const keep = () => (ta.value.trim() && ta.value !== (o.value || '') ? api.put(path, { Text: ta.value }) : api.del(path)).catch(() => {});
    const later = debounce(() => { if (typed) keep(); }, 2000);
    draft.save = () => { typed = true; later(); };
    draft.drop = () => { typed = false; api.del(path).catch(() => {}); };
    draft.flush = () => { if (typed) keep(); typed = false; };
    api.get(path, { fresh: true }).then(d => {
      if (!d || !d.Text || d.Text === ta.value || ta.value !== (o.value || '')) return;
      ta.value = d.Text; size();
      ui.toast('Draft restored', { action: { label: 'Drop it', run: () => { ta.value = o.value || ''; size(); draft.drop(); } } });
    }).catch(() => {});
  }
  node._escape = () => { if (pop.hidden) return false; closePop(); return true; };

  ta.addEventListener('input', () => { size(); trigger(); livePreview(); draft.save(); });
  ta.addEventListener('keydown', e => {
    if (pop.hidden || e.ctrlKey || e.metaKey) return;
    const k = e.key;
    if (k === 'ArrowDown' || (k === 'Tab' && !e.shiftKey)) pick = (pick + 1) % items.length;
    else if (k === 'ArrowUp' || (k === 'Tab' && e.shiftKey)) pick = (pick - 1 + items.length) % items.length;
    else if (k === 'Enter') accept(pick);
    else return;
    e.preventDefault(); e.stopPropagation(); if (k !== 'Enter') paint();
  });
  ta.addEventListener('blur', () => setTimeout(closePop, 150));
  ta.addEventListener('paste', e => {
    const fs = [...(e.clipboardData ? e.clipboardData.files : [])];
    if (fs.length && o.onFiles) { e.preventDefault(); files(fs); }
  });
  for (const t of ['dragenter', 'dragover']) node.addEventListener(t, e => { if (o.onFiles && e.dataTransfer && [...e.dataTransfer.types].includes('Files')) { e.preventDefault(); node.classList.add('drop'); } });
  node.addEventListener('dragleave', e => { if (!node.contains(e.relatedTarget)) node.classList.remove('drop'); });
  node.addEventListener('drop', e => {
    node.classList.remove('drop');
    if (o.onFiles && e.dataTransfer && e.dataTransfer.files.length) { e.preventDefault(); files([...e.dataTransfer.files]); }
  });

  // Shortcuts live in a scope while the text has focus, so they beat the global ones (ctrl+k).
  let scope = null;
  ta.addEventListener('focus', () => {
    if (scope) return;
    scope = app.keys.scope('mdedit');
    const b = (spec, fn, desc) => scope.bind(spec, fn, desc, { input: true, group: 'Editor' });
    b('ctrl+b', () => ctl.wrap('**', '**', 'bold'), 'bold');
    b('ctrl+i', () => ctl.wrap('*', '*', 'italic'), 'italic');
    b('ctrl+k', () => ctl.link(), 'link');
    b('ctrl+p', togglePreview, 'toggle preview');
  });
  const release = () => { draft.flush(); if (scope) { scope.dispose(); scope = null; } };
  ta.addEventListener('blur', release);

  requestAnimationFrame(size);
  return { el: node, ta, mentions, size, focus: () => { ta.focus({ preventScroll: true }); ta.selectionStart = ta.selectionEnd = ta.value.length; size(); },
    preview: togglePreview, dispose: release, dropDraft: () => draft.drop() };
}
