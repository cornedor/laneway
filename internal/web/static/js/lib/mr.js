// A GitLab merge request (forge.Change, GET /api/gitlab/mr) in pieces the merge request page (views/mr.js), the
// development section and the merge requests screen share: its state, the Jira issues it names, the pipeline as a
// flow of stages, the approvals, and the summary an unfolded row shows. Styles: dev.css.
import { h } from './dom.js';
import { render as md } from './md.js';
import { icon } from './icons.js';
import { ago, isZero, duration, plural } from './fmt.js';

const safe = u => (/^https?:\/\//i.test(u || '') ? u : '');
// A check status → a tone and a glyph.
const CHECK = { success: ['ok', '✓'], failed: ['err', '✗'], warning: ['warn', '!'], running: ['run', '●'], pending: ['run', '○'], manual: ['none', '▶'], canceled: ['warn', '⊘'], skipped: ['none', '»'] };
const check = s => CHECK[s] || CHECK.skipped;
const WORD = { success: 'passed', failed: 'failed', warning: 'passed with warnings', running: 'running', pending: 'pending', manual: 'manual', canceled: 'canceled', skipped: 'skipped' };

// mrHref is the merge request's page; tab 'changes' opens on its diff.
export const mrHref = (link, tab) => '#/mr?url=' + encodeURIComponent(link) + (tab ? '&tab=' + tab : '');
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
export function pipeline(c) {
  if (!c) return null;
  const head = h('div.pp-head', glyph(c.Status), ' ',
    safe(c.WebURL) ? h('a', { href: safe(c.WebURL), target: '_blank', rel: 'noopener noreferrer' }, 'Pipeline ' + (WORD[c.Status] || c.Label)) : 'Pipeline ' + (WORD[c.Status] || c.Label),
    c.Duration ? h('span.dv-dim', ' · ' + duration(c.Duration)) : null);
  const stages = (c.Groups || []).map(g => {
    const jobs = g.Jobs || [], ok = jobs.filter(j => j.Status === 'success').length, look = jobs.filter(j => j.Status !== 'success' && j.Status !== 'skipped');
    const [tone] = check(stageStatus(g));
    return h('li.pp-stage.dvt-' + tone, { title: jobs.map(j => (WORD[j.Status] || j.Status) + ': ' + j.Name).join('\n') },
      h('div.pp-node', h('span.pp-dot', check(stageStatus(g))[1]), h('b', g.Name), h('span.pp-n', ok + '/' + jobs.length)),
      look.length ? h('ul.pp-jobs', look.map(j => h('li', glyph(j.Status), ' ', h('span', j.Name)))) : null);
  });
  return h('div.pp', head, stages.length ? h('ol.pp-flow', stages) : null);
}

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

// mrButtons are a merge request row's own actions, so a click on the rest of the row only unfolds it: Review
// (its page, on the diff: d) and GitLab (a new tab: o).
export const mrButtons = link => (safe(link) ? h('span.dv-acts',
  h('a.btn.sm', { href: diffHref(link), title: 'Review the changes (d)', tabindex: -1 }, icon('code'), 'Review'),
  h('a.btn.ghost.sm', { href: safe(link), target: '_blank', rel: 'noopener noreferrer', title: 'Open in GitLab (o)', tabindex: -1 }, 'GitLab', icon('external-link'))) : null);

// mrBody is an unfolded row's summary: merge status, approvals, the pipeline and the description.
export function mrBody(m, ui) {
  const kv = (k, ...v) => (v.some(Boolean) ? h('div.dv-kv', h('span.dv-k', k), h('span', ...v)) : null);
  const merge = m.State !== 'opened' ? null : m.HasConflicts ? h('span.dvt-err', m.MergeStatus) : m.Mergeable ? h('span.dvt-ok', m.MergeStatus) : m.MergeStatus;
  return h('div.dv-mr', { onclick: e => e.stopPropagation() },
    kv('Merge', merge, isZero(m.UpdatedAt) ? null : h('span.dv-dim', ' · updated ' + ago(m.UpdatedAt))),
    kv('Approvals', approvals(m, ui)),
    kv('Labels', (m.Labels || []).join(', ')),
    m.Checks ? pipeline(m.Checks) : null,
    (m.Description || '').trim() ? h('div.dv-desc.md', md(m.Description)) : null);
}
