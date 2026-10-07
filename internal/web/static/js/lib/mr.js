// A GitLab merge request (forge.Change, GET /api/gitlab/mr) in pieces the merge request page (views/mr.js), the
// development section and the merge requests screen share: its state, the Jira issues it names, the pipeline as a
// flow of stages, the approvals, and the summary an unfolded row shows. Styles: dev.css.
import { h, safe } from './dom.js';
import { render as md } from './md.js';
import { icon } from './icons.js';
import { ago, isZero, duration, plural } from './fmt.js';

// A check status → a tone and a glyph.
const CHECK = { success: ['ok', '✓'], failed: ['err', '✗'], warning: ['warn', '!'], running: ['run', '●'], pending: ['run', '○'], manual: ['none', '▶'], canceled: ['warn', '⊘'], skipped: ['none', '»'] };
const check = s => CHECK[s] || CHECK.skipped;
const WORD = { success: 'passed', failed: 'failed', warning: 'passed with warnings', running: 'running', pending: 'pending', manual: 'manual', canceled: 'canceled', skipped: 'skipped' };

// mrHref is the merge request's page; tab 'changes' opens on its diff.
export const mrHref = (link, tab) => '/mr?url=' + encodeURIComponent(link) + (tab ? '&tab=' + tab : '');
// diffHref is the page on its diff (d).
export const diffHref = link => mrHref(link, 'changes');

// glyph is a check status as its coloured mark.
export const glyph = s => { const [tone, g] = check(s); return h('span.dv-check.dvt-' + tone, { title: WORD[s] || s }, g); };

// stateBadge is the merge request's state as the development section's PR badges draw it.
export function stateBadge(m) {
  const [cls, text, ic] = m.State === 'merged' ? ['merged', 'Merged', 'git-merge'] : m.State === 'closed' ? ['declined', 'Closed', 'git-pull-request-closed']
    : m.Draft ? ['draft', 'Draft', 'git-pull-request-draft'] : ['open', 'Open', 'git-pull-request'];
  return h('span.dv-badge.pr-' + cls, icon(ic), ' ' + text);
}

// issueKeys are the Jira keys the merge request names, in its title, source branch and description, first seen
// first; only the session's projects' when it knows them, so "UTF-8" is no issue.
export function issueKeys(m, projects) {
  const known = new Set(projects || []), seen = [];
  for (const s of [m.Title, m.SourceBranch, m.Description]) {
    for (const [k] of String(s || '').toUpperCase().matchAll(/\b[A-Z][A-Z0-9]+-\d+\b/g)) {
      if ((!known.size || known.has(k.split('-')[0])) && !seen.includes(k)) seen.push(k);
    }
  }
  return seen;
}

// stageStatus is a stage's worst job: what it shows as.
const RANK = ['failed', 'running', 'pending', 'canceled', 'warning', 'manual', 'success', 'skipped'];
export const stageStatus = g => (g.Jobs || []).map(j => j.Status).sort((a, b) => RANK.indexOf(a) - RANK.indexOf(b))[0] || 'skipped';

// pipeline is the pipeline as its stages in order, joined left to right: each a node with its passed count, the
// jobs that want a look named under it. A big pipeline's passed names (docker/build: [...]) are on hover only.
// o.onJob(job): the jobs are buttons
// (their log), o.job the one shown; o.onStage(name): a stage's passed count is a button, and a stage in o.open
// lists its passed jobs too.
export function pipeline(c, o = {}) {
  if (!c) return null;
  const head = h('div.pp-head', glyph(c.Status), ' ',
    safe(c.WebURL) ? h('a', { href: safe(c.WebURL), target: '_blank', rel: 'noopener noreferrer' }, 'Pipeline ' + (WORD[c.Status] || c.Label)) : 'Pipeline ' + (WORD[c.Status] || c.Label),
    c.Duration ? h('span.dv-dim', ' · ' + duration(c.Duration)) : null);
  const stages = (c.Groups || []).map(g => {
    const jobs = g.Jobs || [], passed = jobs.filter(j => j.Status === 'success' || j.Status === 'skipped'), ok = jobs.filter(j => j.Status === 'success').length;
    const open = !!(o.open && o.open.has(g.Name)), look = jobs.filter(j => !passed.includes(j));
    const [tone, mark] = check(stageStatus(g));
    const node = h('div.pp-node', h('span.pp-dot', mark), h('b', g.Name), h('span.pp-n', ok + '/' + jobs.length));
    const cls = j => (j.Status === 'success' || j.Status === 'skipped' ? '.quiet' : '') + (o.job === j.ID ? '.on' : '');
    const job = j => (o.onJob && j.ID
      ? h('button.pp-job' + cls(j), { title: j.Name + ': its log', onclick: () => o.onJob(j) }, glyph(j.Status), jobName(j.Name))
      : h('span.pp-job' + cls(j), { title: j.Name }, glyph(j.Status), jobName(j.Name)));
    const more = passed.length && o.onStage ? h('button.pp-job.pp-more' + (open ? '.open' : ''), { 'aria-expanded': String(open), title: (open ? 'Hide' : 'Show') + ' the passed jobs', onclick: () => o.onStage(g.Name) },
      glyph('success'), h('span.clip', (look.length ? passed.length + ' more passed' : plural(passed.length, 'job') + ' passed')), h('span.dv-caret', icon('chevron-right'))) : null;
    const rows = [...look.map(job), more, ...(open ? passed.map(job) : [])].filter(Boolean);
    return h('li.pp-stage.dvt-' + tone, { title: jobs.map(j => (WORD[j.Status] || j.Status) + ': ' + j.Name).join('\n') },
      node, rows.length ? h('ul.pp-jobs', rows.map(r => h('li', r))) : null);
  });
  return h('div.pp', head, stages.length ? h('ol.pp-flow', stages) : null);
}

// nameParts is a job name as its start and its end: a matrix job's names share their start (docker/build:branch:
// [...]) and differ at their end, so the end always shows and the start gives way (CSS cuts it); the whole name is
// on hover. A short name is all start.
export const nameParts = (s, tail = 14) => (s.length <= 2 * tail ? [s, ''] : [s.slice(0, -tail), s.slice(-tail)]);
const jobName = n => { const [a, b] = nameParts(n); return h('span.pp-name', h('span.clip', a), b ? h('span.pp-tail', b) : null); };

// running is whether a pipeline or job status may still change.
export const running = s => s === 'running' || s === 'pending';

// pipelineMini is the pipeline in a line: its mark and a dot per stage.
export function pipelineMini(c) {
  if (!c) return null;
  return h('span.pp-mini', { title: 'Pipeline ' + (WORD[c.Status] || c.Label) + ((c.Groups || []).length ? ': ' + c.Groups.map(g => g.Name + ' ' + (WORD[stageStatus(g)] || '')).join(' → ') : '') },
    glyph(c.Status), ' ' + (WORD[c.Status] || c.Label), (c.Groups || []).map(g => h('i.dv-dot.dvt-' + check(stageStatus(g))[0])));
}

// approvals is who approved and who it waits on: the reviewers as avatars, ticked once they approve. null without
// approval rules and approvers.
export function approvals(m, ui) {
  const a = m.Approvals;
  if (!a || !(a.Approved || a.Required || (a.By || []).length)) return null;
  const by = a.By || [], people = [...new Set([...(m.Reviewers || []), ...by])], waiting = (m.Reviewers || []).filter(r => !by.includes(r));
  const count = a.Required ? (a.Required - a.Left) + ' of ' + a.Required : plural(by.length, 'approval');
  return h('span.ap',
    people.length ? h('span.dv-revs', people.map(p => h('span.dv-rev' + (by.includes(p) ? '.ok' : ''), { title: p + (by.includes(p) ? ' approved' : ' has not approved') },
      ui.avatar(p, '', 18), by.includes(p) ? h('i.dv-tick', icon('check')) : null))) : null,
    a.Approved ? h('b.dvt-ok.ap-ok', 'Approved') : h('span', count),
    !a.Approved && waiting.length ? h('span.dv-dim', 'waiting on ' + waiting.join(', ')) : null);
}

// signInHelp is how to sign in to a GitLab host (an ApiError's signin, gitlab.SignIn): glab's login, else a token
// in the config; null for another error.
export function signInHelp(e) {
  const s = e && e.signin;
  if (!s) return null;
  const link = (u, text) => h('a', { href: safe(u), target: '_blank', rel: 'noopener noreferrer' }, text);
  return h('div.mr-signin',
    h('b.dvt-err', s.Rejected ? s.Host + ' rejected the token: expired or revoked.' : 'No GitLab token for ' + s.Host + '.'),
    h('p', 'Sign in with glab: ', h('code', s.Glab), s.Install ? [' (', link(s.Install, 'install glab'), ')'] : null),
    h('p', 'Or add it under ', h('code', 'gitlab:'), ' in the config: a ', link(s.TokenURL, 'personal access token'), ' with scope api (read_api only reads).'),
    h('pre', 'gitlab:\n  - base_url: https://' + s.Host + '\n    token_cmd: [pass, gitlab]'),
    h('p.dv-dim', 'Then reload (r).'));
}

// mrButtons are a merge request row's own actions, so a click on the rest of the row only unfolds it: Review
// (its page, on the diff: d) and GitLab (a new tab: o).
export const mrButtons = link => (safe(link) ? h('span.dv-acts',
  h('a.btn.sm', { href: diffHref(link), title: 'Review the changes (d)', tabindex: -1 }, icon('code'), 'Review'),
  h('a.btn.ghost.sm', { href: safe(link), target: '_blank', rel: 'noopener noreferrer', title: 'Open in GitLab (o)', tabindex: -1 }, 'GitLab', icon('external-link'))) : null);

// mrBody is an unfolded row's summary: merge status, approvals, the pipeline and the description.
export function mrBody(m, ui, go = u => { location.href = u; }) {
  const kv = (k, ...v) => (v.some(Boolean) ? h('div.dv-kv', h('span.dv-k', k), h('span', ...v)) : null);
  const merge = m.State !== 'opened' ? null : m.HasConflicts ? h('span.dvt-err', m.MergeStatus) : m.Mergeable ? h('span.dvt-ok', m.MergeStatus) : m.MergeStatus;
  return h('div.dv-mr', { onclick: e => e.stopPropagation() },
    kv('Merge', merge, isZero(m.UpdatedAt) ? null : h('span.dv-dim', ' · updated ' + ago(m.UpdatedAt))),
    kv('Approvals', approvals(m, ui)),
    kv('Labels', (m.Labels || []).join(', ')),
    m.Checks ? pipeline(m.Checks, { onJob: j => go(mrHref(m.WebURL) + '&job=' + j.ID) }) : null,
    (m.Description || '').trim() ? h('div.dv-desc.md', md(m.Description)) : null);
}
