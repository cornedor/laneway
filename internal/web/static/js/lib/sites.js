// Jira sites from the config: a chip beside the brand (with more than one) and `@` pick one; the server keeps
// every site open and follows the lw_site cookie, so switching is a reload. As the TUI, the picker's last row
// adds a site: the server signs in, writes it to the config and comes back on it.
import { h, $ } from './dom.js';
import { css } from './css.js';

const ADD = '\u0000add'; // no site name looks like this

export function install(app) {
  const label = s => s || app.session.defaultName || 'jira';
  const sites = app.session.sites || [], add = app.session.addSite;
  const cur = app.session.site;
  if (sites.length > 1) $('#site').append(h('button.site-chip', { type: 'button', title: 'Jira site: ' + label(cur) + ' · @ switches', onclick: () => choose() }, label(cur)));
  if (sites.length < 2 && !add) return;

  async function go(s) {
    if (s === null || s === cur) return;
    if (s === ADD) return addSite(app, add);
    try { await app.api.post('/site', { Site: s }); } catch (e) { return app.ui.errToast(e); }
    setCookie(s); reload();
  }
  const items = () => [...(sites.length ? sites : [cur]), ...(add ? [ADD] : [])];
  const choose = async () => go(await app.ui.pick({ title: 'Jira site', items: items(), label: x => (x === ADD ? '+ add a Jira site' : label(x)), detail: x => (x === cur ? 'current' : ''), placeholder: 'Site…' }));
  app.keys.scope('sites').bind('@', choose, 'switch or add a Jira site', { group: 'Global' });
  app.commands.register({ id: 'site', title: 'Switch Jira site', group: 'App', run: choose });
  for (const s of sites) if (s !== cur) app.commands.register({ id: 'site:' + s, title: 'Site: ' + label(s), group: 'App', run: () => go(s) });
  if (add) app.commands.register({ id: 'site:add', title: 'Add a Jira site', group: 'App', run: () => addSite(app, add) });
}

const setCookie = s => { document.cookie = 'lw_site=s:' + encodeURIComponent(s) + '; path=/; max-age=31536000; samesite=strict'; };
const reload = () => { location.href = '/board'; };

// The form `laneway setup` asks with a site set up: address, email, token and a name to pick it by.
function addSite(app, info) {
  css('setup');
  const errs = {};
  const field = (name, label, input, hint) => {
    errs[name] = h('div.form-err', { role: 'alert' });
    return h('div.su-field', h('label.su-label', label), hint && h('p.su-hint', hint), input, errs[name]);
  };
  const inp = (o) => h('input.input', { spellcheck: false, autocapitalize: 'off', ...o });
  const f = {
    site: inp({ placeholder: 'acme.atlassian.net', autocomplete: 'url' }),
    email: inp({ type: 'email', placeholder: 'you@example.com', autocomplete: 'email', value: (app.session.me && app.session.me.Email) || '' }),
    token: inp({ type: 'password', autocomplete: 'off', 'data-1p-ignore': true, 'data-lpignore': 'true', placeholder: info.envToken ? 'empty: uses JIRA_API_TOKEN' : 'paste it here' }),
    name: inp({ placeholder: 'empty: named after its address' }),
  };
  const keyring = info.keyring ? h('input', { type: 'checkbox', checked: true }) : null;
  const status = h('div.su-status', { role: 'status', 'aria-live': 'polite' });
  const ok = h('button.btn.primary', { type: 'submit' }, 'Add and switch');
  const form = h('form.su-form', { novalidate: true, onsubmit: e => { e.preventDefault(); send(); } },
    field('site', 'Its Jira address', f.site),
    field('email', 'Your email there', f.email),
    field('token', 'An API token', f.token, h('span', 'Make one on ', h('a', { href: info.tokenURL, target: '_blank', rel: 'noopener' }, 'Atlassian\'s API token page'), '.')),
    field('name', 'A name for it, to pick it by', f.name, 'Lower-case letters, digits, - and _.'),
    keyring && h('label.check', keyring, 'Keep the token in this computer\'s keyring instead of in the config file'),
    h('div.su-actions', ok, status),
    h('p.su-foot.faint', 'Saved in ', h('code', info.configPath || 'the config file'), '. In a terminal, ', h('code', 'laneway setup'), ' does the same.'));
  app.ui.modal(form, { title: 'Add a Jira site' });
  f.site.focus();
  let busy = false;
  async function send() {
    if (busy) return;
    busy = true; ok.disabled = true;
    for (const k in errs) errs[k].textContent = '';
    status.className = 'su-status dim'; status.textContent = 'Signing in to Jira…';
    try {
      const r = await app.api.post('/sites', { Site: f.site.value, Email: f.email.value, Token: f.token.value, Name: f.name.value, Keyring: !!(keyring && keyring.checked) });
      status.className = 'su-status ok'; status.textContent = 'Signed in as ' + r.who + '. Opening ' + r.site + '…';
      setCookie(r.site); // the server asks the cookie's site
      await comeBack(r.site);
      reload();
    } catch (e) {
      busy = false; ok.disabled = false; status.textContent = '';
      const k = e.fields && Object.keys(e.fields)[0];
      if (k && errs[k]) { errs[k].textContent = e.fields[k]; f[k].focus(); } else { status.className = 'su-status err'; status.textContent = e.message; }
    }
  }
}

// restart asks the server to start again (the config read anew) and reloads the page once it answers.
export async function restart(app) {
  try { await app.api.post('/restart'); } catch (e) { return app.ui.errToast(e); }
  app.ui.toast('Restarting laneway web…');
  try { await comeBack(app.session.site); } catch (e) { return app.ui.toast('laneway web did not come back; start it again.', { kind: 'err' }); }
  location.reload();
}

// The server starts again on the added site; wait until it answers.
async function comeBack(site) {
  await new Promise(r => setTimeout(r, 600));
  for (let i = 0; i < 100; i++) {
    try { const res = await fetch('/api/session'); if (res.ok && (await res.json()).site === site) return; } catch (e) { /* restarting */ }
    await new Promise(r => setTimeout(r, 300));
  }
  throw new Error('laneway did not come back on ' + site + '; start it again.');
}
