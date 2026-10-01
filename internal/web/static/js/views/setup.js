// First start: no Jira site yet. Asks for the site, email and API token the
// way `laneway setup` does, with the steps to make a token; the server signs
// in, writes the config and comes back as the app, so the page reloads.
import { h, $, clear } from '../lib/dom.js';
import * as api from '../lib/api.js';

export function mountSetup(el, st) {
  document.title = 'Connect laneway to Jira';
  $('#app').classList.add('setup-mode');
  const again = !!st.site;
  const errs = {};
  const field = (name, label, input, ...hint) => {
    errs[name] = h('div.form-err', { id: 'su-err-' + name, role: 'alert' });
    input.setAttribute('aria-describedby', 'su-err-' + name);
    return h('div.su-field', h('label.su-label', { for: input.id }, label), ...hint, input, errs[name]);
  };
  const site = h('input.input#su-site', { name: 'site', value: st.site || '', placeholder: 'acme.atlassian.net', autocomplete: 'url', spellcheck: false, autocapitalize: 'off' });
  const email = h('input.input#su-email', { name: 'email', type: 'email', value: st.email || '', placeholder: 'you@example.com', autocomplete: 'email' });
  const token = h('input.input.mono#su-token', { name: 'token', type: 'password', autocomplete: 'off', spellcheck: false, 'data-1p-ignore': true, 'data-bwignore': true, 'data-lpignore': 'true', // laneway keeps it, not the password manager
    placeholder: st.envToken ? 'empty: uses JIRA_API_TOKEN' : 'paste it here' });
  const show = h('button.btn.ghost.su-show', { type: 'button', onclick: () => { const on = token.type === 'password'; token.type = on ? 'text' : 'password'; show.textContent = on ? 'Hide' : 'Show'; } }, 'Show');
  const keyring = st.keyring ? h('input#su-keyring', { type: 'checkbox', checked: true }) : null;
  const connect = h('button.btn.primary.su-go', { type: 'submit' }, 'Connect');
  const status = h('div.su-status', { role: 'status', 'aria-live': 'polite' });
  const tokenLink = h('a', { href: st.tokenURL, target: '_blank', rel: 'noopener' }, 'Atlassian\'s API token page');

  const form = h('form.su-form', { novalidate: true, onsubmit: e => { e.preventDefault(); send(false); } },
    field('site', 'Your Jira address', site,
      h('p.su-hint', 'The address you open Jira at in the browser, like https://acme.atlassian.net. Just acme works too.')),
    field('email', 'Your email', email,
      h('p.su-hint', 'The email address you sign in to Jira with.')),
    field('token', 'An API token', h('div.su-token', token, show),
      h('p.su-hint', 'A password for laneway only. You can delete it at any time; your own password stays as it is.'),
      h('ol.su-steps',
        h('li', 'Open ', tokenLink, ' (it opens in a new tab) and sign in if it asks.'),
        h('li', 'Click ', h('b', 'Create API token'), '. Name it ', h('b', 'laneway'), ', pick how long it lasts, and click ', h('b', 'Create'), '.'),
        h('li', 'Click ', h('b', 'Copy'), ', come back to this tab and paste it below.')),
      st.envToken ? h('p.su-hint', 'JIRA_API_TOKEN is set: leave this empty to use it.') : null),
    keyring ? h('label.check.su-keyring', keyring, 'Keep the token in this computer\'s keyring instead of in the config file') : null,
    h('div.su-actions', connect, status));

  clear(el).append(h('div.su-wrap', h('div.su-card',
    h('h2', again ? 'Sign in to Jira again' : 'Connect laneway to Jira'),
    h('p.su-intro.dim', again
      ? 'The config has this site, but not everything it needs to sign in. Fill in what is missing.'
      : 'laneway shows your Jira boards and works on them as you. Tell it where your Jira is and give it a token to sign in with. Works with Jira Cloud.'),
    form,
    again ? null : h('p.su-demo.dim', 'Just looking? ', h('button.btn.link', { type: 'button', onclick: () => send(true) }, 'Try the demo'), ' on a made-up board first; nothing to set up.'),
    h('p.su-foot.faint', 'Saved on this computer in ', h('code', st.configPath || 'the config file'), '. In a terminal, ', h('code', 'laneway setup'), ' does the same.'))));
  (site.value ? email.value ? token : email : site).focus();

  let busy = false;
  async function send(demo) {
    if (busy) return;
    busy = true; connect.disabled = true;
    for (const k in errs) errs[k].textContent = '';
    status.className = 'su-status dim';
    status.textContent = demo ? 'Starting the demo…' : 'Signing in to Jira…';
    try {
      const r = await api.post('/setup', demo ? { Demo: true } : { Site: site.value, Email: email.value, Token: token.value, Keyring: !!(keyring && keyring.checked) });
      status.className = 'su-status ok';
      status.textContent = demo ? 'Opening the demo…' : `Signed in as ${r.who}. Opening your boards…`;
      await waitForApp();
    } catch (e) {
      busy = false; connect.disabled = false;
      status.textContent = '';
      const f = e.fields && Object.keys(e.fields)[0];
      if (f && errs[f]) { errs[f].textContent = e.fields[f]; ({ site, email, token })[f].focus(); } else { status.className = 'su-status err'; status.textContent = e.message; }
    }
  }
}

// The server swaps itself for the app; reload once it answers as that.
async function waitForApp() {
  for (let i = 0; i < 100; i++) {
    await new Promise(r => setTimeout(r, 300));
    try {
      const res = await fetch('/api/session');
      if (res.ok && !(await res.json()).setup) { location.reload(); return; }
    } catch (e) { /* restarting */ }
  }
  throw new Error('laneway did not come back; start it again.');
}
