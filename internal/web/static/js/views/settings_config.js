// The config file's ui: options on the settings page. The server owns the table (/api/settings: type,
// default, value, description, choices) and writes the file the terminal app reads, comments kept.
import { h } from '../lib/dom.js';
import { restart } from '../lib/sites.js';

const cycle = (list, cur, d) => list[(Math.max(0, list.indexOf(cur)) + d + list.length) % list.length];
const show = v => (Array.isArray(v) ? v.join(', ') : String(v));

// host: {redraw(o) re-renders the row, begin(edit) / end() tell the page an inline editor is open}
export async function load(app, host) {
  const d = await app.api.get('/settings', { fresh: true });
  const options = d.settings.map(st => make(app, host, st, d.editable));
  return { options, path: d.path, editable: d.editable, warnings: d.warnings || [], groups: d.groups, keyActions: d.keyActions };
}

// put writes ui.<name> and refreshes the session's copy; the setting comes back as /api/settings lists it.
export async function put(app, name, payload) {
  const st = await app.api.put('/settings/' + name, payload);
  app.api.get('/session', { fresh: true }).then(s => { if (app.session) app.session.ui = s.ui; }).catch(() => {});
  app.bus.emit('prefs', { key: 'ui.' + name, value: '' });
  return st;
}

function make(app, host, st0, editable) {
  const o = { name: st0.Name, section: st0.Group, cfg: true, st: st0, err: '', wide: st0.Type === 'yaml', editable };
  const st = () => o.st;
  const current = () => { const s = st(); return s.Value != null ? show(s.Value) : (s.Type === 'bool' || s.Type === 'enum') && s.Choices && s.Name !== 'theme' && s.Name !== 'code_theme' ? s.Choices[0] : ''; };
  o.desc = st0.Doc;
  Object.defineProperty(o, 'meta', { get: () => (st().Restart ? 'restart needed' : '') });

  async function save(payload, quiet) {
    if (!editable) { app.ui.toast('No config file to write to', { kind: 'err' }); return false; }
    try {
      o.st = await put(app, o.name, payload);
      o.err = '';
      if (!quiet) app.ui.toast('Saved ui.' + o.name + (st().Restart ? ' · takes effect on restart' : ''), { kind: 'ok', action: st().Restart ? { label: 'Restart', run: () => restart(app) } : undefined });
      host.redraw(o);
      return true;
    } catch (e) { o.err = e.message; host.redraw(o); return false; }
  }
  o.reset = () => { if (st().Set) save({ Value: null }); };

  const tag = (cls, text, title) => h('span.st-tag' + cls, { title }, text);
  const valueText = () => {
    const s = st();
    if (s.Type === 'yaml') return s.Set ? s.YAML.split('\n').length + (s.YAML.includes('\n') ? ' lines' : ' line') : '';
    return s.Set ? show(s.Value) : '';
  };
  const def = () => h('span.faint', 'default: ' + (st().Default || '-'));

  o.edit = null;
  function begin(multi) {
    const s = st();
    const init = s.Type === 'yaml' ? s.YAML || '' : s.Value != null ? show(s.Value) : '';
    const input = multi ? h('textarea.input.st-json', { rows: Math.min(14, Math.max(5, init.split('\n').length + 1)), spellcheck: false, value: init, placeholder: s.Default })
      : h('input.input', { type: 'text', value: init, placeholder: s.Default, spellcheck: false, autocomplete: 'off' });
    o.edit = { input, multi };
    const finish = () => { o.edit = null; host.end(); host.redraw(o); };
    host.begin({
      o, multi,
      commit: async () => {
        const v = input.value;
        const ok = await save(s.Type === 'yaml' ? { YAML: v } : { Value: v }, false);
        if (ok) finish(); else { o.edit.input = input; host.redraw(o); input.focus(); }
      },
      cancel: () => { o.err = ''; finish(); },
    });
    host.redraw(o);
    setTimeout(() => { input.focus(); if (input.select && !multi) input.select(); }, 0);
  }

  const toggleBtn = (label, on, onclick) => h('button.btn.ghost.st-pick' + (on ? '.on' : ''), { tabindex: -1, onclick }, label);

  o.render = () => {
    const s = st();
    if (o.edit) {
      const ed = o.edit;
      return h('div.st-edit', ed.input, o.err && h('div.st-err', o.err),
        h('div.row', h('button.btn', { tabindex: -1, onclick: () => host.commit() }, 'Save'), h('button.btn.ghost', { tabindex: -1, onclick: () => host.cancel() }, 'Cancel'),
          h('span.faint', ed.multi ? 'ctrl+enter saves · esc cancels · empty resets' : 'enter saves · esc cancels · empty resets')));
    }
    const reset = s.Set && h('button.btn.ghost.st-x', { tabindex: -1, title: 'Back to the default (del)', 'aria-label': 'Reset ' + o.name, onclick: e => { e.stopPropagation(); o.reset(); } }, '×');
    const err = o.err && h('div.st-err', o.err);
    switch (s.Type) {
      case 'bool': {
        const on = current() === 'on';
        return h('span.st-val', h('button.st-switch' + (on ? '.on' : '') + (s.Set ? '' : '.unset'), { role: 'switch', 'aria-checked': on, 'aria-label': o.name, tabindex: -1, onclick: () => o.change(1) }, h('i')),
          h('span.st-state', on ? 'on' : 'off'), reset, err);
      }
      case 'enum':
        return h('span.st-val', h('button.btn.ghost', { tabindex: -1, onclick: () => o.activate() }, s.Set ? show(s.Value) : h('span.faint', current() || 'default')), reset, err);
      case 'yaml':
        return h('div.st-yaml', h('div.row', s.Set ? h('span.chip', valueText()) : def(), h('span.spacer'), h('button.btn.ghost', { tabindex: -1, onclick: () => o.activate() }, 'Edit'), reset),
          s.Set && h('pre.st-pre', s.YAML.split('\n').slice(0, 8).join('\n') + (s.YAML.split('\n').length > 8 ? '\n…' : '')), err);
      default:
        return h('span.st-val.mono', s.Set ? h('button.btn.ghost.st-text', { tabindex: -1, onclick: () => o.activate() }, valueText()) : h('button.btn.ghost.st-text', { tabindex: -1, onclick: () => o.activate() }, def()), reset, err);
    }
  };

  o.change = d => {
    const s = st();
    if (s.Type === 'bool') save({ Value: current() === 'on' ? 'off' : 'on' });
    else if (s.Type === 'enum' && s.Choices.length <= 8) save({ Value: cycle(s.Choices, current() || s.Choices[0], d) });
  };
  o.activate = async () => {
    const s = st();
    if (o.edit) return;
    if (s.Type === 'bool') return o.change(1);
    if (s.Type === 'enum') {
      const r = await app.ui.pick({ title: o.name, items: s.Choices, placeholder: s.Doc || 'Filter…', selected: [] });
      if (r) save({ Value: r });
      return;
    }
    begin(s.Type === 'yaml');
  };
  return o;
}
