// The board's search language, the TUI's (internal/ui/jira_search.go), on loaded cards:
//   login  "log in"  status:review,test  epic:  points>2  prio>=high  is:flagged  is:notes
//   due<7d  age>3d  updated<1d  created<7d  reporter:ada  component:api  pr:open
//   deploy:prod  sprint:4  "test type":e2e  -label:ui
import { isZero } from './fmt.js';
import { T } from './i18n.js';

const FIELDS = {
  status: 'status', assignee: 'assignee', who: 'assignee', type: 'type', prio: 'priority', priority: 'priority',
  epic: 'parent', parent: 'parent', label: 'label', labels: 'label', key: 'key', points: 'points', sp: 'points',
  is: 'is', has: 'is', due: 'due', age: 'age', updated: 'updated', pr: 'pr', deploy: 'deploy', sprint: 'sprint',
  created: 'created', reporter: 'reporter', component: 'component', components: 'component',
};
const PRIO = { highest: 0, blocker: 0, critical: 0, high: 1, major: 1, low: 3, minor: 3, lowest: 4, trivial: 4 };
const prioRank = p => { const v = PRIO[String(p).toLowerCase()]; return v == null ? 2 : v; };
const SEP = '\u001f'; // jira.ExtraSep

// Split on spaces outside quotes.
export function words(q) {
  const out = []; let b = '', quoted = false;
  for (const ch of q) {
    if (ch === '"') { quoted = !quoted; b += ch; }
    else if (ch === ' ' && !quoted) { if (b) out.push(b); b = ''; }
    else b += ch;
  }
  if (b) out.push(b);
  return out;
}

function splitTerm(w) {
  let field, rest;
  if (w[0] === '"') {
    const j = w.indexOf('"', 1);
    if (j < 2) return null;
    field = 'custom:' + w.slice(1, j).toLowerCase(); rest = w.slice(j + 1);
    if (!rest || !':<>='.includes(rest[0])) return null;
  } else {
    const i = w.search(/[:<>=]/);
    if (i <= 0 || !FIELDS[w.slice(0, i).toLowerCase()]) return null;
    field = FIELDS[w.slice(0, i).toLowerCase()]; rest = w.slice(i);
  }
  let op = rest[0]; rest = rest.slice(1);
  if ((op === '>' || op === '<') && rest[0] === '=') { op += '='; rest = rest.slice(1); }
  return { field, op, value: rest };
}

export function parse(q) {
  const out = [];
  for (let w of words(q)) {
    const t = { not: false, field: '', op: ':', values: [] };
    if (w[0] === '-' && w.length > 1) { t.not = true; w = w.slice(1); }
    const sp = splitTerm(w);
    if (sp) {
      t.field = sp.field; t.op = sp.op;
      for (const v of sp.value.split(',')) { const x = v.replace(/^"+|"+$/g, '').toLowerCase(); if (x) t.values.push(x); }
    } else t.values = [w.replace(/^"+|"+$/g, '').toLowerCase()];
    out.push(t);
  }
  return out;
}

const order = (op, a, b) => op === '>' ? a > b : op === '>=' ? a >= b : op === '<' ? a < b : op === '<=' ? a <= b : a === b;
const spanMs = s => {
  const m = /^(\d+)([hdw])$/.exec(s);
  return m ? Number(m[1]) * { h: 36e5, d: 864e5, w: 6048e5 }[m[2]] : null;
};
const t = x => (isZero(x) ? null : Date.parse(x));

export function extraOf(c) {
  const out = {};
  if (!c.Extra) return out;
  for (const kv of c.Extra.split(SEP)) { const i = kv.indexOf('='); if (i > 0) out[kv.slice(0, i).toLowerCase()] = kv.slice(i + 1); }
  return out;
}

function is(c, what, env) {
  switch (what) {
    case 'mine': return !!env.me && c.AssigneeID === env.me;
    case 'overdue': return !!t(c.Due) && !c.Done && t(c.Due) < env.now;
    case 'flagged': return !!c.Flagged;
    case 'done': return !!c.Done;
    case 'pr': return !!c.PR;
    case 'unassigned': return !c.Assignee;
    case 'pinned': return env.pins ? env.pins.has(c.Key) : false;
    case 'notes': return env.notes ? env.notes.has(c.Key) : false;
    default: return false;
  }
}

function span(term, c, now) {
  if (!term.values.length || term.op === ':') return false;
  let have;
  if (term.field === 'due' && t(c.Due) && !c.Done) have = t(c.Due) - now;
  else if (term.field === 'age' && c.InProgress && t(c.Since)) have = now - t(c.Since);
  else if (term.field === 'updated' && t(c.Updated)) have = now - t(c.Updated);
  else if (term.field === 'created' && t(c.Created)) have = now - t(c.Created);
  else return false;
  const want = spanMs(term.values[0]);
  return want != null && order(term.op, have, want);
}

function compare(field, op, have, want) {
  if (op === ':') {
    if (field === 'label') return have.split(/\s+/).some(l => l.includes(want));
    if (field === 'component') return have.split(SEP).some(l => l.includes(want));
    return have.includes(want);
  }
  if (field === 'priority') return have !== '' && order(op, prioRank(want), prioRank(have));
  if (field === 'points') { const h = parseFloat(have), w = parseFloat(want); return !isNaN(h) && !isNaN(w) && order(op, h, w); }
  return op === '=' && have === want;
}

const HAVE = {
  status: c => c.Status, assignee: c => c.Assignee, type: c => c.Type, priority: c => c.Priority,
  parent: c => c.ParentKey + ' ' + c.ParentSummary, label: c => c.Labels, key: c => c.Key, points: c => c.Points,
  pr: c => c.PR, deploy: c => c.Deploy, sprint: c => c.Sprint, reporter: c => c.Reporter, component: c => c.Components,
};

function matchTerm(term, c, env) {
  if (!term.field) {
    const q = term.values[0];
    if (env.text) return env.text(c).includes(q);
    return [c.Key, c.Summary, c.Assignee, c.ParentKey, c.ParentSummary].some(s => String(s || '').toLowerCase().includes(q));
  }
  if (term.field === 'is') return term.values.some(v => is(c, v, env));
  if (['due', 'age', 'updated', 'created'].includes(term.field)) return span(term, c, env.now);
  let have = '';
  if (HAVE[term.field]) have = HAVE[term.field](c);
  else if (term.field.startsWith('custom:')) have = extraOf(c)[term.field.slice(7)] || '';
  have = String(have == null ? '' : have).trim().toLowerCase();
  if (!term.values.length) return have === '';
  return term.values.some(v => compare(term.field, term.op, have, v));
}

// compile(q, env) → card => bool; env {me, pins, notes, text}: text(card) is the lowercase haystack for plain words.
export function compile(q, env = {}) {
  const terms = parse(q);
  if (!terms.length) return null;
  return c => {
    const e = { ...env, now: Date.now() };
    for (const tm of terms) if (matchTerm(tm, c, e) === tm.not) return false;
    return true;
  };
}

// ---- the filter builder's vocabulary
export const BUILDER_FIELDS = [
  ['status', T('Status')], ['assignee', T('Assignee')], ['type', T('Type')], ['prio', T('Priority')], ['points', T('Story points')],
  ['label', T('Label')], ['epic', T('Epic')], ['pr', T('Pull request')], ['deploy', T('Deployed to')], ['component', T('Component')],
  ['reporter', T('Reporter')], ['is', T('Is: mine, overdue, flagged…')],
];
export function builderOps(field) {
  if (field === 'is') return [[':', T('is')]];
  if (field === 'prio') return [['>=', T('at least')], ['<=', T('at most')], ['=', T('exactly')]];
  if (field === 'points') return [['>=', T('at least')], ['<=', T('at most')], ['=', T('exactly')], ['empty', T('is empty')], ['-empty', T('is not empty')]];
  return [[':', T('is')], ['-:', T('is not')], ['empty', T('is empty')]];
}
// Values of a field on the loaded cards: [{id, label, n}], most common first.
export function builderValues(cards, field, env) {
  const n = new Map(), label = new Map();
  const add = (v, l) => { if (v) { n.set(v, (n.get(v) || 0) + 1); label.set(v, l || v); } };
  for (const c of cards) {
    switch (field) {
      case 'status': add(c.Status); break;
      case 'assignee': add(c.Assignee); break;
      case 'type': add(c.Type); break;
      case 'prio': add(c.Priority); break;
      case 'points': add(c.Points); break;
      case 'label': for (const l of (c.Labels || '').split(/\s+/)) add(l); break;
      case 'component': for (const l of (c.Components || '').split(SEP)) add(l); break;
      case 'reporter': add(c.Reporter); break;
      case 'pr': add((c.PR || '').toLowerCase()); break;
      case 'deploy': add(c.Deploy); break;
      case 'epic': add(c.ParentKey, (c.ParentKey + ' ' + (c.ParentSummary || '')).trim()); break;
      case 'is': for (const v of ['mine', 'overdue', 'flagged', 'done', 'pr', 'unassigned', 'notes']) if (is(c, v, env)) add(v); break;
      default:
    }
  }
  const out = [...n].map(([id, k]) => ({ id, label: label.get(id), n: k }));
  out.sort((a, b) => (field === 'prio' ? prioRank(a.id) - prioRank(b.id) : b.n - a.n || a.label.toLowerCase().localeCompare(b.label.toLowerCase())));
  return out;
}
export function builderTerm(field, op, v) {
  if (!field || !op) return '';
  if (op === 'empty') return field + ':';
  if (op === '-empty') return '-' + field + ':';
  if (!v) return '';
  if (/[ ,]/.test(v)) v = '"' + v + '"';
  return op === '-:' ? '-' + field + ':' + v : field + op + v;
}
// Put term in q: a ":" term on the same field adds a value, a comparison replaces its like.
export function addTerm(q, term) {
  const m = /^-?[a-z]+(?::|>=|<=|>|<|=)/i.exec(term);
  const prefix = m ? m[0] : term, v = m ? term.slice(m[0].length) : '';
  const ws = words(q);
  const i = ws.findIndex(w => w.toLowerCase().startsWith(prefix.toLowerCase()));
  if (i < 0) ws.push(term);
  else if (!prefix.endsWith(':')) ws[i] = prefix + v;
  else if (v && ws[i].length > prefix.length) ws[i] = ws[i] + ',' + v;
  else ws[i] = prefix + v;
  return ws.join(' ');
}
export const removeTerm = (q, i) => { const ws = words(q); ws.splice(i, 1); return ws.join(' '); };
