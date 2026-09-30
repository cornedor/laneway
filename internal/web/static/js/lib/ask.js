// Ask ui.llm about an issue (TUI: ctrl+a): canned questions or free text, the answer streams in.
import { h, clear } from './dom.js';
import { css } from './css.js';
import { render } from './md.js';
import { target } from './timer.js';

export function install(app) {
  const ask = () => {
    if (app.session && app.session.demo) return app.ui.toast('Not available in demo');
    const key = app.panel.key || target(app);
    if (!key) return app.ui.toast('Select an issue first');
    openAsk(app, key);
  };
  app.keys.scope('ask').bind('ctrl+a', ask, 'ask the LLM about the issue', { group: 'Issue' });
  app.commands.register({ id: 'ask', title: 'Ask the LLM about the issue', group: 'Issue', run: ask });
  app.askIssue = openAsk.bind(null, app);
}

// Reads a POST that answers with server-sent events, calling on(event, data) per frame.
async function sse(url, body, signal, on) {
  const res = await fetch(url, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body), signal });
  if (!res.ok) { let m = res.statusText; try { m = (await res.json()).error || m; } catch (e) { /* no body */ } throw new Error(m); }
  const rd = res.body.getReader(), dec = new TextDecoder();
  let buf = '';
  for (;;) {
    const { done, value } = await rd.read();
    if (done) break;
    buf += dec.decode(value, { stream: true });
    let i;
    while ((i = buf.indexOf('\n\n')) >= 0) {
      const frame = buf.slice(0, i); buf = buf.slice(i + 2);
      let ev = 'message', data = '';
      for (const l of frame.split('\n')) { if (l.startsWith('event: ')) ev = l.slice(7); else if (l.startsWith('data: ')) data += l.slice(6); }
      if (data) on(ev, JSON.parse(data));
    }
  }
}

export async function openAsk(app, key) {
  css('ask');
  let info;
  try { info = await app.api.get('/ask'); } catch (e) { return app.ui.errToast(e); }
  if (!info.Available) return app.ui.toast('Asking needs ui.llm (claude -p, llm, ollama run …) or claude on the PATH', { kind: 'err' });
  let abort = null, text = '', busy = false;
  const out = h('div.ask-out.md', { tabindex: 0 }, h('div.dim', 'Pick a question, or type your own.'));
  const status = h('span.dim');
  const free = h('input.input', { type: 'text', placeholder: 'Ask anything about ' + key + '…', autofocus: true, spellcheck: true });
  const copy = h('button.btn', { disabled: true, onclick: async () => { try { await navigator.clipboard.writeText(text); app.ui.toast('Copied'); } catch (e) { app.ui.toast('Could not copy', { kind: 'err' }); } } }, 'Copy');
  const post = h('button.btn', { disabled: true, title: 'Add the answer as a comment', onclick: async () => {
    if (!await app.ui.confirm({ title: 'Post as comment', text: 'Post this answer on ' + key + '?', ok: 'Post' })) return;
    try { await app.api.post('/issues/' + key + '/comments', { Markdown: text }); app.bus.emit('issue:changed', { key }); app.ui.toast('Comment posted'); } catch (e) { app.ui.errToast(e); }
  } }, 'Post as comment');
  const stop = h('button.btn', { hidden: true, onclick: () => abort && abort.abort() }, 'Stop');

  async function run(body, label) {
    if (busy) return;
    busy = true; text = ''; abort = new AbortController();
    clear(out); out.classList.add('streaming');
    status.textContent = label + '…'; stop.hidden = false; copy.disabled = post.disabled = true;
    const pre = h('pre.ask-raw'); out.append(pre);
    try {
      await sse('/api/issues/' + key + '/ask', body, abort.signal, (ev, d) => {
        if (ev === 'message') { text += d; pre.textContent = text; out.scrollTop = out.scrollHeight; }
        else if (ev === 'error') throw new Error(d);
      });
      text = text.trim();
      clear(out).append(text ? render(text) : h('div.dim', 'The answer came back empty.'));
      copy.disabled = post.disabled = !text;
      status.textContent = '';
    } catch (e) {
      status.textContent = e.name === 'AbortError' ? 'stopped' : '';
      if (e.name !== 'AbortError') app.ui.errToast(e);
      if (text) { copy.disabled = post.disabled = false; }
    } finally { busy = false; abort = null; stop.hidden = true; out.classList.remove('streaming'); }
  }

  const canned = info.Asks.map((a, i) => h('button.btn.ask-q', { title: 'Alt+' + (i + 1), onclick: () => run({ Question: a.ID }, a.Label) }, h('kbd', i + 1), ' ' + a.Label));
  const ask = () => { const t = free.value.trim(); if (t) run({ Text: t }, 'Asking'); };
  const m = app.ui.modal(h('div.ask',
    h('div.ask-qs', canned),
    h('form.ask-free', { onsubmit: e => { e.preventDefault(); ask(); } }, free, h('button.btn.primary', { type: 'submit' }, 'Ask')),
    out,
    h('div.row.end', status, h('span.spacer'), stop, copy, post, h('button.btn', { onclick: () => m.close() }, 'Close'))),
  { title: 'Ask ' + info.Command + ' about ' + key, wide: true, onClose: () => abort && abort.abort() });
  info.Asks.forEach((a, i) => m.scope.bind('alt+' + (i + 1), () => run({ Question: a.ID }, a.Label), '', { input: true, hidden: true }));
}
