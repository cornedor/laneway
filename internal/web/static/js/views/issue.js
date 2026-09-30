// The issue panel (right-hand aside) and full page. mountIssue(el, key, {app, full}) → cleanup.
// One scroll, three tabs: Details (fields, description, children, links, files), Comments, History.
// Renders from cache first, refetches, and patches sections rather than rebuilding them.
import { h, clear, debounce, delegate } from '../lib/dom.js';
import { css } from '../lib/css.js';
import { render as md } from '../lib/md.js';
import { ago, dateTime, shortDate, isZero, duration, plural } from '../lib/fmt.js';

const RECENT = 40;           // comments drawn at first; the rest on demand
const drafts = new Map();    // unsent comment text by issue key
const fill = (el, ...kids) => { clear(el); for (const k of kids.flat(Infinity)) if (k) el.append(k); return el; };
const dash = h('span.faint', '—');

export function mountIssue(el, key, { app, full }) {
  css('issue');
  const { api, bus, ui } = app;
  const me = () => (app.session && app.session.me) || {};
  const st = { issue: null, card: null, tab: 'details', children: null, weblinks: null, hist: null, tis: null, dev: null,
    focusId: null, reply: null, all: false, pending: [], descSig: null, editingDesc: false, editingComment: null };
  let dead = false;
  const scope = app.keys.scope('issue');
  // Beside a board the panel's keys apply only while it has focus (Tab / click), so the board keeps j/k/c/e/s.
  if (!full) {
    const bind = scope.bind;
    scope.bind = (spec, fn, desc, opts = {}) => bind(spec, fn, desc, { ...opts, help: true, when: opts.when || (() => el.contains(document.activeElement)) });
  }

  // ---- skeleton
  const scroll = h('div.iss-scroll', { tabindex: -1 });
  const head = h('header.iss-head');
  const tabs = h('nav.iss-tabs', { role: 'tablist' });
  const panes = {
    details: h('section.pane.details'),
    comments: h('section.pane.comments'),
    history: h('section.pane.history'),
  };
  const box = {
    fields: h('div.fields'), desc: h('div.desc'), children: h('div.block.children'), links: h('div.block.links'), files: h('div.block.files'),
    list: h('div.cm-list'), more: h('div.cm-more'), composer: h('div.composer'),
  };
  panes.details.append(box.fields, box.desc, box.children, box.links, box.files);
  panes.comments.append(box.more, box.list, box.composer);
  scroll.append(panes.details, panes.comments, panes.history);
  const root = h('div.iss' + (full ? '.full' : ''), !full && h('div.iss-grip', { title: 'Drag to resize', role: 'separator' }), head, tabs, scroll);
  el.append(root);

  // ---- helpers
  const copy = async (text, what) => {
    try { await navigator.clipboard.writeText(text); } catch (e) {
      const t = h('textarea', { value: text, style: { position: 'fixed', opacity: 0 } }); document.body.append(t); t.select();
      try { document.execCommand('copy'); } catch (e2) { /* nothing more to try */ }
      t.remove();
    }
    ui.toast(what + ' copied');
  };
  const browseURL = () => (st.issue && st.issue.URL) || (app.session.baseURL + '/browse/' + key);
  const open = k => (full ? app.go('/issue/' + k) : app.panel.open(k));
  const prefixes = () => new Set([...(app.session.projects || []), key.split('-')[0], ...(st.issue ? st.issue.Links.map(l => l.Key.split('-')[0]) : [])]);
  const people = () => {
    const i = st.issue, m = new Map();
    const add = (id, name) => { if (name && !m.has(name)) m.set(name, id || ''); };
    if (i) { add(i.AssigneeAccountID, i.Assignee); add(i.ReporterAccountID, i.Reporter); for (const c of i.Comments) add(c.AuthorID, c.Author); }
    return m;
  };
  const mdOpts = () => {
    const pre = prefixes();
    return { attachment: id => '/api/attachments/' + encodeURIComponent(id), names: [...people().keys()].sort((a, b) => b.length - a.length),
      isKey: s => pre.has(s.slice(0, s.lastIndexOf('-'))), onKey: open };
  };
  const fail = e => ui.errToast(e);
  const changed = () => bus.emit('issue:changed', { key });
  const edit = (field, anchor) => app.actions.edit(key, field, anchor);

  // ---- header + tabs
  function renderHead() {
    const i = st.issue, c = st.card;
    const summary = (i && i.Summary) || (c && c.Summary) || '';
    const type = (i && i.Type) || (c && c.Type) || '';
    clear(head).append(
      h('div.iss-top',
        type && h('span.chip', type),
        h('button.iss-key.btn.link', { title: 'Copy key (y)', onclick: () => copy(key, key) }, key),
        h('button.btn.ghost.sm', { title: 'Copy link (Y)', onclick: () => copy(browseURL(), 'Link') }, 'Copy link'),
        h('span.spacer'),
        h('a.btn.ghost.sm', { href: browseURL(), target: '_blank', rel: 'noopener noreferrer', title: 'Open in Jira (o)' }, 'Jira ↗'),
        h('button.btn.ghost.sm', { title: full ? 'Back (esc)' : 'Close (esc)', onclick: goBack }, full ? '← Back' : '✕')),
      h('h1.iss-title', { title: 'Edit summary', onclick: e => edit('summary', e.currentTarget) }, summary || '…'),
      i && h('div.iss-sub', h('button.pill-btn', { title: 'Change status', onclick: () => app.actions.transition(key) }, ui.statusPill(i.Status, i.StatusCategory)),
        c && c.Flagged && h('span.chip.flag', 'Flagged'),
        h('span.dim', 'updated ' + ago(i.Updated))));
    document.title = key + (summary ? ' ' + summary : '') + ' · laneway';
  }
  function renderTabs() {
    const n = st.issue ? (st.issue.CommentTotal || st.issue.Comments.length) : 0;
    clear(tabs).append(...[['details', 'Details', '1'], ['comments', 'Comments' + (n ? ' ' + n : ''), '2'], ['history', 'History', '3']].map(([id, label, k]) =>
      h('button.tab' + (st.tab === id ? '.on' : ''), { role: 'tab', 'aria-selected': st.tab === id, onclick: () => setTab(id) }, label, h('kbd', k))));
  }
  function setTab(t) {
    st.tab = t;
    for (const [id, p] of Object.entries(panes)) p.hidden = id !== t;
    renderTabs();
    if (t === 'history') loadHistory();
    if (t === 'comments' && st.focusId == null) focusComment(null);
  }
  function goBack() {
    if (!full) return app.panel.close();
    if (history.length > 1) history.back(); else app.go('/board');
  }

  // ---- fields
  function renderFields() {
    const i = st.issue; if (!i) return clear(box.fields);
    const c = st.card || {};
    const cell = (field, label, value) => {
      const inner = [h('span.k', label), h('span.v', value == null || value === '' ? dash : value)];
      return field ? h('button.fld', { dataset: { field }, title: 'Edit ' + label.toLowerCase(), onclick: e => edit(field, e.currentTarget) }, inner)
        : h('div.fld.ro', inner);
    };
    const parent = (c.ParentKey || parentLink());
    const pk = c.ParentKey || (parent && parent.Key), ps = c.ParentSummary || (parent && parent.Summary);
    const date = t => (isZero(t) ? null : shortDate(t));
    clear(box.fields).append(
      cell('status', 'Status', ui.statusPill(i.Status, i.StatusCategory)),
      cell('priority', 'Priority', i.Priority),
      cell('assignee', 'Assignee', i.Assignee && h('span.who', ui.avatar(i.Assignee, c.AvatarURL, 18), i.Assignee)),
      cell('reporter', 'Reporter', i.Reporter),
      cell('points', 'Points', i.StoryPoints),
      cell('labels', 'Labels', i.Labels && i.Labels.length && h('span.chips', i.Labels.map(l => h('span.chip', l)))),
      cell('parent', 'Parent', pk && h('span.parent', h('a.issue-ref', { href: '#/issue/' + pk, title: ps, onclick: e => { e.preventDefault(); e.stopPropagation(); open(pk); } }, pk), ps && h('span.dim.clip', ' ' + ps))),
      cell('sprint', 'Sprint', c.Sprint),
      cell('due', 'Due', date(c.Due)),
      cell(null, 'Updated', ago(i.Updated)),
      cell(null, 'Created', !isZero(c.Created) ? dateTime(c.Created) : null));
  }
  const parentLink = () => st.issue && st.issue.Links.find(l => l.Rel === 'parent');

  // ---- description
  function renderDesc(force) {
    const i = st.issue; if (!i || st.editingDesc) return;
    const sig = i.Description;
    if (!force && st.descSig === sig) return;
    st.descSig = sig;
    const body = h('div.md');
    if (sig && sig.trim()) body.append(md(sig, { ...mdOpts(), onTask: toggleTask }));
    else body.append(h('p.faint', 'No description. Press e to write one.'));
    clear(box.desc).append(h('div.sec-head', h('h3', 'Description'), h('button.btn.ghost.sm', { title: 'Edit (E)', onclick: editDesc }, 'Edit')), body);
    box.desc.ondblclick = e => { if (!e.target.closest('a,input,img,summary')) editDesc(); };
  }
  let taskChain = Promise.resolve();
  function toggleTask(n, total, done, input) {
    taskChain = taskChain.then(() => api.post('/issues/' + key + '/description/task', { N: n, Total: total, Done: done }))
      .then(() => { st.issue.Description = flipTask(st.issue.Description, n, done); st.descSig = st.issue.Description; box.desc.querySelector('[data-task="' + n + '"]')?.closest('li')?.classList.toggle('done', done); })
      .catch(e => { input.checked = !done; fail(e); reload(); });
  }
  async function editDesc() {
    if (st.editingDesc || !st.issue) return;
    setTab('details');
    let ed;
    try { ed = await api.get('/issues/' + key + '/description', { fresh: true }); } catch (e) { return fail(e); }
    if (dead) return;
    if (!ed.Editable) {
      return ui.toast('Edit this one in Jira: ' + (ed.Reason || 'markdown cannot hold it'), { kind: 'err', action: { label: 'Open', run: () => window.open(browseURL(), '_blank', 'noopener') } });
    }
    st.editingDesc = true;
    const e = editor({ value: ed.Markdown, rows: 10, placeholder: 'Description (markdown)…', allowEmpty: true, label: 'Save', mono: true,
      save: async (text, mentions) => {
        await api.put('/issues/' + key + '/description', { Markdown: text, Kept: [...ed.Kept, ...mentions.map(mentionNode)] });
        st.editingDesc = false; st.issue.Description = null; st.descSig = null; changed(); ui.toast('Description saved', { kind: 'ok' });
      },
      cancel: () => { st.editingDesc = false; renderDesc(true); } });
    clear(box.desc).append(h('div.sec-head', h('h3', 'Description')), e.el);
    e.focus();
  }

  // ---- children, links, files
  function renderChildren() {
    const kids = st.children;
    if (!kids || !kids.length) return clear(box.children);
    const done = kids.filter(k => k.Done).length;
    clear(box.children).append(
      h('div.sec-head', h('h3', 'Child issues'), h('span.dim', done + '/' + kids.length), h('div.bar', h('i', { style: { width: Math.round(100 * done / kids.length) + '%' } }))),
      h('div.rows', kids.map(k => h('a.row-link' + (k.Done ? '.done' : ''), { href: '#/issue/' + k.Key, onclick: e => { e.preventDefault(); open(k.Key); } },
        h('span.mono.k', k.Key), h('span.clip', k.Summary), ui.statusPill(k.Status, k.Done ? 'done' : 'indeterminate'), k.Assignee && ui.avatar(k.Assignee, null, 18)))));
  }
  function renderLinks() {
    const i = st.issue; if (!i) return clear(box.links);
    const links = i.Links.filter(l => l.Rel !== 'parent' && l.Rel !== 'subtask');
    const web = st.weblinks || [];
    const groups = new Map();
    for (const l of links) (groups.get(l.Rel) || groups.set(l.Rel, []).get(l.Rel)).push(l);
    fill(box.links,
      h('div.sec-head', h('h3', 'Links'), h('button.btn.ghost.sm', { title: 'Link an issue (L)', onclick: addLink }, '+ Issue'), h('button.btn.ghost.sm', { onclick: addWebLink }, '+ Web')),
      !links.length && !web.length && h('p.faint', 'No links.'),
      [...groups].map(([rel, ls]) => h('div.lgroup', h('div.rel', rel), ls.map(l => h('div.row-link', { dataset: { key: l.Key }, onclick: () => open(l.Key) },
        h('span.mono.k', l.Key), h('span.clip', l.Summary), l.Status && h('span.chip', l.Status),
        l.LinkID && h('button.btn.ghost.sm.x', { title: 'Remove link', onclick: e => { e.stopPropagation(); removeLink(l); } }, '✕'))))),
      web.length > 0 && h('div.lgroup', h('div.rel', 'Web'), web.map(w => h('a.row-link', { href: safeHref(w.URL), target: '_blank', rel: 'noopener noreferrer' },
        h('span.clip', w.Title), w.App && h('span.chip', w.App)))));
  }
  async function addLink() {
    let types;
    try { types = await api.get('/linktypes'); } catch (e) { return fail(e); }
    const opts = types.flatMap(t => [{ label: t.Outward, Type: t.Name, Outward: true }, ...(t.Inward !== t.Outward ? [{ label: t.Inward, Type: t.Name, Outward: false }] : [])]);
    const t = await ui.pick({ title: key + ' …', items: opts, label: o => o.label, placeholder: 'Link type…' });
    if (!t) return;
    const other = await ui.prompt({ title: key + ' ' + t.label + ' …', placeholder: 'Issue key, e.g. ' + key, ok: 'Link' });
    if (!other || !other.trim()) return;
    try { await api.post('/issues/' + key + '/links', { Type: t.Type, Other: other.trim().toUpperCase(), Outward: t.Outward }); changed(); } catch (e) { fail(e); }
  }
  async function addWebLink() {
    const url = await ui.prompt({ title: 'Link a web page', placeholder: 'https://…', ok: 'Add' });
    if (!url || !url.trim()) return;
    try { await api.post('/issues/' + key + '/weblinks', { URL: url.trim(), Title: '' }); st.weblinks = null; loadExtras(); } catch (e) { fail(e); }
  }
  async function removeLink(l) {
    if (!await ui.confirm({ title: 'Remove link', text: l.Rel + ' ' + l.Key + '?', ok: 'Remove', danger: true })) return;
    try { await api.del('/issues/' + key + '/links/' + encodeURIComponent(l.LinkID)); changed(); } catch (e) { fail(e); }
  }
  const safeHref = u => (/^https?:\/\//i.test(u) ? u : '#');

  const size = n => (n > 1 << 20 ? (n / (1 << 20)).toFixed(1) + ' MB' : Math.max(1, Math.round(n / 1024)) + ' KB');
  function renderFiles() {
    const fs = (st.issue && st.issue.Attachments) || [];
    if (!fs.length) return clear(box.files);
    const url = a => '/api/attachments/' + encodeURIComponent(a.ID);
    const imgs = fs.filter(a => a.MimeType.startsWith('image/') && !/svg/.test(a.MimeType)), rest = fs.filter(a => !imgs.includes(a));
    fill(box.files,h('div.sec-head', h('h3', 'Attachments'), h('span.dim', String(fs.length))),
      imgs.length > 0 && h('div.thumbs', imgs.map(a => h('a.thumb', { href: url(a), title: a.Filename + ' · ' + size(a.Size), onclick: e => { e.preventDefault(); lightbox(url(a), a.Filename); } },
        h('img', { src: url(a), alt: a.Filename, loading: 'lazy', decoding: 'async' }), h('span.clip', a.Filename)))),
      rest.map(a => h('a.row-link', { href: url(a) + '?download=1&name=' + encodeURIComponent(a.Filename), download: a.Filename }, h('span.clip', a.Filename), h('span.dim', size(a.Size)))));
  }
  const lightbox = (src, name) => ui.modal(h('img.lightbox', { src, alt: name }), { title: name, wide: true });
  root.addEventListener('click', e => { const im = e.target.closest('img.md-img'); if (im) lightbox(im.src, im.alt); });

  // ---- comments
  const order = () => {
    const cs = st.issue ? st.issue.Comments : [];
    const byId = new Map(cs.map(c => [c.ID, c]));
    const kids = new Map(), roots = [];
    for (const c of cs) {
      let p = c.ParentID && byId.get(c.ParentID);
      while (p && p.ParentID && byId.get(p.ParentID)) p = byId.get(p.ParentID);
      if (p) (kids.get(p.ID) || kids.set(p.ID, []).get(p.ID)).push(c); else roots.push(c);
    }
    const out = [];
    for (const r of roots) { out.push({ c: r, reply: false }); for (const k of kids.get(r.ID) || []) out.push({ c: k, reply: true }); }
    for (const p of st.pending) out.push({ c: p, reply: false });
    return out;
  };
  const cmEls = new Map();
  function commentEl(c, reply) {
    const mine = !c.pending && c.AuthorID && c.AuthorID === me().AccountID;
    const sig = [c.Body, c.Author, reply, mine, c.pending].join('\u0001');
    const hit = cmEls.get(c.ID);
    if (hit && hit.sig === sig) return hit.el;
    const body = h('div.md.cbody');
    body.append(md(c.Body, mdOpts()));
    const act = (a, label, title) => h('button.btn.ghost.sm', { dataset: { act: a }, title }, label);
    const node = h('article.cm' + (reply ? '.reply' : '') + (mine ? '.mine' : '') + (c.pending ? '.pending' : ''), { dataset: { id: c.ID }, tabindex: -1 },
      h('header', ui.avatar(c.Author, null, 22), h('b', c.Author), h('time', { title: dateTime(c.Created) }, c.pending ? 'sending…' : ago(c.Created)),
        h('span.spacer'), !c.pending && h('span.acts', act('reply', 'Reply', 'Reply (R)'), mine && act('edit', 'Edit', 'Edit (e)'), mine && act('del', 'Delete', 'Delete (d)'))),
      body);
    cmEls.set(c.ID, { el: node, sig });
    return node;
  }
  function renderComments() {
    if (!st.issue) return;
    const all = order();
    const total = st.issue.CommentTotal || st.issue.Comments.length;
    const shown = st.all ? all : all.slice(-RECENT);
    const want = shown.map(({ c, reply }) => commentEl(c, reply));
    const live = new Set(shown.map(s => s.c.ID));
    if (st.editingComment && !live.has(st.editingComment.id)) st.editingComment = null;
    for (const [id, v] of cmEls) if (!live.has(id)) { cmEls.delete(id); v.el.remove(); }
    let at = box.list.firstChild;
    for (const n of want) {
      if (n === at) { at = at.nextSibling; continue; }
      box.list.insertBefore(n, at);
    }
    while (at) { const nx = at.nextSibling; at.remove(); at = nx; }
    for (const n of box.list.children) n.classList.toggle('focus', n.dataset.id === st.focusId);
    clear(box.more);
    if (all.length > shown.length) box.more.append(h('button.btn', { onclick: () => { st.all = true; renderComments(); } }, 'Show ' + plural(all.length - shown.length, 'earlier comment')));
    else if (total > all.length && !st.pending.length) box.more.append(h('p.faint', plural(total - all.length, 'older comment') + ' not loaded. Open in Jira to read them.'));
    if (!all.length) box.list.dataset.empty = 'No comments yet.'; else delete box.list.dataset.empty;
  }
  const commentEls = () => [...box.list.children].filter(n => n.dataset.id && !n.dataset.id.startsWith('tmp-'));
  function focusComment(id, scrollTo = true) {
    st.focusId = id;
    for (const n of box.list.children) n.classList.toggle('focus', n.dataset.id === id);
    const n = id && box.list.querySelector('[data-id="' + CSS.escape(id) + '"]');
    if (n && scrollTo) n.scrollIntoView({ block: 'nearest' });
  }
  function moveComment(d) {
    const els = commentEls(); if (!els.length) return;
    let i = els.findIndex(n => n.dataset.id === st.focusId);
    i = i < 0 ? (d > 0 ? 0 : els.length - 1) : Math.max(0, Math.min(els.length - 1, i + d));
    focusComment(els[i].dataset.id);
  }
  const focused = () => st.issue && st.issue.Comments.find(c => c.ID === st.focusId);
  const isMine = c => c && c.AuthorID && c.AuthorID === me().AccountID;

  delegate(box.list, 'click', 'article.cm', (e, art) => {
    if (!e.target.closest('a,input,img,textarea')) focusComment(art.dataset.id, false);
    const b = e.target.closest('button[data-act]'); if (!b) return;
    const c = st.issue.Comments.find(x => x.ID === art.dataset.id);
    if (!c) return;
    ({ reply: () => replyTo(c), edit: () => editComment(c), del: () => deleteComment(c) })[b.dataset.act]();
  });

  async function editComment(c) {
    if (st.editingComment) return;
    const art = box.list.querySelector('[data-id="' + CSS.escape(c.ID) + '"]'); if (!art) return;
    let ed;
    try { ed = await api.get('/issues/' + key + '/comments/' + encodeURIComponent(c.ID) + '/edit', { fresh: true }); } catch (e) { return fail(e); }
    if (!ed.Editable) return ui.toast('Edit this comment in Jira: ' + (ed.Reason || 'markdown cannot hold it'), { kind: 'err' });
    st.editingComment = { id: c.ID };
    const body = art.querySelector('.cbody');
    const e = editor({ value: ed.Markdown, rows: 4, label: 'Save',
      save: async (text, mentions) => {
        await api.put('/issues/' + key + '/comments/' + encodeURIComponent(c.ID), { Markdown: text, Kept: [...ed.Kept, ...mentions.map(mentionNode)] });
        st.editingComment = null; cmEls.delete(c.ID); art.remove(); changed();
      },
      cancel: () => { st.editingComment = null; body.hidden = false; e.el.remove(); } });
    body.hidden = true; body.after(e.el); e.focus();
  }
  async function deleteComment(c) {
    if (!await ui.confirm({ title: 'Delete comment', text: 'Delete this comment by ' + c.Author + '?', ok: 'Delete', danger: true })) return;
    try { await api.del('/issues/' + key + '/comments/' + encodeURIComponent(c.ID)); st.issue.Comments = st.issue.Comments.filter(x => x.ID !== c.ID); renderComments(); changed(); } catch (e) { fail(e); }
  }

  // composer
  const comp = editor({ value: drafts.get(key) || '', rows: 3, placeholder: 'Write a comment… (@ to mention, markdown works)', label: 'Comment', noCancel: true,
    save: async (text, mentions) => {
      const tmp = { ID: 'tmp-' + Date.now(), Author: me().DisplayName || 'You', AuthorID: me().AccountID, Body: text, Created: new Date(), pending: true };
      st.pending.push(tmp); renderComments(); box.list.lastElementChild?.scrollIntoView({ block: 'nearest' });
      comp.ta.value = ''; drafts.delete(key); setReply(null); comp.size();
      try {
        await api.post('/issues/' + key + '/comments', { Markdown: text, Mentions: mentions });
        st.pending = st.pending.filter(p => p !== tmp);
        await reload(true); changed();
      } catch (e) {
        st.pending = st.pending.filter(p => p !== tmp); renderComments();
        comp.ta.value = text; drafts.set(key, text); comp.size(); throw e;
      }
    } });
  comp.ta.addEventListener('input', () => drafts.set(key, comp.ta.value));
  const replyChip = h('div.reply-chip', { hidden: true });
  box.composer.append(replyChip, comp.el);
  function setReply(c) {
    st.reply = c;
    replyChip.hidden = !c;
    clear(replyChip);
    if (c) replyChip.append(h('span', 'Replying to ', h('b', c.Author)), h('button.btn.ghost.sm', { onclick: () => { const t = '@' + c.Author + ' '; if (comp.ta.value.startsWith(t)) comp.ta.value = comp.ta.value.slice(t.length); setReply(null); } }, '✕'));
  }
  function replyTo(c) {
    setTab('comments');
    setReply(c);
    const tag = '@' + c.Author + ' ';
    if (!comp.ta.value.startsWith(tag)) comp.ta.value = tag + comp.ta.value;
    if (c.AuthorID) comp.mentions.push({ AccountID: c.AuthorID, DisplayName: c.Author });
    comp.size(); comp.focus();
  }
  function composeComment() {
    setTab('comments');
    comp.focus();
    comp.el.scrollIntoView({ block: 'nearest' });
  }

  // ---- history tab: changes, time in status, development
  async function loadHistory() {
    if (st.hist) return;
    st.hist = 'loading';
    clear(panes.history).append(h('div.loading', 'Loading history…'));
    const [log, tis, dev] = await Promise.all([
      api.get('/issues/' + key + '/history').catch(e => e), api.get('/issues/' + key + '/timeinstatus').catch(() => null), api.get('/issues/' + key + '/dev').catch(() => null)]);
    if (dead) return;
    st.hist = log instanceof Error ? null : log; st.tis = tis; st.dev = dev;
    renderHistory(log instanceof Error ? log : null);
  }
  function renderHistory(err) {
    const p = clear(panes.history);
    if (err) return p.append(h('div.empty', 'Could not load history: ' + err.message, h('div', h('button.btn', { onclick: () => { st.hist = null; loadHistory(); } }, 'Retry'))));
    const tis = st.tis || [];
    if (tis.length) {
      const max = Math.max(...tis.map(t => t.Time), 1);
      p.append(h('div.sec-head', h('h3', 'Time in status')), h('div.tis', tis.map(t => h('div.tis-row' + (t.Now ? '.now' : ''),
        h('span.name', t.Status), h('span.track', h('i', { style: { width: Math.max(2, Math.round(100 * t.Time / max)) + '%' } })),
        h('span.dim', duration(t.Time / 1e9) + (t.Visits > 1 ? ' · ' + t.Visits + '×' : ''))))));
    }
    const dev = st.dev || [];
    if (dev.length) {
      p.append(h('div.sec-head', h('h3', 'Development')), h('div.rows', dev.map(d => h(d.URL ? 'a.row-link' : 'div.row-link', d.URL ? { href: safeHref(d.URL), target: '_blank', rel: 'noopener noreferrer' } : null,
        h('span.chip', d.Kind), h('span.clip', d.Name), d.Status && h('span.chip.st-' + d.Status.toLowerCase(), d.Status), d.Repo && h('span.dim.clip', d.Repo)))));
    }
    const log = (st.hist || []).slice().reverse();
    p.append(h('div.sec-head', h('h3', 'Changes'), h('span.dim', String(log.length))));
    if (!log.length) p.append(h('p.faint', 'Nothing recorded.'));
    const frag = document.createDocumentFragment();
    for (const e of log) {
      frag.append(h('div.chg', h('div.chg-head', ui.avatar(e.Who, null, 18), h('b', e.Who || 'Jira'), h('time.dim', { title: dateTime(e.When) }, ago(e.When))),
        (e.Changes && e.Changes.length ? e.Changes : [{ Field: '', From: '', To: e.What }]).map(c => h('div.chg-line', c.Field && h('span.fname', c.Field), c.From || c.To ? changeText(c) : null))));
    }
    p.append(frag);
  }
  const clip = (s, n = 160) => (s.length > n ? s.slice(0, n) + '…' : s);
  const changeText = c => (c.Field && /^description$/i.test(c.Field) ? h('span.dim', 'edited') : !c.Field ? h('span', c.To) : [
    c.From ? h('span.from', clip(c.From)) : h('span.faint', 'none'), h('span.arrow', '→'), c.To ? h('span.to', clip(c.To)) : h('span.faint', 'none')]);

  // ---- editor (description, comment edit, composer): textarea + mention completion
  function editor({ value = '', rows = 4, placeholder = '', label = 'Save', save, cancel, allowEmpty = false, mono = false, noCancel = false }) {
    const mentions = [];
    const ta = h('textarea.input.ed-ta' + (mono ? '.mono' : ''), { rows, placeholder, spellcheck: true });
    ta.value = value;
    const pop = h('div.mention-pop', { hidden: true, role: 'listbox' });
    const go = h('button.btn.primary', { onclick: () => run() }, label);
    const node = h('div.ed', ta, pop, h('div.ed-foot', h('span.dim.hint', 'ctrl+⏎ ' + label.toLowerCase() + (noCancel ? '' : ' · esc cancels')), h('span.spacer'),
      !noCancel && cancel && h('button.btn.ghost', { onclick: () => cancel() }, 'Cancel'), go));
    const size = () => { ta.style.height = 'auto'; ta.style.height = Math.min(ta.scrollHeight + 2, window.innerHeight * 0.6) + 'px'; };
    let busy = false;
    async function run() {
      if (busy || (!allowEmpty && !ta.value.trim())) return;
      busy = true; go.disabled = true; ta.readOnly = true;
      const text = ta.value;
      try { await save(text, mentions.filter(m => text.includes('@' + m.DisplayName))); } catch (e) { fail(e); } finally { busy = false; go.disabled = false; ta.readOnly = false; }
    }
    node._save = run;
    node._cancel = () => { pop.hidden = true; if (noCancel || !cancel) ta.blur(); else cancel(); };
    // mention popup
    let items = [], sel = 0, from = -1;
    const closePop = () => { pop.hidden = true; from = -1; };
    node._escape = () => { if (pop.hidden) return false; closePop(); return true; };
    const paint = () => {
      clear(pop).append(...items.map((u, i) => h('div.mp' + (i === sel ? '.sel' : ''), { role: 'option', onmousedown: e => { e.preventDefault(); accept(i); } }, ui.avatar(u.DisplayName, null, 18), u.DisplayName)));
      pop.hidden = !items.length;
    };
    function accept(i) {
      const u = items[i]; if (!u) return;
      const end = ta.selectionStart;
      const name = '@' + u.DisplayName + ' ';
      ta.value = ta.value.slice(0, from) + name + ta.value.slice(end);
      ta.selectionStart = ta.selectionEnd = from + name.length;
      if (u.AccountID && !mentions.some(m => m.AccountID === u.AccountID)) mentions.push({ AccountID: u.AccountID, DisplayName: u.DisplayName });
      closePop(); ta.dispatchEvent(new Event('input'));
    }
    const lookup = debounce(async (q, at) => {
      const local = [...people()].filter(([n]) => n.toLowerCase().includes(q.toLowerCase())).map(([n, id]) => ({ DisplayName: n, AccountID: id }));
      let remote = [];
      try {
        const r = await api.get('/users?issue=' + encodeURIComponent(key) + '&q=' + encodeURIComponent(q));
        remote = (Array.isArray(r) ? r : r.Users || r.users || []).map(u => ({ DisplayName: u.DisplayName || u.displayName, AccountID: u.AccountID || u.accountId }));
      } catch (e) { /* local names only */ }
      if (from !== at) return;
      const seen = new Set();
      items = [...remote, ...local].filter(u => u.DisplayName && u.AccountID && !seen.has(u.AccountID) && seen.add(u.AccountID)).slice(0, 8);
      sel = 0; paint();
    }, 120);
    ta.addEventListener('input', () => {
      size();
      const before = ta.value.slice(0, ta.selectionStart);
      const m = /(?:^|[\s(])@([^\s@]{0,30})$/u.exec(before);
      if (!m) return closePop();
      from = before.length - m[1].length - 1;
      lookup(m[1], from);
    });
    ta.addEventListener('keydown', e => {
      if (pop.hidden || e.ctrlKey || e.metaKey) return;
      const k = e.key;
      if (k === 'ArrowDown' || (k === 'Tab' && !e.shiftKey)) sel = (sel + 1) % items.length;
      else if (k === 'ArrowUp' || (k === 'Tab' && e.shiftKey)) sel = (sel - 1 + items.length) % items.length;
      else if (k === 'Enter') accept(sel);
      else return;
      e.preventDefault(); e.stopPropagation(); if (k !== 'Enter') paint();
    });
    ta.addEventListener('blur', () => setTimeout(closePop, 150));
    requestAnimationFrame(size);
    return { el: node, ta, mentions, size, focus: () => { ta.focus({ preventScroll: true }); ta.selectionStart = ta.selectionEnd = ta.value.length; size(); } };
  }
  const mentionNode = m => ({ type: 'mention', attrs: { id: m.AccountID, text: '@' + m.DisplayName } });

  // ---- data
  let gen = 0;
  async function reload(quiet) {
    const g = ++gen;
    try {
      const [iss, card] = await Promise.all([api.get('/issues/' + key + '?fresh=1', { fresh: true }), api.get('/issues/' + key + '/card', { fresh: true }).catch(() => st.card)]);
      if (dead || g !== gen) return;
      st.issue = iss; st.card = card; st.children = null; st.hist = null; st.weblinks = null; st.tis = null; st.dev = null;
      paint(); loadExtras();
      if (st.tab === 'history') loadHistory();
    } catch (e) { if (!quiet) fail(e); }
  }
  async function loadExtras() {
    const [kids, web] = await Promise.all([st.children ? null : api.get('/issues/' + key + '/children').catch(() => []), st.weblinks ? null : api.get('/issues/' + key + '/weblinks').catch(() => [])]);
    if (dead) return;
    if (kids) { st.children = kids; renderChildren(); }
    if (web) { st.weblinks = web; renderLinks(); }
  }
  function paint() {
    renderHead(); renderTabs(); renderFields(); renderDesc(); renderChildren(); renderLinks(); renderFiles(); renderComments();
  }

  renderHead(); renderTabs(); setTab('details');
  clear(panes.history);
  box.fields.append(h('div.loading', 'Loading ' + key + '…'));
  api.swr('/issues/' + key, iss => { if (!dead) { st.issue = iss; paint(); } }).then(() => { if (!dead) loadExtras(); })
    .catch(e => { if (!dead && !st.issue) clear(box.fields).append(h('div.empty', 'Could not load ' + key + ': ' + e.message)); });
  api.swr('/issues/' + key + '/card', c => { if (!dead) { st.card = c; renderFields(); renderHead(); } }).catch(() => {});

  const offChanged = bus.on('issue:changed', e => { if (e && e.key === key) reload(true); });
  const offFocus = bus.on('focus', () => reload(true));

  // ---- keys
  const G = 'Issue';
  scope.bind('Escape', e => {
    const ed = e.target.closest && e.target.closest('.ed');
    if (ed) { if (ed._escape && ed._escape()) return; if (ed._cancel) return ed._cancel(); }
    if (document.activeElement && document.activeElement.matches && document.activeElement.matches('input,textarea,select')) return document.activeElement.blur();
    goBack();
  }, full ? 'back' : 'close panel', { group: G, input: true });
  scope.bind('ctrl+Enter', e => { const ed = e.target.closest && e.target.closest('.ed'); if (ed && ed._save) ed._save(); }, 'save / send', { group: G, input: true });
  scope.bind(['j', 'ArrowDown'], () => (st.tab === 'comments' ? moveComment(1) : scroll.scrollBy({ top: 80 })), 'next comment / scroll down', { group: G });
  scope.bind(['k', 'ArrowUp'], () => (st.tab === 'comments' ? moveComment(-1) : scroll.scrollBy({ top: -80 })), 'previous comment / scroll up', { group: G });
  scope.bind('ctrl+d', () => scroll.scrollBy({ top: scroll.clientHeight / 2 }), 'half page down', { group: G, hidden: true });
  scope.bind('ctrl+u', () => scroll.scrollBy({ top: -scroll.clientHeight / 2 }), 'half page up', { group: G, hidden: true });
  scope.bind('c', composeComment, 'write a comment', { group: G });
  scope.bind('e', () => { const c = st.tab === 'comments' && focused(); if (c && isMine(c)) editComment(c); else edit('summary'); }, 'edit summary (or own focused comment)', { group: G });
  scope.bind('E', editDesc, 'edit description', { group: G });
  scope.bind('a', () => edit('assignee'), 'change assignee', { group: G });
  scope.bind('p', () => edit('priority'), 'change priority', { group: G });
  scope.bind('P', () => edit('points'), 'set story points', { group: G });
  scope.bind('l', () => edit('labels'), 'edit labels', { group: G });
  scope.bind('r', () => reload(true), 'refresh', { group: G });
  scope.bind('R', () => { const c = focused() || (st.issue && st.issue.Comments[st.issue.Comments.length - 1]); if (c) replyTo(c); }, 'reply to comment', { group: G });
  scope.bind('d', () => { const c = st.tab === 'comments' && focused(); if (c && isMine(c)) deleteComment(c); }, 'delete own comment', { group: G });
  scope.bind('L', addLink, 'link an issue', { group: G });
  scope.bind('[', () => bus.emit('issue:nav', { dir: -1, key }), 'previous issue in list', { group: G });
  scope.bind(']', () => bus.emit('issue:nav', { dir: 1, key }), 'next issue in list', { group: G });
  scope.bind('1', () => setTab('details'), 'details tab', { group: G });
  scope.bind('2', () => setTab('comments'), 'comments tab', { group: G });
  scope.bind('3', () => setTab('history'), 'history tab', { group: G });
  scope.bind('y', () => copy(key, key), 'copy key', { group: G });
  scope.bind('Y', () => copy(browseURL(), 'Link'), 'copy link', { group: G });
  scope.bind('o', () => window.open(browseURL(), '_blank', 'noopener'), 'open in Jira', { group: G });
  scope.bind('s', () => edit('status'), 'change status', { group: G });

  // ---- resize (panel only)
  if (!full) {
    const w0 = app.prefs.get('panelW', '');
    if (w0) document.documentElement.style.setProperty('--panel-w', w0 + 'px');
    const grip = root.querySelector('.iss-grip');
    grip.addEventListener('pointerdown', e => {
      e.preventDefault(); grip.setPointerCapture(e.pointerId); grip.classList.add('drag');
      const right = el.getBoundingClientRect().right;
      const move = ev => { document.documentElement.style.setProperty('--panel-w', Math.round(Math.max(360, Math.min(right - ev.clientX, window.innerWidth * 0.7))) + 'px'); };
      const up = () => {
        grip.removeEventListener('pointermove', move); grip.removeEventListener('pointerup', up); grip.classList.remove('drag');
        app.prefs.set('panelW', Math.round(el.getBoundingClientRect().width));
      };
      grip.addEventListener('pointermove', move); grip.addEventListener('pointerup', up);
    });
    grip.addEventListener('dblclick', () => { document.documentElement.style.removeProperty('--panel-w'); app.prefs.set('panelW', ''); });
  }
  scroll.focus({ preventScroll: true });

  return () => {
    dead = true; scope.dispose(); offChanged(); offFocus();
    drafts.set(key, comp.ta.value);
    if (!full) document.title = 'laneway';
  };
}

// Sets the nth task checkbox of markdown (outside code fences) done or not.
function flipTask(md, n, done) {
  if (!md) return md;
  let i = 0, fence = false;
  return md.split('\n').map(l => {
    if (/^\s*```/.test(l)) fence = !fence;
    if (fence) return l;
    return l.replace(/^(\s*[-*+]\s+)\[([ xX])\](\s)/, (m, pre, _box, sp) => (++i === n ? pre + (done ? '[x]' : '[ ]') + sp : m));
  }).join('\n');
}
