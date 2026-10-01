// A text field that draws its text through highlight(text) → HTML (markup that
// wraps the text, never changes it): a plaintext contenteditable carrying a
// textarea's API (value, selectionStart/End, setSelectionRange, setRangeText,
// readOnly), so callers use it as one. Re-rendering clears the browser's undo,
// so it keeps its own (ctrl+z, ctrl+shift+z, ctrl+y).
//
//   const ta = mdArea('div.input.ed-ta', {highlight, enter(v, a, b) → edit | null, paste(v, a, b, text) → edit | null, placeholder, rows})
//   ta.edit({from, to, text, a, b})   replace [from, to) with text, select [a, b), one undo step
import { h } from './dom.js';

// The text of el: text nodes, a <br> as a newline except the last one (it only
// holds an empty last line open), a block element starts a line.
function read(el) {
  let s = '', br = false;
  const walk = n => {
    for (let c = n.firstChild; c; c = c.nextSibling) {
      if (c.nodeType === 3) { if (c.data) { s += c.data; br = false; } } else if (c.nodeName === 'BR') { s += '\n'; br = true; } else if (c.nodeType === 1) {
        if (/^(DIV|P)$/.test(c.nodeName) && s && !s.endsWith('\n')) s += '\n';
        walk(c);
      }
    }
  };
  walk(el);
  return br ? s.slice(0, -1) : s;
}

// The text nodes of el with where each starts in read(el).
function pieces(el) {
  const out = [];
  let at = 0;
  const walk = n => {
    for (let c = n.firstChild; c; c = c.nextSibling) {
      if (c.nodeType === 3) { out.push({ node: c, at }); at += c.data.length; } else if (c.nodeName === 'BR') { out.push({ node: c, at, br: true }); at += 1; } else if (c.nodeType === 1) walk(c);
    }
  };
  walk(el);
  return out;
}

export function mdArea(sel, o) {
  const el = h(sel, { role: 'textbox', 'aria-multiline': 'true', spellcheck: 'true', 'data-placeholder': o.placeholder || '' });
  el.contentEditable = 'plaintext-only';
  if (el.contentEditable !== 'plaintext-only') el.contentEditable = 'true';
  const editable = el.contentEditable;
  if (o.rows) el.style.minHeight = 'calc(' + o.rows + ' * 1.45em + .714rem + 2px)';

  let text = '', composing = false;

  // ---- selection as offsets into the text
  const offset = (node, off) => {
    if (node === el || node.nodeType !== 3) {
      // An element and a child index: where that child starts.
      const child = node.childNodes[off];
      const ps = pieces(el);
      if (!child) {
        if (node === el) return text.length;
        const next = ps.find(p => node.compareDocumentPosition(p.node) & Node.DOCUMENT_POSITION_FOLLOWING && !node.contains(p.node));
        return next ? next.at : text.length;
      }
      const p = ps.find(p => p.node === child || child.contains(p.node) || child.compareDocumentPosition(p.node) & Node.DOCUMENT_POSITION_FOLLOWING);
      return p ? p.at : text.length;
    }
    const p = pieces(el).find(p => p.node === node);
    return p ? Math.min(p.at + off, text.length) : text.length;
  };
  const getSel = () => {
    const s = window.getSelection();
    if (!s.rangeCount || !el.contains(s.anchorNode)) return [text.length, text.length];
    const r = s.getRangeAt(0);
    return [offset(r.startContainer, r.startOffset), offset(r.endContainer, r.endOffset)];
  };
  // The node and offset for i: at a line start the next node, so the caret sits
  // on the new line rather than after the old one's newline.
  const point = (ps, i) => {
    for (const p of ps) {
      if (p.br) { if (p.at === i) return [p.node.parentNode, [...p.node.parentNode.childNodes].indexOf(p.node)]; continue; }
      const end = p.at + p.node.data.length;
      if (i >= p.at && (i < end || (i === end && !p.node.data.endsWith('\n')))) return [p.node, i - p.at];
    }
    const t = ps.filter(p => !p.br).pop();
    return t ? [t.node, t.node.data.length] : [el, el.childNodes.length];
  };
  // Only a focused field takes a selection; focus() places the caret.
  const setSel = (a, b) => {
    if (document.activeElement !== el) return;
    const ps = pieces(el), r = document.createRange();
    r.setStart(...point(ps, a)); r.setEnd(...point(ps, b));
    const s = window.getSelection();
    s.removeAllRanges(); s.addRange(r);
  };

  const render = () => {
    el.innerHTML = text ? o.highlight(text) + (text.endsWith('\n') ? '<br>' : '') : '';
  };

  // ---- undo: one entry per change, typing within a second makes one step
  const hist = [{ text: '', a: 0, b: 0 }];
  let at = 0, last = 0;
  const record = (typing, a, b) => {
    const now = Date.now();
    if (typing && last && now - last < 1000 && at === hist.length - 1 && at > 0) hist[at] = { text, a, b };
    else { hist.splice(at + 1); hist.push({ text, a, b }); at = hist.length - 1; if (hist.length > 200) { hist.shift(); at--; } }
    last = typing ? now : 0;
  };
  const restore = i => {
    if (i < 0 || i >= hist.length) return;
    at = i; last = 0;
    const st = hist[i];
    text = st.text; render(); setSel(st.a, st.b);
    el.dispatchEvent(new Event('input'));
  };

  const edit = e => {
    if (!e) return false;
    text = text.slice(0, e.from) + e.text + text.slice(e.to);
    render();
    setSel(e.a, e.b);
    record(false, e.a, e.b);
    el.dispatchEvent(new Event('input'));
    return true;
  };

  // The browser typed, deleted, cut or dropped: read it back and redraw.
  el.addEventListener('input', e => {
    if (!e.isTrusted || composing) return;
    text = read(el);
    const [a, b] = getSel();
    render(); setSel(a, b);
    record(e.inputType === 'insertText' || e.inputType === 'deleteContentBackward' || e.inputType === 'deleteContentForward', a, b);
  });
  el.addEventListener('compositionstart', () => { composing = true; });
  el.addEventListener('compositionend', () => {
    composing = false;
    text = read(el);
    const [a, b] = getSel();
    render(); setSel(a, b); record(true, a, b);
    el.dispatchEvent(new Event('input'));
  });
  // A newline is ours, so no <div> or <br> appears; a key handler that takes
  // enter first (a popup) prevents it before it gets here. shift+enter is a plain one.
  let shift = false;
  el.addEventListener('beforeinput', e => {
    const t = e.inputType;
    if (t === 'historyUndo' || t === 'historyRedo') { e.preventDefault(); restore(at + (t === 'historyUndo' ? -1 : 1)); return; }
    if (t !== 'insertParagraph' && t !== 'insertLineBreak') return;
    e.preventDefault();
    const [a, b] = getSel();
    edit((!shift && o.enter && o.enter(text, a, b)) || { from: a, to: b, text: '\n', a: a + 1, b: a + 1 });
  });
  el.addEventListener('keydown', e => {
    shift = e.shiftKey;
    if (!(e.ctrlKey || e.metaKey) || e.altKey) return;
    const k = e.key.toLowerCase();
    if (k === 'z' || k === 'y') { e.preventDefault(); restore(at + (k === 'z' && !e.shiftKey ? -1 : 1)); }
  });
  // Pasted text goes in as text; files are left to the field's owner.
  el.addEventListener('paste', e => {
    const d = e.clipboardData;
    if (!d || d.files.length) return;
    e.preventDefault();
    const t = d.getData('text/plain').replace(/\r\n?/g, '\n');
    if (!t) return;
    const [a, b] = getSel();
    edit((o.paste && o.paste(text, a, b, t)) || { from: a, to: b, text: t, a: a + t.length, b: a + t.length });
  });

  // ---- a textarea's API
  Object.defineProperties(el, {
    value: {
      get: () => text,
      set: v => { v = String(v == null ? '' : v); if (v === text) return; text = v; render(); setSel(text.length, text.length); record(false, text.length, text.length); },
    },
    selectionStart: { get: () => getSel()[0] },
    selectionEnd: { get: () => getSel()[1] },
    readOnly: { get: () => el.contentEditable === 'false', set: v => { el.contentEditable = v ? 'false' : editable; } },
  });
  el.setSelectionRange = (a, b = a) => setSel(a, b);
  el.setRangeText = (t, a, b, mode) => {
    const [s0, s1] = getSel(), d = t.length - (b - a);
    const map = i => (i <= a ? i : i >= b ? i + d : a + t.length);
    const [n0, n1] = mode === 'end' ? [a + t.length, a + t.length] : mode === 'select' ? [a, a + t.length] : [map(s0), map(s1)];
    text = text.slice(0, a) + t + text.slice(b);
    render(); setSel(n0, n1); record(false, n0, n1);
  };
  el.edit = edit;
  el.rerender = () => { const [a, b] = getSel(); render(); if (document.activeElement === el) setSel(a, b); };
  el.offsetOf = node => offset(node, 0);

  el.value = o.value || '';
  hist.length = 0; hist.push({ text, a: text.length, b: text.length }); at = 0;
  return el;
}
