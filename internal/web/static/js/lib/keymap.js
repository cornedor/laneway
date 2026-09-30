// Which TUI action (ui.keys name, internal/ui/keys.go) each web binding is, by scope and default key.
// A `ui.keys` entry for the action replaces the key here; bindings without an action stay as they are.
const COMMON = { j: 'down', ArrowDown: 'down', k: 'up', ArrowUp: 'up', Home: 'top', End: 'bottom', PageDown: 'page_down', PageUp: 'page_up', r: 'refresh', o: 'browser', y: 'copy_key' };
const TABLE = {
  global: { ':': 'palette', '/': 'search', 'g g': 'goto', '?': 'help', n: 'create', Q: 'jql', ',': 'settings' },
  timer: { T: 'timer', w: 'log_work' },
  undo: { u: 'undo' },
  board: { ...COMMON, h: 'left', ArrowLeft: 'left', l: 'right', ArrowRight: 'right', Enter: 'open', s: 'status', e: 'summary', a: 'assign', p: 'priority', P: 'points',
    H: 'move_left', L: 'move_right', J: 'rank_down', K: 'rank_up', x: 'mark', X: 'bulk', t: 'toggle_mode', S: 'sort', B: 'board', m: 'mine', A: 'assignee_filter', 0: 'clear_filters' },
  planning: { ...COMMON, J: 'rank_down', K: 'rank_up', m: 'move_sprint', x: 'mark', N: 'plan_new', Z: 'plan_start', C: 'plan_complete', E: 'plan_rename', P: 'points', b: 'board', Enter: 'open', R: 'refresh' },
  reports: { ...COMMON, R: 'refresh', Enter: 'open' },
  roadmap: { ...COMMON, h: 'left', ArrowLeft: 'left', l: 'right', ArrowRight: 'right', '+': 'zoom_in', '-': 'zoom_out', '.': 'today', Space: 'roadmap_fold', R: 'refresh', Enter: 'open' },
  work: { ...COMMON, Enter: 'open', e: 'edit_entry', d: 'delete_entry' },
  inbox: { ...COMMON, Enter: 'open', e: 'inbox_done', E: 'inbox_done_all', s: 'inbox_snooze', u: 'inbox_unread' },
  standup: { ...COMMON, Enter: 'open' },
  agents: { Enter: 'open', v: 'toggle_panel', 'ctrl+\\': 'agent_back' },
  terminal: { 'ctrl+\\': 'agent_back' },
  'agents-global': { 'ctrl+\\': 'agent_back' },
  issue: { j: 'next_comment', ArrowDown: 'next_comment', k: 'prev_comment', ArrowUp: 'prev_comment', c: 'comment', R: 'reply', e: 'summary', E: 'description', a: 'assign', p: 'priority', P: 'points',
    l: 'labels', r: 'refresh', d: 'delete_comment', L: 'linked_issue', y: 'copy_key', Y: 'copy_url', o: 'browser', s: 'status', 'ctrl+\\': 'agent_back' },
};
export const actionFor = (scope, spec) => (TABLE[scope] && TABLE[scope][spec]) || '';

const NAMED = { enter: 'Enter', esc: 'Escape', space: 'Space', ' ': 'Space', up: 'ArrowUp', down: 'ArrowDown', left: 'ArrowLeft', right: 'ArrowRight',
  tab: 'Tab', backspace: 'Backspace', delete: 'Delete', home: 'Home', end: 'End', pgup: 'PageUp', pgdown: 'PageDown' };
// A key as the config spells it (bubbletea: "ctrl+y", "enter", "H") in the web's spelling; '' when the browser can't.
export function fromTUI(k) {
  const m = /^((?:ctrl\+|alt\+)*)(.+)$/.exec(k);
  if (!m || /shift|super|hyper|meta/.test(m[2]) && m[2].length > 1 && !NAMED[m[2]]) return '';
  const name = NAMED[m[2].toLowerCase()] || m[2];
  return name.length > 1 && !/^[A-Z][a-z0-9]+([A-Z][a-z]+)*$/.test(name) ? '' : m[1] + name;
}
