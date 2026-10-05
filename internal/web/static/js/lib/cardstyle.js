// Lane cards: where their fields sit (ui.card_layout) and how the cards a board query matches look (ui.card_styles).
// No DOM: the board, the settings designer and jstest share it. The TUI's twin is internal/ui/cardlayout.go.
//   layoutOf(ui) → {top, top_right, bottom, bottom_right}   field ids; a custom field by its configured name
//   lookOf(styles, env) → card => {edge, tint, fade, bold, hidden: Set} | null without styles
import { compile } from './cardquery.js';

export const FIELDS = {
  type: 'Type', key: 'Key', flagged: 'Flag', priority: 'Priority', status: 'Status', points: 'Points', parent: 'Epic',
  subtasks: 'Subtasks', due: 'Due', pr: 'PR', deploy: 'Deploy', labels: 'Labels', age: 'Age', avatar: 'Avatar', assignee: 'Assignee',
};
export const SLOTS = ['top', 'top_right', 'bottom', 'bottom_right'];
export const COLORS = ['accent', 'ok', 'warn', 'err', 'info'];

const goName = k => k.split('_').map(w => w[0].toUpperCase() + w.slice(1)).join(''); // top_right → TopRight
const get = (o, k) => (o ? (o[k] !== undefined ? o[k] : o[goName(k)]) : undefined); // yaml or Go JSON names

// fieldId is a field name as the layout keeps it, '' for an unknown one.
export function fieldId(name, custom = []) {
  const n = String(name || '').trim().toLowerCase();
  if (FIELDS[n]) return n;
  return custom.find(c => c.toLowerCase() === n) || '';
}
export const fieldLabel = id => FIELDS[id] || id;

// defaultLayout is the card without a ui.card_layout: ui.card_fields picks, custom fields before the labels.
export function defaultLayout(cardFields, custom = []) {
  const on = cardFields && cardFields.length ? new Set(cardFields.map(x => String(x).toLowerCase().trim())) : null;
  const has = f => f === 'key' || f === 'labels' || custom.includes(f) || !on || on.has(f) || (f === 'avatar' && on.has('assignee'));
  const pick = l => l.filter(has);
  return {
    top: pick(['type', 'key', 'flagged']), top_right: pick(['priority', 'points']),
    bottom: pick(['parent', 'subtasks', 'due', 'pr', 'deploy', ...custom, 'labels']), bottom_right: pick(['age', 'avatar']),
  };
}

// normLayout is a layout from the config (either key style) with known fields, each once, and the key.
export function normLayout(raw, custom = []) {
  const seen = new Set(), out = {};
  for (const s of SLOTS) out[s] = (get(raw, s) || []).map(f => fieldId(f, custom)).filter(f => f && !seen.has(f) && seen.add(f));
  if (!seen.has('key')) out.top.unshift('key');
  return out;
}
const isSet = raw => !!raw && SLOTS.some(s => (get(raw, s) || []).length);

// layoutOf is the layout the board draws for session.ui.
export function layoutOf(ui = {}) {
  const custom = ui.CustomFields || [];
  return isSet(ui.CardLayout) ? normLayout(ui.CardLayout, custom) : defaultLayout(ui.CardFields, custom);
}
export const layoutIsSet = ui => isSet(ui && ui.CardLayout);

// normStyle is a ui.card_styles entry with yaml names, fields known, colours checked.
export function normStyle(raw, custom = []) {
  const colour = v => { v = String(v || '').trim(); return COLORS.includes(v.toLowerCase()) ? v.toLowerCase() : /^#([0-9a-f]{3}|[0-9a-f]{6})$/i.test(v) ? v : ''; };
  const fields = l => (l || []).map(f => fieldId(f, custom)).filter(Boolean);
  return { when: String(get(raw, 'when') || ''), edge: colour(get(raw, 'edge')), tint: colour(get(raw, 'tint')), fade: !!get(raw, 'fade'), bold: !!get(raw, 'bold'), hide: fields(get(raw, 'hide')), show: fields(get(raw, 'show')) };
}

// lookOf compiles styles: a later match's colours win; a field in some style's show is hidden unless one that shows
// it matches, and a match's hide hides it whatever shows it.
export function lookOf(styles, env = {}, custom = []) {
  const rules = (styles || []).map(s => normStyle(s, custom)).map(s => ({ ...s, match: s.when.trim() ? compile(s.when.trim().toLowerCase(), env) : null })).filter(r => r.match);
  if (!rules.length) return null;
  const gated = rules.flatMap(r => r.show);
  return c => {
    const lk = { edge: '', tint: '', fade: false, bold: false, hidden: new Set(gated) };
    const hits = rules.filter(r => r.match(c));
    for (const r of hits) {
      if (r.edge) lk.edge = r.edge;
      if (r.tint) lk.tint = r.tint;
      lk.fade = lk.fade || r.fade; lk.bold = lk.bold || r.bold;
      for (const f of r.show) lk.hidden.delete(f);
    }
    for (const r of hits) for (const f of r.hide) lk.hidden.add(f);
    return lk;
  };
}

// colour is a style colour as CSS.
export const colour = v => (COLORS.includes(v) ? 'var(--' + v + ')' : v || '');

// layoutValue and styleValue are what PUT /api/settings takes: yaml names, empty parts left out.
export function layoutValue(l) {
  const out = {};
  for (const s of SLOTS) if (l[s] && l[s].length) out[s] = l[s];
  return out;
}
export function styleValue(s) {
  const out = { when: s.when.trim() };
  for (const k of ['edge', 'tint']) if (s[k]) out[k] = s[k];
  for (const k of ['fade', 'bold']) if (s[k]) out[k] = true;
  for (const k of ['hide', 'show']) if (s[k] && s[k].length) out[k] = s[k];
  return out;
}
