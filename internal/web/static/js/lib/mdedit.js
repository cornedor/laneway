// Markdown editor: markdown drawn as rich text, its markers shown only on the
// caret's line (lib/mdarea.js, lib/mdhl.js; Source shows them all): enter
// continues a list, quote or table, tab nests a list item or steps through a
// table's cells, a task box clicks, a marker typed over a selection wraps it,
// pasted HTML comes in as markdown (lib/html2md.js), alt+↑↓ move lines, ctrl+click
// follows a link; a formatting toolbar, a `/` menu for Jira formatting, `@`
// mentions, `:` emoji, ctrl+b/i/k, ctrl+p preview, full screen and pasted/dropped files.
//
//   const e = mdEdit(app, {value, rows, placeholder, mono, issueKey, project, label, save(text, mentions), cancel,
//                          allowEmpty, noCancel, people() → Map(name → accountId), mdOpts() → render options, onFiles(files), hint,
//                          draft: an id ("comment:KEY", "desc:KEY", "desc:KEY:comment:ID") kept in the state file as the TUI's drafts,
//                          base: the Base of the document it edits, kept with the draft and handed to save(text, mentions, base)})
//   e.el (with ._save ._cancel ._escape for the Escape/ctrl+Enter bindings), e.ta, e.mentions, e.size(), e.focus(), e.rebase(base)
//   save answering false keeps the editor and its draft (a save Jira refused).
import { h, clear, debounce, onLeave } from './dom.js';
import { icon } from './icons.js';
import { css } from './css.js';
import { render as md, glyph } from './md.js';
import { jiraKey } from './issuepill.js';
import { mdArea } from './mdarea.js';
import { lines, enter, indent, toggleTask, pasteLink, tableTab, tableArrow, moveLines, wrapWith, backspace, inFence as fenced } from './mdhl.js';
import { htmlToMd, rich } from './html2md.js';
import { T } from './i18n.js';
import { mentionsIn } from './comment.js';

css('mdedit');

const today = () => new Date().toISOString().slice(0, 10);
const MODE = 'laneway.editor.source'; // this browser shows the markdown as typed, not rendered
const source = () => { try { return localStorage.getItem(MODE) === '1'; } catch (e) { return false; } };
const PANELS = ['info', 'note', 'success', 'warning', 'error'];

// Slash commands: apply(ctl) runs with the typed "/query" already removed.
const SLASH = [
  { name: 'h1', label: T('Heading 1'), run: c => c.line('# ') },
  { name: 'h2', label: T('Heading 2'), run: c => c.line('## ') },
  { name: 'h3', label: T('Heading 3'), run: c => c.line('### ') },
  { name: 'bullet', label: T('Bulleted list'), run: c => c.line('- ') },
  { name: 'numbered', label: T('Numbered list'), run: c => c.line('1. ') },
  { name: 'task', label: T('Task list'), run: c => c.line('- [ ] ') },
  { name: 'code', label: T('Code block'), run: c => c.block('```\n', '\n```', '') },
  { name: 'quote', label: T('Quote'), run: c => c.line('> ') },
  { name: 'rule', label: T('Horizontal rule'), run: c => c.insert('---\n\n') },
  { name: 'table', label: T('Table'), run: c => c.insert('| Header | Header |\n| --- | --- |\n| Cell | Cell |\n', 2, 8) },
  ...PANELS.map(p => ({ name: p, label: T('Panel: %s', p), run: c => c.block('<!-- panel:' + p + ' -->\n\n', '\n\n<!-- /panel -->', 'Text') })),
  { name: 'expand', label: T('Expand (collapsible)'), run: c => c.block('<!-- expand: Title -->\n\n', '\n\n<!-- /expand -->', 'Text') },
  { name: 'mention', label: T('Mention someone'), run: c => c.insert('@') },
  { name: 'issue', label: T('Issue reference'), run: c => c.issueRef() },
  { name: 'emoji', label: T('Emoji'), run: c => c.insert(':') },
  { name: 'link', label: T('Link'), run: c => c.link() },
  { name: 'date', label: T('Today’s date'), run: c => c.insert('<date>' + today() + '</date> ') },
  { name: 'decision', label: T('Decision'), run: c => c.line('<> ') },
  ...[['grey', 'neutral'], ['purple', 'purple'], ['blue', 'blue'], ['red', 'red'], ['yellow', 'yellow'], ['green', 'green']]
    .map(([label, color]) => ({ name: 'status' + label, label: T('Status: %s', label), run: c => c.wrap('<status color="' + color + '">', '</status>', 'DONE') })),
  { name: 'underline', label: T('Underline'), run: c => c.wrap('<u>', '</u>', 'text') },
  ...[['blue', '#0747a6'], ['teal', '#008da6'], ['green', '#006644'], ['orange', '#ff991f'], ['red', '#bf2600'], ['purple', '#403294'], ['grey', '#97a0af']]
    .map(([label, hex]) => ({ name: 'colour' + label, label: T('Text colour: %s', label), run: c => c.wrap('<span style="color:' + hex + '">', '</span>', 'text') })),
];

export function mdEdit(app, o) {
  const { api, ui } = app;
  const mentions = [];
  const glyphs = new Map(); // emoji taken here → glyph, for the preview
  // Longest first, so "@Ann Lee" wins over "@Ann".
  const names = () => [...new Set([...mentions.map(m => m.DisplayName), ...(o.people ? [...o.people()] : []).map(([n]) => n)])].filter(Boolean).sort((a, b) => b.length - a.length);
  const cache = new Map(); // a line → its HTML, while the names and the emoji table stay the same
  let cacheSig = '';
  // An attachment's picture, as the panel draws it (the page loads no other images).
  const img = u => {
    const m = u.startsWith('attachment:') && o.mdOpts && o.mdOpts();
    return m && m.attachment ? m.attachment(u.slice(11)) : '';
  };
  // Pasted HTML (not a code editor's, not into a code block) comes in as markdown.
  const paste = (v, a, b, t, html) => {
    const link = t && pasteLink(v, a, b, t);
    if (link) return link;
    if (!html || !rich(html) || fenced(v, a)) return null;
    const m = htmlToMd(new DOMParser().parseFromString(html, 'text/html').body);
    if (!m || m.replace(/\s+/g, ' ').trim() === t.replace(/\s+/g, ' ').trim()) return null;
    return { from: a, to: b, text: m, a: a + m.length, b: a + m.length };
  };
  const ta = mdArea('div.input.ed-ta' + (o.mono ? '.mono' : '') + (source() ? '' : '.live'), { value: o.value, rows: o.rows || 4, placeholder: o.placeholder,
    lines: t => {
      const ns = names(), sig = ns.join('\u0001') + (glyph('smile') ? '+' : '');
      if (sig !== cacheSig) { cache.clear(); cacheSig = sig; }
      return lines(t, { names: ns, emoji: glyph, img, cache });
    }, enter, paste, type: wrapWith });
  const pop = h('div.mention-pop', { hidden: true, role: 'listbox' });
  const preview = h('div.md.ed-preview', { hidden: true });
  const fileIn = h('input', { type: 'file', multiple: true, hidden: true, onchange: () => { if (fileIn.files.length) files([...fileIn.files]); fileIn.value = ''; } });
  const go = h('button.btn.primary', { onclick: () => run() }, o.label || T('Save'));

  // ---- text operations: each one undo step
  const replace = (from, to, text, s0 = text.length, s1 = s0) => {
    ta.focus({ preventScroll: true });
    ta.edit({ from, to, text, a: from + s0, b: from + s1 });
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
      const c = await ui.pick({ title: T('Issue'), items: [], label: x => x.Key + ' ' + x.Summary, placeholder: T('Find an issue…'), empty: T('Type to search'),
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
    // At the caret: below it, above when there is no room.
    const r = ta.caretRect(), below = window.innerHeight - r.bottom > 270 || r.top < 270;
    Object.assign(pop.style, { position: 'fixed', left: Math.max(4, Math.min(r.left - 8, window.innerWidth - 270)) + 'px', right: 'auto',
      top: below ? r.bottom + 4 + 'px' : 'auto', bottom: below ? 'auto' : window.innerHeight - r.top + 4 + 'px' });
    const s = pop.querySelector('.sel'); if (s) s.scrollIntoView({ block: 'nearest' });
  };
  function accept(i) {
    const it = items[i]; if (!it) return;
    const end = ta.selectionStart, at = from, k = kind;
    closePop();
    if (k === 'mention') {
      const name = '@' + it.DisplayName + ' ';
      if (it.AccountID && !mentions.some(m => m.AccountID === it.AccountID)) mentions.push({ AccountID: it.AccountID, DisplayName: it.DisplayName });
      replace(at, end, name, name.length);
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
    clear(preview).append(text.trim() ? md(text, o.mdOpts ? o.mdOpts() : {}) : h('p.faint', T('Nothing to preview.')));
  };
  const livePreview = debounce(renderPreview, 120);
  const togglePreview = () => { preview.hidden = !preview.hidden; bPrev.classList.toggle('on', !preview.hidden); renderPreview(); ta.focus({ preventScroll: true }); };

  // ---- files: onFiles uploads them and answers each one's attachment ({ID, Filename}, or null).
  // On an issue an image goes in the text where it was pasted: a line while it uploads, then ![name](attachment:ID).
  let uploads = 0;
  async function files(list) {
    if (!o.onFiles) return;
    const marks = list.map(f => o.issueKey && f.type.startsWith('image/') ? '![' + T('Uploading %s %d…', f.name || T('image'), ++uploads) + ']()' : '');
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
  const bPrev = btn(T('Preview'), T('Preview (ctrl+p)'), togglePreview, '.txt');
  const bSrc = btn(T('Source'), T('Show the markdown everywhere, not only on the caret’s line'), () => {
    const on = ta.classList.toggle('live');
    bSrc.classList.toggle('on', !on);
    try { localStorage.setItem(MODE, on ? '0' : '1'); } catch (e) { /* this time only */ }
    ta.focus({ preventScroll: true });
  }, '.txt' + (source() ? '.on' : ''));
  const full = () => {
    const on = node.classList.toggle('full');
    bFull.classList.toggle('on', on);
    ta.focus({ preventScroll: true });
  };
  const bFull = btn(icon('maximize-2'), T('Full screen (esc leaves it)'), full);
  const toolbar = h('div.ed-tools', { role: 'toolbar' },
    btn(icon('bold'), T('Bold (ctrl+b)'), () => ctl.wrap('**', '**', 'bold')), btn(icon('italic'), T('Italic (ctrl+i)'), () => ctl.wrap('*', '*', 'italic')),
    btn(icon('strikethrough'), T('Strikethrough'), () => ctl.wrap('~~', '~~', 'text')), btn(icon('code'), T('Inline code'), () => ctl.wrap('`', '`', 'code')),
    btn(icon('link'), T('Link (ctrl+k)'), () => ctl.link()), h('i.sep'),
    btn(icon('heading'), T('Heading (/h2)'), () => ctl.line('## ')), btn(icon('list'), T('Bulleted list'), () => ctl.line('- ')), btn(icon('list-ordered'), T('Numbered list'), () => ctl.line('1. ')),
    btn(icon('list-todo'), T('Task list'), () => ctl.line('- [ ] ')), btn(icon('quote'), T('Quote'), () => ctl.line('> ')), btn(icon('square-code'), T('Code block'), () => ctl.block('```\n', '\n```', '')), h('i.sep'),
    btn('/', T('Insert… (type / in the text)'), () => { ctl.insert('/'); trigger(); }), btn(icon('at-sign'), T('Mention'), () => { ctl.insert('@'); trigger(); }),
    o.onFiles && btn(icon('paperclip'), T('Attach files (or paste, or drop them)'), () => fileIn.click()),
    h('span.spacer'), bSrc, bPrev, bFull);
  if (!o.onFiles) fileIn.remove();

  // ---- shell
  const node = h('div.ed', toolbar, ta, pop, preview, fileIn,
    h('div.ed-foot', h('span.dim.hint', o.hint || (o.noCancel ? T('ctrl+⏎ %s · / formats', (o.label || T('Save')).toLowerCase()) : T('ctrl+⏎ %s · esc cancels · / formats', (o.label || T('Save')).toLowerCase()))), h('span.spacer'),
      !o.noCancel && o.cancel && h('button.btn.ghost', { onclick: () => node._cancel() }, T('Cancel')), o.save && go));
  const size = () => {}; // the field grows with its text (css: max-height)
  let busy = false;
  let base = o.base || '';
  async function run() {
    if (busy || !o.save || (!o.allowEmpty && !ta.value.trim())) return;
    busy = true; go.disabled = true; ta.readOnly = true;
    const text = ta.value;
    try { if (await o.save(text, mentionsIn(text, mentions, people()), base) !== false) draft.drop(); } catch (e) { ui.errToast(e); } finally { busy = false; go.disabled = false; ta.readOnly = false; }
  }
  node._save = run;
  // Cancelling with changes asks first (TUI: esc in the description editor); it drops the draft too.
  node._cancel = async () => {
    closePop();
    if (o.noCancel || !o.cancel) return ta.blur();
    if (ta.value !== (o.value || '') && !await ui.confirm({ title: T('Discard your changes?'), text: T('What you typed here is lost.'), ok: T('Discard'), danger: true })) return ta.focus();
    draft.drop(); o.cancel();
  };

  // ---- drafts: what is typed is kept a moment after each change (TUI drafts.go: "unix base\ntext" under jira_tab:draft:),
  // so a reload or the terminal brings it back; saving or discarding drops it. Leaving the page sends the last
  // moment's typing (keepalive: it outlives the page). A restored draft saves against the document it was written on:
  // one Jira changed since asks first.
  const draft = { save: () => {}, drop: () => {}, flush: () => {} };
  if (o.draft) {
    const path = '/drafts/' + encodeURIComponent(o.draft);
    let typed = false;
    const keep = (keepalive = false) => (ta.value.trim() && ta.value !== (o.value || '') ? api.put(path, { Text: ta.value, Base: base }, { keepalive }) : api.del(path, undefined, { keepalive })).catch(() => {});
    const later = debounce(() => { if (typed) keep(); }, 2000);
    draft.save = () => { typed = true; later(); };
    draft.drop = () => { typed = false; api.del(path).catch(() => {}); };
    draft.flush = keepalive => { if (typed) keep(keepalive); typed = false; };
    api.get(path, { fresh: true }).then(d => {
      if (!d || !d.Text || d.Text === ta.value || ta.value !== (o.value || '')) return;
      ta.value = d.Text; size();
      const was = base;
      if (d.Base && base) base = d.Base;
      ui.toast(base !== was ? T('Draft restored; Jira’s has changed since') : T('Draft restored'),
        { action: { label: T('Drop it'), run: () => { ta.value = o.value || ''; base = was; size(); draft.drop(); } } });
    }).catch(() => {});
  }
  node._escape = () => {
    if (!pop.hidden) { closePop(); return true; }
    if (node.classList.contains('full')) { full(); return true; }
    return false;
  };

  ta.addEventListener('input', () => { size(); trigger(); livePreview(); draft.save(); });
  ta.addEventListener('keydown', e => {
    if (e.defaultPrevented) return;
    if (e.altKey && !e.ctrlKey && !e.metaKey && (e.key === 'ArrowUp' || e.key === 'ArrowDown') && pop.hidden) {
      const ed = moveLines(ta.value, ta.selectionStart, ta.selectionEnd, e.key === 'ArrowUp' ? -1 : 1);
      e.preventDefault();
      if (ed) ta.edit(ed);
      return;
    }
    if (e.ctrlKey || e.metaKey || e.altKey) return;
    // Up and down in a table row stay in its column (the browser loses its way in a grid).
    if (pop.hidden && !e.shiftKey && (e.key === 'ArrowUp' || e.key === 'ArrowDown') && ta.selectionStart === ta.selectionEnd) {
      const to = tableArrow(ta.value, ta.selectionStart, e.key === 'ArrowUp' ? -1 : 1);
      if (to != null) { e.preventDefault(); ta.setSelectionRange(to); }
      return;
    }
    if (pop.hidden && e.key === 'Backspace' && !e.shiftKey) {
      const ed = backspace(ta.value, ta.selectionStart, ta.selectionEnd);
      if (ed) { e.preventDefault(); ta.edit(ed); }
      return;
    }
    if (pop.hidden) {
      if (e.key !== 'Tab') return;
      const [a, b] = [ta.selectionStart, ta.selectionEnd];
      const ed = indent(ta.value, a, b, e.shiftKey) || tableTab(ta.value, a, e.shiftKey);
      if (ed) { e.preventDefault(); ta.edit(ed); }
      return;
    }
    const k = e.key;
    if (k === 'ArrowDown' || (k === 'Tab' && !e.shiftKey)) pick = (pick + 1) % items.length;
    else if (k === 'ArrowUp' || (k === 'Tab' && e.shiftKey)) pick = (pick - 1 + items.length) % items.length;
    else if (k === 'Enter') accept(pick);
    else return;
    e.preventDefault(); e.stopPropagation(); if (k !== 'Enter') paint();
  });
  ta.addEventListener('blur', () => setTimeout(closePop, 150));
  ta.addEventListener('click', e => {
    const t = e.target.closest ? e.target : e.target.parentElement;
    const box = t.closest('.hl-box');
    if (box && !ta.readOnly) return ta.edit(toggleTask(ta.value, ta.offsetOf(box)));
    if (!(e.ctrlKey || e.metaKey)) return;
    const a = t.closest('[data-href]'), k = t.closest('[data-key]');
    const m = o.mdOpts && o.mdOpts(), local = a && m && m.onKey && jiraKey(a.dataset.href, m.site); // an issue on this site opens here
    if (local) { e.preventDefault(); m.onKey(local); } else if (a) { e.preventDefault(); window.open(a.dataset.href, '_blank', 'noopener'); } else if (k && m && m.onKey) { e.preventDefault(); m.onKey(k.dataset.key); }
  });
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
    b('ctrl+b', () => ctl.wrap('**', '**', 'bold'), T('bold'));
    b('ctrl+i', () => ctl.wrap('*', '*', 'italic'), T('italic'));
    b('ctrl+k', () => ctl.link(), T('link'));
    b('ctrl+shift+x', () => ctl.wrap('~~', '~~', 'text'), T('strikethrough'));
    b('ctrl+e', () => ctl.wrap('`', '`', 'code'), T('inline code'));
    b('ctrl+p', togglePreview, T('toggle preview'));
    scope.bind('Escape', closePop, '', { input: true, hidden: true, when: () => !pop.hidden }); // the list, not the dialog the editor is in
  });
  const release = () => { draft.flush(); if (scope) { scope.dispose(); scope = null; } };
  ta.addEventListener('blur', release);
  const unLeave = o.draft ? onLeave(() => draft.flush(true)) : () => {};

  requestAnimationFrame(size);
  return { el: node, ta, mentions, size, focus: () => { ta.focus({ preventScroll: true }); ta.setSelectionRange(ta.value.length); },
    preview: togglePreview, dispose: () => { release(); unLeave(); }, dropDraft: () => draft.drop(), rebase: b => { base = b; } };
}
