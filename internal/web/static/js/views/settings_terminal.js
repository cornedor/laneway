// Settings > Appearance: the browser terminal's font and size (prefs terminal.font, terminal.size; lib/fonts.js).
// The presets: the bundled JetBrainsMono Nerd Font Mono (default), the monospace font, the bundled mono fonts and
// uploads; Symbols Nerd Font Mono always follows, so any of them shows icons.
import { h } from '../lib/dom.js';
import fonts from '../lib/fonts.js';
import { T } from '../lib/i18n.js';

const cycle = (list, cur, d) => list[(Math.max(0, list.indexOf(cur)) + d + list.length) % list.length];
const SAMPLE = ' main   ~/src   ✓ ⏺ ⎿ ▁▂▃▅▇ ═╗ -> != 0O1lI';
const SIZES = [0, 11, 12, 13, 14, 15, 16, 18, 20];

export function terminalOptions(app, refresh) {
  const emit = () => app.bus.emit('prefs', { key: 'terminal.font', value: fonts.terminalFont() });
  // [value, label, note]: value is what terminal.font stores
  const presets = () => [
    ['', 'JetBrainsMono Nerd Font', T('default · icons')],
    ['mono', T('Same as monospace'), T('the font above')],
    ...fonts.monoPresets.map(p => [p.stack ? p.stack.replace(/,\s*$/, '') : 'monospace', p.name, p.stack ? T('+ symbols') : T('system + symbols')]),
    ...fonts.files().map(f => ['"' + fonts.fileFamily(f.Name) + '"', f.Name, T('uploaded')]),
  ];
  const preview = v => (v === 'mono' ? fonts.monoStack() : v || fonts.NERD) + ', ' + fonts.SYMBOLS + ', monospace';

  function fontRow() {
    const sample = h('div.st-fsample.mono', SAMPLE);
    const btns = new Map();
    const grid = h('div.st-fgrid', { role: 'radiogroup', 'aria-label': T('Terminal font') });
    const input = h('input.input', {
      type: 'text', tabindex: -1, spellcheck: false, autocomplete: 'off', 'aria-label': T('Terminal font, any CSS font-family'),
      placeholder: T('Custom: any installed font, e.g. "Iosevka Term", Hack Nerd Font Mono'),
      oninput: () => { const v = input.value.trim(); if (!v || fonts.validFamily(v)) { fonts.setTerminalFont(v); paint(); emit(); } },
    });
    const pick = v => { fonts.setTerminalFont(v); input.value = ''; paint(); emit(); };
    for (const [v, label, note] of presets()) {
      const b = h('button.st-fbtn', { type: 'button', tabindex: -1, role: 'radio', style: { fontFamily: preview(v) }, onclick: () => pick(v) }, label, h('small', note));
      btns.set(v, b); grid.append(b);
    }
    function paint() {
      const cur = fonts.terminalFont();
      for (const [v, b] of btns) { const on = v === cur; b.classList.toggle('on', on); b.setAttribute('aria-checked', String(on)); }
      if (!btns.has(cur) && document.activeElement !== input) input.value = cur;
      sample.style.fontFamily = fonts.terminalStack();
    }
    paint();
    return {
      name: T('Terminal font'), desc: T('the agents’ terminal; h/l cycle, enter types a custom family; icons come from the bundled Symbols Nerd Font'), section: 'Fonts', wide: true,
      render: () => h('div.st-fonts', grid, h('div.st-fcustom', input), sample),
      change: d => pick(cycle(presets().map(p => p[0]), fonts.terminalFont(), d)),
      activate: () => { input.tabIndex = 0; input.focus(); input.select(); },
      reset: () => { pick(''); refresh(); },
    };
  }

  const sizeLabel = () => fonts.terminalSize() ? fonts.terminalSize() + ' px' : T('as the UI');
  const setSize = px => { fonts.setTerminalSize(px); app.bus.emit('prefs', { key: 'terminal.size', value: px || '' }); refresh(); };
  return [
    fontRow(),
    { name: T('Terminal font size'), desc: T('the agents’ terminal; h/l step'), section: 'Fonts',
      render: () => h('span.st-val', h('button.btn.ghost', { tabindex: -1, onclick: () => setSize(cycle(SIZES, fonts.terminalSize(), 1)) }, sizeLabel())),
      change: d => setSize(cycle(SIZES, fonts.terminalSize(), d)), reset: () => setSize(0) },
  ];
}
