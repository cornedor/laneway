// A GitLab merge request (forge.Change, GET /api/gitlab/mr) as the development section and the merge requests
// screen unfold it: state, merge status, approvals, the pipeline by stage and the description. Styles: dev.css.
import { h } from './dom.js';
import { render as md } from './md.js';
import { ago, isZero, duration } from './fmt.js';

const safe = u => (/^https?:\/\//i.test(u || '') ? u : '');
// A check status → a tone and a glyph.
const CHECK = { success: ['ok', '✓'], failed: ['err', '✗'], running: ['run', '●'], pending: ['run', '○'], manual: ['none', '▶'], canceled: ['warn', '⊘'], skipped: ['none', '»'] };
const check = s => CHECK[s] || CHECK.skipped;

// diffHref is the browser's diff view of the merge request at link (views/mr.js).
export const diffHref = link => '#/mr?url=' + encodeURIComponent(link);

// glyph is a check status as its coloured mark.
export const glyph = s => { const [tone, g] = check(s); return h('span.dv-check.dvt-' + tone, { title: s }, g); };

export function mrBody(m) {
  const kv = (k, ...v) => (v.some(Boolean) ? h('div.dv-kv', h('span.dv-k', k), h('span', ...v)) : null);
  const a = m.Approvals, c = m.Checks;
  const approvals = a && [a.Approved ? h('b.dvt-ok', 'approved ') : null, a.Required ? (a.Required - a.Left) + ' of ' + a.Required : String((a.By || []).length),
    (a.By || []).length ? h('span.dv-dim', ' · ' + a.By.join(', ')) : null];
  return h('div.dv-mr', { onclick: e => e.stopPropagation() },
    kv('State', m.Draft ? 'draft' : m.State, isZero(m.UpdatedAt) ? null : h('span.dv-dim', ' · updated ' + ago(m.UpdatedAt))),
    m.State === 'opened' ? kv('Merge', m.HasConflicts ? h('span.dvt-err', m.MergeStatus) : m.MergeStatus) : null,
    kv('Changes', m.ChangesCount && m.ChangesCount + ' files'),
    kv('Assignees', (m.Assignees || []).join(', ')), kv('Reviewers', (m.Reviewers || []).join(', ')),
    approvals ? kv('Approvals', ...approvals) : null,
    kv('Labels', (m.Labels || []).join(', ')),
    c ? kv('Pipeline', glyph(c.Status), ' ', safe(c.WebURL) ? h('a', { href: safe(c.WebURL), target: '_blank', rel: 'noopener noreferrer' }, c.Label) : c.Label, c.Duration ? h('span.dv-dim', ' · ' + duration(c.Duration)) : null) : null,
    c && (c.Groups || []).length ? h('div.dv-stages', c.Groups.map(g => h('div.dv-stage', h('span.dv-k', g.Name), (g.Jobs || []).map(j => h('span.dv-job', glyph(j.Status), ' ' + j.Name))))) : null,
    (m.Description || '').trim() ? h('div.dv-desc.md', md(m.Description)) : null,
    safe(m.WebURL) ? h('div.dv-kv', h('a.btn.ghost.sm', { href: diffHref(m.WebURL), title: 'd' }, 'Diff')) : null);
}
