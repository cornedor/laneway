// Settings > Appearance: the font rows (UI font, monospace font, ligatures, line height, reset).
// Pure view over lib/fonts.js; each font row is one wide option: presets in their own face, a custom
// family field (installed fonts), uploaded files, and a sample line. h/l cycle the choices, enter edits the field.
import { h } from '../lib/dom.js';
import fonts from '../lib/fonts.js';

const cycle = (list, cur, d) => list[(Math.max(0, list.indexOf(cur)) + d + list.length) % list.length];
const SAMPLE = {
  ui: 'The quick brown fox jumps over the lazy dog 0123456789',
  mono: 'const ok = a !== b && c >= 0 ? x => y : null; // -> <= == =>',
};
const cleanName = n => n.toLowerCase().replace(/^.*[\\/]/, '').replace(/[^a-z0-9._-]+/g, '-').replace(/^[.-]+/, '');

export function fontOptions(app, refresh) {
  const toast = (m, kind) => app.ui.toast(m, { kind });
  const emit = role => app.bus.emit('prefs', { key: 'font.' + role, value: fonts.choice(role) });

  function fontRow(role, name, desc) {
    const presets = role === 'ui' ? fonts.uiPresets : fonts.monoPresets;
    const ids = () => [...presets.map(p => p.id), ...fonts.files().map(f => 'file:' + f.Name), ...(fonts.custom(role) ? ['custom'] : [])];
    const sample = h('div.st-fsample', { class: role === 'mono' ? 'mono' : '' }, SAMPLE[role]);
    const hint = h('div.st-fhint');
    const input = h('input.input', {
      type: 'text', tabindex: -1, spellcheck: false, autocomplete: 'off', 'aria-label': name + ', any CSS font-family',
      placeholder: role === 'ui' ? 'Custom: any installed font, e.g. Berkeley Sans, "Helvetica Neue"' : 'Custom: any installed font, e.g. Berkeley Mono, Iosevka',
      value: fonts.custom(role),
      oninput: () => {
        const v = input.value.trim();
        if (v && !fonts.validFamily(v)) { hintFor(v); return; }
        fonts.setCustom(role, v);
        fonts.setChoice(role, v ? 'custom' : 'system');
        paint(); emit(role);
      },
    });
    const btns = new Map();
    const grid = h('div.st-fgrid', { role: 'radiogroup', 'aria-label': name });
    const pick = id => { fonts.setChoice(role, id); paint(); emit(role); };
    const mk = (id, label, note, stack) => {
      const b = h('button.st-fbtn', { type: 'button', tabindex: -1, role: 'radio', style: { fontFamily: stack }, onclick: () => pick(id) }, label, note && h('small', note));
      btns.set(id, b); return b;
    };
    for (const p of presets) grid.append(mk(p.id, p.name, p.note, fonts.presetStack(role, p)));
    const fileBox = h('div.row');
    const drawFiles = () => {
      fileBox.replaceChildren();
      for (const f of fonts.files()) {
        const id = 'file:' + f.Name, b = mk(id, f.Name, 'uploaded · ' + Math.ceil(f.Size / 1024) + ' kB', '"' + fonts.fileFamily(f.Name) + '"');
        const x = h('button.btn.ghost.st-x', { type: 'button', tabindex: -1, title: 'Remove ' + f.Name, 'aria-label': 'Remove ' + f.Name, onclick: () => removeFile(f.Name) }, '×');
        fileBox.append(h('span.st-ffile', b, x));
      }
    };
    async function removeFile(n) {
      try { await fonts.remove(n); if (fonts.choice(role) === 'file:' + n) fonts.setChoice(role, 'system'); toast('Removed ' + n, 'ok'); refresh(); } catch (e) { app.ui.errToast(e); }
    }
    const picker = h('input', { type: 'file', accept: '.woff2,.woff,.ttf,.otf,font/*', hidden: true, onchange: async () => {
      const f = picker.files[0]; picker.value = ''; if (!f) return;
      try {
        await fonts.upload(f.name, f);
        const n = cleanName(f.name);
        fonts.setChoice(role, 'file:' + n); emit(role); toast('Uploaded ' + n, 'ok'); refresh();
      } catch (e) { app.ui.errToast(e); }
    } });
    function hintFor(v) {
      const cur = fonts.choice(role);
      hint.className = 'st-fhint';
      if (!v) { hint.textContent = 'Type the name of an installed font, or upload a file. Falls back to the ' + (role === 'ui' ? 'system font' : 'system monospace') + '.'; return; }
      if (!fonts.validFamily(v)) { hint.textContent = 'Not a plain font-family list (no ; { } @ url()).'; hint.classList.add('warn'); return; }
      const ok = fonts.available(v);
      if (ok === null) hint.textContent = '';
      else if (ok) { hint.textContent = (cur === 'custom' ? 'Applied. ' : '') + 'Found.'; hint.classList.add('ok'); }
      else { hint.textContent = 'Not found on this device (first family); the fallback shows instead.'; hint.classList.add('warn'); }
    }
    function paint() {
      const cur = fonts.choice(role);
      for (const [id, b] of btns) { const on = id === cur; b.classList.toggle('on', on); b.setAttribute('aria-checked', String(on)); }
      sample.style.fontFamily = fonts.stack(role);
      hintFor(input.value.trim());
    }
    drawFiles(); paint();
    return {
      name, desc, section: 'Appearance', wide: true,
      render: () => h('div.st-fonts', grid, fileBox, h('div.st-fcustom', input,
        h('button.btn', { type: 'button', tabindex: -1, onclick: () => picker.click() }, 'Upload font…'), picker), hint, sample),
      change: d => { const id = cycle(ids(), fonts.choice(role), d); if (id === 'custom' && !input.value.trim()) return; pick(id); },
      activate: () => { input.tabIndex = 0; input.focus(); input.select(); },
      reset: () => { fonts.setChoice(role, 'system'); fonts.setCustom(role, ''); emit(role); refresh(); },
    };
  }

  const lhs = [0, 1.3, 1.45, 1.6, 1.8];
  return [
    fontRow('ui', 'UI font', 'h/l cycle, enter edits the custom field; bundled fonts load when first used'),
    fontRow('mono', 'Monospace font', 'keys, code, the editor’s monospace mode, the terminal'),
    { name: 'Ligatures', desc: 'in monospace text: -> => != as one glyph (Fira Code, JetBrains Mono, Cascadia Code)', section: 'Appearance',
      render: () => { const on = fonts.ligatures(); return h('span.st-val', h('button.st-switch' + (on ? '.on' : ''), { role: 'switch', 'aria-checked': on, 'aria-label': 'Ligatures', tabindex: -1, onclick: () => { fonts.setLigatures(!on); refresh(); } }, h('i')), h('span.st-state', on ? 'on' : 'off')); },
      change: () => { fonts.setLigatures(!fonts.ligatures()); refresh(); }, reset: () => { fonts.setLigatures(true); refresh(); } },
    { name: 'Line height', desc: 'text leading; density sets spacing of rows and panels, this the lines inside them', section: 'Appearance',
      render: () => h('span.st-val', h('button.btn.ghost', { tabindex: -1, onclick: () => { fonts.setLineHeight(cycle(lhs, fonts.lineHeight(), 1)); refresh(); } }, fonts.lineHeight() || 'default (1.45)')),
      change: d => { fonts.setLineHeight(cycle(lhs, fonts.lineHeight(), d)); refresh(); }, reset: () => { fonts.setLineHeight(0); refresh(); } },
    { name: 'Reset fonts', desc: 'system fonts, ligatures on, default line height (uploaded files stay)', section: 'Appearance',
      render: () => h('span.st-val', h('button.btn', { tabindex: -1, onclick: doReset }, 'Reset')), change: doReset },
  ];
  function doReset() { fonts.reset(); refresh(); toast('Fonts reset', 'ok'); }
}
