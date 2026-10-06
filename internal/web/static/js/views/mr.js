// A GitLab merge request's page (TUI: enter on one in D, then d): a head as the issue page's (its reference, title,
// state, branches, the Jira issues it names, the pipeline, approvals and the review's buttons), then two tabs.
// Overview: what it is (merge status, people, the pipeline stage by stage, description) and the discussions on the
// merge request as a whole, outdated ones too, with a comment of your own. Changes: the files beside the diff,
// every line highlighted by the server (GET /api/gitlab/diff?url=), the inline threads under their lines.
// Reply and Resolve on a thread, a click on a line's number starts one there; all of it into your pending review
// (POST /api/gitlab/note, PUT /api/gitlab/resolve) until S submits it. M merges it (POST /api/gitlab/merge), GitLab's
// defaults first; e edits it (PUT /api/gitlab/mr), as a click on its title, a field or Mark as draft does. 1/2 the
// tabs; on Changes j/k or ]/[ file, n/N thread, z folds the file, Z all, e the whole file, v a version; i the issue it
// names, beside; o GitLab, r reload, esc back. A host without a token says how to sign in.
// #/mr?url=LINK[&tab=changes][&version=N].
import { h, clear, delegate } from '../lib/dom.js';
import { css } from '../lib/css.js';
import { icon } from '../lib/icons.js';
import { ago, isZero, plural, duration } from '../lib/fmt.js';
import { render as md } from '../lib/md.js';
import { stateBadge, issueKeys, pipeline, pipelineMini, approvals, mrHref, glyph, running, signInHelp } from '../lib/mr.js';

const raw = s => { const t = document.createElement('template'); t.innerHTML = s; return t.content; };
const safe = u => (/^https?:\/\//i.test(u || '') ? u : '');
const catOf = c => (c.Done ? 'done' : c.InProgress ? 'indeterminate' : 'new');

export default function mount(el, { app, scope, query }) {
  css('issue'); css('diff'); css('dev');
  const { api, ui } = app;
  const jira = { site: app.session && app.session.baseURL, onKey: k => app.panel.open(k) }; // its Jira links open beside
  const url = query.url || '';
  let mr = null, mrErr = null, data = null, dead = false, cur = 0, version = +query.version || 0, tab = query.tab === 'changes' ? 'changes' : 'overview', jobId = +query.job || 0;
  const cards = new Map(); // a named issue's card, by key: {card} | {err}
  const folded = new Set();
  const whole = new Map(); // e: file index → that file with its unchanged lines filled in
  const head = h('header.iss-head.mrp-head'), tabs = h('nav.iss-tabs', { role: 'tablist' });
  const over = h('section.mrp-over'), files = h('nav.df-files', { 'aria-label': 'Changed files' }), body = h('div.df-body');
  const changesBar = h('div.mrp-cbar'), changes = h('section.mrp-changes', changesBar, h('div.df', files, body));
  el.append(h('div.mrp', head, tabs, over, changes));

  // ---- the head
  const label = () => (mr ? mr.Repo + '!' + mr.Number : data ? data.Label : '');
  const titleText = () => (mr && mr.Title) || (data && data.Title) || '';
  const drafts = () => (data && data.Drafts) || [];
  const keys = () => (mr ? issueKeys(mr, app.session && app.session.projects) : []);
  async function copy(text, what) {
    try { await navigator.clipboard.writeText(text); ui.toast(what + ' copied'); } catch (e) { ui.toast('Could not copy: ' + text, { kind: 'err' }); }
  }
  // issueChip is a Jira issue the merge request names: its key, status and summary; a click shows it beside.
  function issueChip(k) {
    const st = cards.get(k);
    if (!st) {
      cards.set(k, {});
      api.get('/issues/' + k + '/card').then(card => { cards.set(k, { card }); if (!dead) renderHead(); }, err => { cards.set(k, { err }); if (!dead) renderHead(); });
    }
    const c = st && st.card;
    return h('button.mrp-issue', { title: 'Show ' + k + ' beside (i)', onclick: () => app.panel.open(k) },
      h('span.mrp-ikey', k), c ? ui.statusPill(c.Status, catOf(c)) : null, c ? h('span.clip', c.Summary) : null);
  }
  function renderHead() {
    const t = titleText(), open = isOpen();
    clear(head).append(...[
      h('div.iss-top',
        h('span.chip', 'Merge request'),
        h('button.iss-key.btn.link', { title: 'Copy link', onclick: () => copy(url, 'Link') }, label() || '…'),
        ...keys().slice(0, 3).map(issueChip),
        h('span.spacer'),
        open ? h('button.btn.ghost.sm', { title: 'GitLab merges no draft', onclick: () => edit('draft') }, mr.Draft ? 'Mark as ready' : 'Mark as draft') : null,
        safe(url) ? h('a.btn.ghost.sm', { href: safe(url), target: '_blank', rel: 'noopener noreferrer', title: 'Open in GitLab (o)' }, 'GitLab', icon('external-link')) : null,
        h('button.btn.ghost.sm', { title: 'Back (esc)', onclick: back }, icon('arrow-left'), 'Back')),
      open ? h('h1.iss-title', { title: 'Edit the title', onclick: () => edit('title') }, t) : h('h1.iss-title.mrp-title', t || (mrErr ? '' : '…')),
      h('div.iss-sub.mrp-sub',
        mr ? stateBadge(mr) : null,
        mr ? h('span.mrp-flow', mr.Author ? h('b', mr.Author) : null, mr.Author ? ' wants to merge ' : 'Merges ', h('code.dv-mono', mr.SourceBranch), ' into ', h('code.dv-mono', mr.TargetBranch)) : null,
        mr && !isZero(mr.UpdatedAt) ? h('span.dim', 'updated ' + ago(mr.UpdatedAt)) : null,
        mr && mr.Checks ? h('button.mrp-link', { title: 'The pipeline, stage by stage', onclick: () => { setTab('overview'); const p = over.querySelector('.mrp-pipeline'); if (p) p.scrollIntoView({ block: 'start', behavior: 'smooth' }); } }, pipelineMini(mr.Checks)) : null,
        mr ? approvals(mr, ui) : null),
      mrErr ? signInHelp(mrErr) || h('div.dv-err', mrErr.message || String(mrErr)) : null].filter(Boolean));
    document.title = label() + (t ? ' ' + t : '') + ' · laneway';
  }
  function renderTabs() {
    const fs = (data && data.Files) || [];
    const ts = [['overview', 'Overview', '1'], ['changes', 'Changes' + (fs.length ? ' ' + fs.length : ''), '2']];
    const n = drafts().length;
    // The review's buttons at the tabs' end: beside the diff, on either tab.
    clear(tabs).append(...ts.map(([id, text, k]) => h('button.tab' + (tab === id ? '.on' : ''), { role: 'tab', 'aria-selected': tab === id, onclick: () => setTab(id) }, text, h('kbd', k))),
      h('span.spacer'), h('div.mrp-acts',
        h('button.btn.ghost.sm', { title: 'An agent reviews it in a worktree of its branch; its notes come back as pending (C)', onclick: agentReview }, icon('zap'), 'Agent review'),
        h('button.btn.sm', { title: 'Approve without a review (A)', onclick: () => review(true) }, icon('check'), 'Approve'),
        isOpen() ? h('button.btn.sm', { title: mr.Mergeable ? 'Merge (M)' : 'Not ready to merge: ' + mr.MergeStatus, disabled: !mr.Mergeable, onclick: merge }, icon('git-merge'), 'Merge') : null,
        h('button.btn.primary.sm', { title: 'Submit your review: comment, approve or request changes (S)', onclick: () => review(false) }, n ? 'Submit review · ' + plural(n, 'pending note') : 'Submit review')));
  }
  function setTab(t) {
    tab = t;
    over.hidden = t !== 'overview'; changes.hidden = t !== 'changes';
    syncURL();
    renderTabs();
  }
  const syncURL = () => history.replaceState(null, '', mrHref(url, tab === 'changes' ? 'changes' : '') + (version ? '&version=' + version : '') + (jobId ? '&job=' + jobId : ''));
  function back() { if (history.length > 1) history.back(); else app.go('/mrs'); }

  // ---- the pipeline, live: polled while it runs; a job's log under it, followed while the job runs
  const openStages = new Set(); // the stages listing their passed jobs
  const pipelineBox = () => pipeline(mr.Checks, { open: openStages, job: jobId, onJob: j => showJob(j.ID),
    onStage: n => { if (!openStages.delete(n)) openStages.add(n); livePipeline(); } });
  // livePipeline repaints what the pipeline moves (the head, the merge status, the pipeline) and leaves the rest,
  // a comment being typed included, as it is.
  function livePipeline() {
    renderHead();
    const box = over.querySelector('.mrp-pipeline'); if (box && mr.Checks) box.replaceChildren(pipelineBox());
    const f = over.querySelector('.mrp-fields'); if (f) f.replaceWith(fieldsEl());
  }
  // refreshPipeline reads the merge request again, past the cache, for its pipeline.
  async function refreshPipeline() {
    try { mr = await api.get('/gitlab/mr?url=' + encodeURIComponent(url) + '&fresh=1', { fresh: true }); if (!dead) livePipeline(); } catch (e) { /* the next try */ }
  }
  let pipeTimer = 0, jobTimer = 0;
  const later = (fn, ms) => setTimeout(() => { if (!dead) (document.hidden ? later(fn, ms) : fn()); }, ms);
  function watchPipeline() {
    clearTimeout(pipeTimer);
    if (!mr || !mr.Checks || !running(mr.Checks.Status)) return;
    pipeTimer = later(async () => { await refreshPipeline(); watchPipeline(); }, 5000);
  }
  // jobView is the log of the job jobId: its state, its lines (followed to the end while it runs), GitLab, close.
  const jobView = (() => {
    const headEl = h('div.jl-head'), bodyEl = h('div.jl-body', { tabindex: 0 }), followBtn = h('button.btn.sm.jl-follow', { hidden: true, onclick: () => { bodyEl.scrollTop = bodyEl.scrollHeight; followBtn.hidden = true; } }, icon('arrow-down'), 'Follow');
    const el = h('section.jl', { 'aria-label': 'Job log' }, headEl, bodyEl, followBtn);
    bodyEl.addEventListener('scroll', () => { followBtn.hidden = !el.dataset.live || bodyEl.scrollHeight - bodyEl.scrollTop - bodyEl.clientHeight < 24; });
    return {
      el,
      loading() { headEl.replaceChildren(h('span.dim', 'Reading the job…')); bodyEl.replaceChildren(); },
      error(e) { headEl.replaceChildren(h('span.dvt-err', e.message || String(e)), h('span.spacer'), closeBtn()); },
      paint(j, first) {
        const live = !j.Done, end = first || bodyEl.scrollHeight - bodyEl.scrollTop - bodyEl.clientHeight < 24;
        el.dataset.live = live ? '1' : '';
        headEl.replaceChildren(...[glyph(j.Status), h('b', j.Name), h('span.dim', j.Stage), h('span.dim', j.Status === 'success' ? 'passed' : j.Status),
          j.Duration ? h('span.dim', duration(j.Duration)) : null, live ? h('span.jl-live', h('i'), 'following') : null, h('span.spacer'),
          j.WebURL ? h('a.btn.ghost.sm', { href: j.WebURL, target: '_blank', rel: 'noopener noreferrer', title: 'The job in GitLab' }, 'GitLab', icon('external-link')) : null, closeBtn()].filter(Boolean));
        bodyEl.replaceChildren(...[j.Truncated ? h('div.jl-cut', 'The log\'s start is cut: GitLab has all of it.') : null, raw(j.HTML || '')].filter(Boolean));
        if (!j.HTML) bodyEl.append(h('div.jl-cut', live ? 'Waiting for output…' : 'No output.'));
        if (end) bodyEl.scrollTop = bodyEl.scrollHeight;
        followBtn.hidden = !live || end;
      },
    };
  })();
  const closeBtn = () => h('button.btn.ghost.sm', { title: 'Close the log (esc)', 'aria-label': 'Close the log', onclick: closeJob }, icon('x'));
  async function showJob(id) {
    const fresh = id !== jobId;
    jobId = id; syncURL();
    if (tab !== 'overview') setTab('overview');
    clearTimeout(jobTimer);
    if (fresh) jobView.loading();
    const box = over.querySelector('.mrp-pipeline');
    if (box) { box.replaceChildren(pipelineBox()); if (!jobView.el.isConnected) box.after(jobView.el); }
    if (fresh) jobView.el.scrollIntoView({ block: 'nearest', behavior: 'smooth' });
    readJob(true);
  }
  async function readJob(first) {
    const id = jobId;
    let j;
    try { j = await api.get('/gitlab/job?url=' + encodeURIComponent(url) + '&job=' + id, { fresh: true }); }
    catch (e) { if (!dead && id === jobId) jobView.error(e); return; }
    if (dead || id !== jobId) return;
    jobView.paint(j, first);
    if (!j.Done) jobTimer = later(() => readJob(false), 2000);
    else if (!first) refreshPipeline().then(watchPipeline); // it just ended: the pipeline moved
  }
  function closeJob() {
    jobId = 0; clearTimeout(jobTimer); syncURL();
    jobView.el.remove();
    const box = over.querySelector('.mrp-pipeline'); if (box && mr && mr.Checks) box.replaceChildren(pipelineBox());
  }

  // ---- overview
  function renderOverview() {
    const kids = [];
    if (drafts().length) kids.push(h('div.mrp-pending', icon('triangle-alert'), h('span', h('b', plural(drafts().length, 'pending note')), ': only you see them until you submit your review.')));
    if (mr) {
      kids.push(fieldsEl());
      if (mr.Checks) kids.push(h('div.sec-head', h('h3', 'Pipeline'), h('span.dim', 'a job shows its log')), h('div.mrp-pipeline', pipelineBox()), jobId ? jobView.el : null);
      kids.push(h('div.sec-head', h('h3', 'Description'), isOpen() ? h('button.btn.ghost.sm', { onclick: () => edit('description') }, 'Edit') : null), (mr.Description || '').trim() ? h('div.md.mrp-desc', md(mr.Description, jira)) : h('div.faint', 'No description.'));
    } else if (!mrErr) kids.push(h('div.df-msg', 'Loading the merge request…'));
    kids.push(...discussions());
    over.replaceChildren(h('div.mrp-page', kids));
  }
  // fieldsEl is the overview's grid: what the merge request is, as the issue page's fields.
  function fieldsEl() {
    const fld = (k, ...v) => (v.some(Boolean) ? h('div.fld.ro', h('span.k', k), h('span.v', ...v)) : null);
    // efld is a field a click edits while the merge request is open.
    const efld = (k, field, ...v) => (isOpen() ? h('button.fld', { title: 'Edit ' + k.toLowerCase(), onclick: () => edit(field) }, h('span.k', k), h('span.v', ...(v.some(Boolean) ? v : [h('span.faint', 'None')]))) : fld(k, ...v));
    const people = list => (list && list.length ? list.map(p => h('span.who', ui.avatar(p, '', 18), p)) : null);
    const ts = (data && data.Threads) || [], open = ts.filter(t => t.Resolvable && !t.Resolved).length, done = ts.filter(t => t.Resolved).length;
    const fs = (data && data.Files) || [], add = fs.reduce((s, f) => s + (f.Add || 0), 0), del = fs.reduce((s, f) => s + (f.Del || 0), 0);
    const merge = mr.State !== 'opened' ? null : mr.HasConflicts ? h('span.dvt-err', mr.MergeStatus) : mr.Mergeable ? h('span.dvt-ok', icon('check'), mr.MergeStatus) : mr.MergeStatus;
    return h('div.fields.mrp-fields',
      fld('Merge', merge),
      fld('Author', ...(people(mr.Author ? [mr.Author] : []) || [])),
      efld('Reviewers', 'reviewers', ...(people(mr.Reviewers) || [])),
      efld('Assignees', 'assignees', ...(people(mr.Assignees) || [])),
      fld('Approvals', approvals(mr, ui)),
      fld('Changes', data ? h('span', plural(fs.length, 'file'), ' ', h('span.df-add', '+' + add), ' ', h('span.df-del', '−' + del)) : mr.ChangesCount && plural(+mr.ChangesCount || 0, 'file')),
      fld('Threads', ts.length ? [open && open + ' open', done && done + ' resolved'].filter(Boolean).join(' · ') || plural(ts.length, 'comment') : null),
      efld('Labels', 'labels', (mr.Labels || []).length ? h('span.chips', mr.Labels.map(l => h('span.chip', l))) : null),
      efld('Target branch', 'target', mr.TargetBranch ? h('code.dv-mono', mr.TargetBranch) : null));
  }
  // discussions are the threads on the merge request as a whole and the outdated ones, with your pending general
  // notes, then a comment of your own.
  function discussions() {
    if (!data) return [];
    const rest = (data.Threads || []).filter(t => !t.Inline || t.Outdated), general = drafts().filter(d => !d.ReplyTo && !d.Path);
    const add = h('button.btn.sm', { onclick: e => { const c = composer({}, 'A comment on the merge request…'); e.currentTarget.replaceWith(c); } }, icon('message-square'), 'Comment');
    return [h('div.sec-head', h('h3', 'Discussions'), rest.length ? h('span.dim', plural(rest.length, 'thread')) : null),
      h('div.mrp-threads', rest.map(note), general.map(pending), rest.length || general.length ? null : h('div.faint', 'No discussions yet.'), h('div.mrp-add', add))];
  }

  // ---- threads and notes (both tabs)
  // A pending note of your review: only you see it until the review is submitted.
  const pending = d => {
    const el = h('div.df-note.pending', h('div', h('span.df-who', 'you'), h('span.df-when', 'pending'), h('div.md', md(d.Body, jira))),
      h('div.df-acts', h('button.btn.ghost.sm', { onclick: () => editDraft(d, el) }, 'Edit'), h('button.btn.ghost.sm', { onclick: () => dropDraft(d) }, 'Drop')));
    return el;
  };
  // Edit: the pending note's text in place, an agent's finding to reword before the review goes out.
  function editDraft(d, el) {
    const ta = h('textarea.input', { rows: 4 }); ta.value = d.Body;
    const save = async () => {
      if (!ta.value.trim()) return;
      try { await api.put('/gitlab/draft?url=' + encodeURIComponent(url) + '&draft=' + d.ID, { Body: ta.value }); load(false); } catch (e) { ui.errToast(e); }
    };
    ta.addEventListener('keydown', e => { if (e.key === 'Enter' && (e.ctrlKey || e.metaKey)) { e.preventDefault(); save(); } else if (e.key === 'Escape') { e.stopPropagation(); paint(); } });
    el.replaceChildren(ta, h('div.df-acts', h('button.btn.sm', { onclick: save }, 'Save'), h('button.btn.ghost.sm', { onclick: () => paint() }, 'Cancel')));
    ta.focus();
  }
  async function dropDraft(d) {
    try { await api.del('/gitlab/draft?url=' + encodeURIComponent(url) + '&draft=' + d.ID); load(false); } catch (e) { ui.errToast(e); }
  }
  const note = t => {
    const el = h('div.df-note' + (t.Resolved ? '.resolved' : ''), { dataset: { thread: t.ID } },
      t.Outdated ? h('div.df-when', 'outdated · ' + t.Path + ':' + (t.NewLine || t.OldLine) + (t.NewLine ? '' : ' (removed)')) : null,
      (t.Notes || []).map(n => h('div.df-msg-row', ui.avatar(n.Author, '', 20), h('div', h('span.df-who', n.Author), isZero(n.Created) ? null : h('span.df-when', ago(n.Created)), h('div.md', md(n.Body, jira))))),
      h('div.df-acts', h('button.btn.ghost.sm', { onclick: () => { const c = composer({ ReplyTo: t.ID }, 'Reply…'); c.classList.add('reply'); el.append(c); } }, 'Reply'),
        t.Resolvable || t.Resolved ? h('button.btn.ghost.sm', { onclick: () => resolve(t) }, t.Resolved ? 'Reopen' : 'Resolve') : null,
        t.Resolved ? h('span.df-when', 'resolved') : null));
    for (const d of drafts()) if (d.ReplyTo === t.ID) el.append(pending(d));
    return el;
  };
  // composer posts form (a reply, a new thread's position, or nothing: a comment) with the text typed; ctrl+enter
  // posts, esc drops it.
  function composer(form, placeholder, lines) {
    const ta = h('textarea.input', { rows: 3, placeholder });
    // Suggest: the lines' new side in a suggestion block to edit (a removed line has none).
    const suggest = lines && lines.every(l => l.K !== '-') ? h('button.btn.ghost.sm', { onclick: () => {
      ta.value = '```suggestion:-' + (lines.length - 1) + '+0\n' + lines.map(l => l.T).join('\n') + '\n```'; ta.focus();
    } }, 'Suggest') : null;
    const post = async () => {
      const text = ta.value.trim(); if (!text) return cancel();
      try { await api.post('/gitlab/note?url=' + encodeURIComponent(url), { ...form, Body: text }); ui.toast('In your review: Submit review publishes it', { kind: 'ok' }); load(false); }
      catch (e) { ui.errToast(e); }
    };
    const cancel = () => (form.ReplyTo || form.NewPath || form.OldPath ? box.remove() : renderOverview());
    ta.addEventListener('keydown', e => { if (e.key === 'Enter' && (e.ctrlKey || e.metaKey)) { e.preventDefault(); post(); } else if (e.key === 'Escape') { e.stopPropagation(); cancel(); } });
    const box = h('div.df-compose', ta, h('div.df-acts', h('button.btn.primary.sm', { onclick: post }, form.ReplyTo ? 'Reply' : 'Add to review'), suggest, h('button.btn.ghost.sm', { onclick: cancel }, 'Cancel'),
      h('span.df-when', 'ctrl+enter adds it' + (form.Range || form.ReplyTo || !form.NewPath ? '' : ' · shift+click another line number: a range'))));
    setTimeout(() => ta.focus());
    return box;
  }
  // A click on a line's number: a note on it; with shift, on the range from the line clicked last (same file).
  let lastAt = null;
  const lpos = l => ({ OldLine: l.K === '+' ? 0 : l.O, NewLine: l.K === '-' ? 0 : l.N, OldPos: l.OP, NewPos: l.NP });
  function openAt(shift, fi, f, l, at_, row) {
    for (const c of body.querySelectorAll('.df-compose:not(.reply)')) c.remove();
    let form = at_, lines = [l], where = 'line ' + (l.N || l.O);
    if (shift && lastAt && lastAt.fi === fi && lastAt.l !== l) {
      const ls = f.Lines || [], a = ls.indexOf(lastAt.l), b = ls.indexOf(l), from = Math.min(a, b), to = Math.max(a, b);
      lines = ls.slice(from, to + 1).filter(x => x.K !== '@' && x.K !== '\\');
      const end = ls[to];
      form = { OldPath: at_.OldPath, NewPath: at_.NewPath, OldLine: end.K === '+' ? 0 : end.O, NewLine: end.K === '-' ? 0 : end.N, Range: { Start: lpos(ls[from]), End: lpos(end) } };
      where = 'lines ' + (ls[from].N || ls[from].O) + '–' + (end.N || end.O);
      row = body.querySelector('#df-' + fi + ' .df-row[data-line="' + to + '"]') || row;
    } else lastAt = { fi, l };
    row.after(composer(form, 'Note on ' + where + '…', lines));
  }
  async function resolve(t) {
    try { await api.put('/gitlab/resolve?url=' + encodeURIComponent(url) + '&thread=' + encodeURIComponent(t.ID), { Resolved: !t.Resolved }); ui.toast(t.Resolved ? 'Thread reopened' : 'Thread resolved', { kind: 'ok' }); load(false); }
    catch (e) { ui.errToast(e); }
  }

  // ---- changes
  function threadsAt(path) {
    const m = new Map();
    for (const t of (data.Threads || [])) if (t.Inline && !t.Outdated && t.Path === path) {
      const k = t.NewLine ? 'n' + t.NewLine : 'o' + t.OldLine;
      if (!m.has(k)) m.set(k, []);
      m.get(k).push(t);
    }
    return m;
  }
  function section(f, i) {
    const at = threadsAt(f.Path), n = [...at.values()].reduce((s, l) => s + l.length, 0);
    const head_ = h('div.df-head', { dataset: { file: i } }, h('span.dv-caret', icon('chevron-right')), h('b.clip', f.Path), f.Renamed && f.OldPath !== f.Path ? h('span.df-dim', '← ' + f.OldPath) : null,
      f.New ? h('span.df-dim', 'new') : null, f.Deleted ? h('span.df-dim', 'deleted') : null, f.Generated ? h('span.df-dim', 'generated') : null,
      h('span.spacer'), n ? h('span.df-threads', icon('message-square'), ' ' + n) : null, h('span.df-add', '+' + f.Add), h('span.df-del', '−' + f.Del),
      f.New || f.Deleted || f.Binary || f.TooLarge ? null : h('button.btn.ghost.sm', { title: 'e', onclick: e => { e.stopPropagation(); expand(i); } }, whole.has(i) ? 'Changes only' : 'Whole file'));
    const sec = h('section.df-sec' + (folded.has(i) ? '.folded' : ''), { id: 'df-' + i }, head_);
    if (f.Binary) { sec.append(h('div.df-msg', 'binary file, not shown')); return sec; }
    if (f.TooLarge) { sec.append(h('div.df-msg', 'too large for GitLab to send: ', h('a', { href: safe(url) + '/diffs', target: '_blank', rel: 'noopener noreferrer' }, 'open it in GitLab'))); return sec; }
    const rows = h('div.df-rows');
    for (const l of f.Lines || []) {
      const cls = l.K === '+' ? '.add' : l.K === '-' ? '.del' : l.K === '@' ? '.hunk' : l.K === '\\' ? '.meta' : '';
      const code = h('span.df-code'); code.append(raw(l.H || ''));
      const at_ = l.K === '@' || l.K === '\\' ? null : { OldPath: f.OldPath || f.Path, NewPath: f.Path, OldLine: l.K === '+' ? 0 : l.O, NewLine: l.K === '-' ? 0 : l.N };
      // A line a note can go on: hovering it shows a + at the gutter's edge; it, or either number, starts one.
      const can = at_ && version === 0, tip = 'Add a note on this line (shift: a range from the last one you clicked)';
      const plus = can ? h('button.df-plus', { title: tip, 'aria-label': 'Add a note on line ' + (l.N || l.O) }, icon('plus')) : null;
      const row = h('div.df-row' + cls + (can ? '.can' : ''), { dataset: { line: (f.Lines || []).indexOf(l) } }, h('span.df-no', { title: can ? tip : '' }, l.O || ''), h('span.df-no', { title: can ? tip : '' }, l.N || '', plus), h('span.df-mk', l.K === '+' || l.K === '-' ? l.K : ' '), code);
      if (can) for (const no of row.querySelectorAll('.df-no')) no.addEventListener('click', e => openAt(e.shiftKey, i, f, l, at_, row));
      rows.append(row);
      const ts = (l.N && at.get('n' + l.N)) || (l.K === '-' && l.O && at.get('o' + l.O));
      if (ts && l.K !== '@') { for (const t of ts) rows.append(note(t)); at.delete(l.N ? 'n' + l.N : 'o' + l.O); }
      if (l.K !== '@') for (const d of drafts()) if (!d.ReplyTo && d.Path === f.Path && (l.K === '-' ? !d.NewLine && d.OldLine === l.O : d.NewLine && d.NewLine === l.N)) rows.append(pending(d));
    }
    sec.append(rows);
    return sec;
  }
  function renderChanges() {
    if (!data) return;
    const fs = data.Files || [], add = fs.reduce((s, f) => s + (f.Add || 0), 0), del = fs.reduce((s, f) => s + (f.Del || 0), 0);
    const inline = (data.Threads || []).filter(t => t.Inline && !t.Outdated).length, general = (data.Threads || []).length - inline;
    clear(changesBar).append(...[h('span', plural(fs.length, 'file')), h('span.df-add', '+' + add), h('span.df-del', '−' + del),
      inline ? h('span.dim', plural(inline, 'thread') + ' on the lines') : null,
      general ? h('button.btn.link', { onclick: () => setTab('overview') }, plural(general, 'discussion') + ' on Overview') : null,
      version ? h('span.chip', 'an earlier version: no threads') : null,
      h('span.spacer'), picker, h('button.btn.ghost.sm', { title: 'Fold or unfold every file (Z)', onclick: foldAll }, 'Fold all'), h('button.btn.ghost.sm', { title: 'Reload (r)', onclick: () => load(true) }, icon('refresh-cw'))].filter(Boolean));
    files.replaceChildren(...fs.map((f, i) => h('div.df-file-link' + (i === cur ? '.cur' : ''), { dataset: { file: i }, title: f.Path },
      h('span.df-path', f.Path), h('span.df-add', '+' + f.Add), h('span.df-del', '−' + f.Del))));
    const secs = fs.map((f, i) => section(whole.get(i) || f, i));
    if (data.Truncated) secs.push(h('div.df-msg', 'GitLab truncated this diff: the rest is only on ', h('a', { href: safe(url) + '/diffs', target: '_blank', rel: 'noopener noreferrer' }, 'GitLab')));
    if (!secs.length) secs.push(h('div.df-msg', 'This merge request has no diff.'));
    body.replaceChildren(...secs);
  }
  function paint() { renderHead(); renderTabs(); renderOverview(); renderChanges(); }

  async function load(fresh) {
    if (!data) body.replaceChildren(h('div.df-msg', 'Loading the diff…'));
    const q = encodeURIComponent(url) + (fresh ? '&fresh=1' : '');
    const gotMR = api.get('/gitlab/mr?url=' + q, { fresh: true }).then(m => { mr = m; mrErr = null; }, e => { mrErr = e; });
    const gotDiff = api.get('/gitlab/diff?url=' + q + (version ? '&version=' + version : ''), { fresh: true }).then(d => { data = d; whole.clear(); },
      e => { if (!dead) body.replaceChildren(h('div.df-msg', e.message, ' ', safe(url) ? h('a', { href: safe(url), target: '_blank', rel: 'noopener noreferrer' }, 'Open in GitLab') : null)); });
    await gotMR; if (!dead && mrErr && mrErr.signin) { tabs.hidden = over.hidden = changes.hidden = true; renderHead(); return; } // how to sign in, alone
    if (!dead) { tabs.hidden = false; setTab(tab); renderHead(); renderTabs(); renderOverview(); watchPipeline(); if (jobId && !jobView.el.dataset.read) { jobView.el.dataset.read = '1'; showJob(jobId); } }
    await gotDiff; if (!dead && data) { versionPick(); paint(); }
  }
  function go(i) {
    const n = (data && data.Files || []).length; if (!n) return;
    if (tab !== 'changes') setTab('changes');
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
  function foldAll() { const all = (data && data.Files || []).every((_, i) => folded.has(i)); (data && data.Files || []).forEach((_, i) => fold(i, !all)); }
  function thread(d) {
    const ns = [...body.querySelectorAll('.df-note')]; if (!ns.length) return ui.toast('No inline threads');
    const top = body.getBoundingClientRect().top + 8;
    const next = d > 0 ? ns.find(n => n.getBoundingClientRect().top > top + 1) : ns.reverse().find(n => n.getBoundingClientRect().top < top - 1);
    if (next) next.scrollIntoView({ block: 'start' }); else ui.toast(d > 0 ? 'No further threads' : 'No earlier threads');
  }
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
    syncURL();
    load(false);
  }
  // S: publish the pending review with a verdict and a summary; A: approve on its own.
  const VERDICT = { comment: 'Comment', approve: 'Approve', changes: 'Request changes' };
  async function review(only) {
    let verdict = 'approve', summary = '';
    if (!only) {
      verdict = await ui.pick({ title: 'Submit your review' + (drafts().length ? ': ' + plural(drafts().length, 'pending note') : ''), items: Object.keys(VERDICT), label: v => VERDICT[v] });
      if (!verdict) return;
      summary = await ui.prompt({ title: VERDICT[verdict] + ': a summary (or nothing)', multiline: true, ok: 'Submit' });
      if (summary == null) return;
    }
    try {
      await api.post('/gitlab/review?url=' + encodeURIComponent(url), { Verdict: verdict, Summary: summary, Only: !!only });
      ui.toast(only ? 'Approved' : { comment: 'Review submitted', approve: 'Review submitted, approved', changes: 'Review submitted, changes requested' }[verdict], { kind: 'ok' });
      load(true);
    } catch (e) { ui.errToast(e); }
  }
  // ---- e: an edit; M: the merge
  const isOpen = () => !!mr && mr.State === 'opened';
  const q = () => '?url=' + encodeURIComponent(url);
  async function save(patch, what) {
    try { mr = await api.put('/gitlab/mr' + q(), patch); ui.toast(what, { kind: 'ok' }); if (!dead) { renderHead(); renderTabs(); renderOverview(); } }
    catch (e) { ui.errToast(e); }
  }
  const EDITS = { title: 'Title', draft: 'Draft or ready', reviewers: 'Reviewers', assignees: 'Assignees', labels: 'Labels', target: 'Target branch', description: 'Description' };
  async function edit(field) {
    if (!isOpen()) return ui.toast(mr ? 'It is ' + mr.State : 'Still reading it');
    if (!field) field = await ui.pick({ title: 'Edit ' + label(), items: Object.keys(EDITS), label: f => EDITS[f] });
    if (field === 'title' || field === 'target') {
      const k = field === 'title' ? 'Title' : 'TargetBranch', v = await ui.prompt({ title: EDITS[field], value: mr[k] || '', ok: 'Save' });
      if (v && v.trim() && v.trim() !== mr[k]) save({ [k]: v.trim() }, EDITS[field] + ' changed');
    } else if (field === 'description') {
      const v = await ui.prompt({ title: 'Description', value: mr.Description || '', multiline: true, ok: 'Save' });
      if (v != null && v !== mr.Description) save({ Description: v }, 'Description saved');
    } else if (field === 'draft') save({ Draft: !mr.Draft }, mr.Draft ? 'Marked as ready' : 'Marked as draft');
    else if (field === 'labels') {
      const have = mr.Labels || [], all = api.get('/gitlab/labels' + q()).then(ls => [...new Set([...ls, ...have])]);
      const r = await ui.pick({ title: 'Labels of ' + label(), items: all, multi: true, selected: have, placeholder: 'Labels…' });
      if (r) save({ Labels: r }, 'Labels changed');
    } else if (field === 'reviewers' || field === 'assignees') {
      const [ik, nk] = field === 'reviewers' ? ['ReviewerIDs', 'Reviewers'] : ['AssigneeIDs', 'Assignees'];
      const ids = mr[ik] || [], names = new Map(ids.map((id, i) => [id, (mr[nk] || [])[i]]));
      const all = api.get('/gitlab/members' + q()).then(ms => { for (const m of ms) names.set(m.ID, m.Name); return [...new Set([...ms.map(m => m.ID), ...ids])]; });
      const r = await ui.pick({ title: EDITS[field] + ' of ' + label(), items: all, multi: true, selected: ids, label: id => names.get(id) || String(id), placeholder: 'People…' });
      if (r) save({ [ik]: r }, EDITS[field] + ' changed');
    }
  }
  async function merge() {
    let m;
    try { m = await api.get('/gitlab/merge' + q(), { fresh: true }); } catch (e) { return ui.errToast(e); }
    if (m.Ready) return ui.toast(m.Ready);
    const c = await ui.pick({ title: m.Title, items: m.Choices, label: c => c.Label, detail: c => (c === m.Choices[0] ? 'GitLab\'s default' : '') });
    if (!c) return;
    try { mr = await api.post('/gitlab/merge' + q(), { Squash: c.Squash, DeleteBranch: c.DeleteBranch }); ui.toast('Merged ' + label(), { kind: 'ok' }); if (!dead) { renderHead(); renderTabs(); renderOverview(); } }
    catch (e) { ui.errToast(e); }
  }

  // C: the work agent reviews it in a worktree of its branch; its findings come back as pending notes (r reloads).
  async function agentReview() {
    try {
      const r = await api.post('/gitlab/agent-review?url=' + encodeURIComponent(url), {});
      ui.toast((r.Running ? 'Already reviewing in ' : r.Agent + ' reviewing in ') + r.Path + ': its notes come here as pending, r reloads', { kind: 'ok', ms: 8000 });
    } catch (e) { ui.errToast(e); }
  }

  const G = 'Merge request', D = 'Changes', onChanges = () => tab === 'changes';
  scope.bind('1', () => setTab('overview'), 'overview', { group: G, bar: 'tabs' });
  scope.bind('2', () => setTab('changes'), 'changes', { group: G, bar: 'tabs' });
  scope.bind('i', () => { const k = keys()[0]; if (k) app.panel.open(k); else ui.toast('It names no Jira issue'); }, 'the Jira issue it names, beside', { group: G });
  scope.bind('C', agentReview, 'an agent reviews it', { group: G, bar: 'agent review' });
  scope.bind('S', () => review(false), 'submit your review', { group: G, bar: 'review' });
  scope.bind('A', () => review(true), 'approve', { group: G, bar: 'approve' });
  scope.bind('M', merge, 'merge: GitLab\'s defaults first', { group: G, bar: 'merge' });
  scope.bind('o', () => safe(url) && window.open(url, '_blank', 'noopener'), 'open in GitLab', { group: G });
  scope.bind('r', () => load(true), 'reload', { group: G });
  scope.bind('Escape', () => (jobId ? closeJob() : back()), 'close the job log, else back', { group: G, bar: 'back' });
  scope.bind(['j', ']'], () => go(cur + 1), 'next file', { group: D });
  scope.bind(['k', '['], () => go(cur - 1), 'previous file', { group: D });
  scope.bind('n', () => thread(1), 'next inline thread', { group: D, when: onChanges, bar: 'thread' });
  scope.bind('N', () => thread(-1), 'previous inline thread', { group: D, when: onChanges, bar: 'thread' });
  scope.bind('z', () => fold(cur), 'fold the file', { group: D, when: onChanges });
  scope.bind('e', () => (onChanges() ? expand(cur) : edit()), 'edit it: title, draft, people, labels, target, description; on Changes the whole file / the changes only', { group: G });
  scope.bind('Z', foldAll, 'fold / unfold every file', { group: D, when: onChanges });
  scope.bind('v', () => { setTab('changes'); if (picker.hidden) return ui.toast('One version: nothing pushed since it opened'); picker.focus(); picker.showPicker && picker.showPicker(); }, 'pick a version', { group: D });
  delegate(files, 'click', '.df-file-link', (e, t) => go(+t.dataset.file));
  delegate(body, 'click', '.df-head', (e, t) => fold(+t.dataset.file));
  setTab(tab);
  renderHead();
  load(false);
  return () => { dead = true; clearTimeout(pipeTimer); clearTimeout(jobTimer); };
}
