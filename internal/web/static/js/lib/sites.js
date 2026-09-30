// Jira sites from the config: a chip beside the brand and `@` pick one; the server keeps
// every site open and follows the lw_site cookie, so switching is a reload.
import { h, $ } from './dom.js';

const label = s => s || 'jira';

export function install(app) {
  const sites = app.session.sites || [];
  if (sites.length < 2) return;
  const cur = app.session.site;
  $('#site').append(h('button.site-chip', { type: 'button', title: 'Jira site: ' + label(cur) + ' · @ switches', onclick: () => choose() }, label(cur)));

  async function go(s) {
    if (s === null || s === cur) return;
    try { await app.api.post('/site', { Site: s }); } catch (e) { return app.ui.errToast(e); }
    document.cookie = 'lw_site=s:' + encodeURIComponent(s) + '; path=/; max-age=31536000; samesite=strict';
    location.hash = '#/board';
    location.reload();
  }
  const choose = async () => go(await app.ui.pick({ title: 'Jira site', items: sites, label, detail: x => x === cur ? 'current' : '', placeholder: 'Site…' }));
  app.keys.scope('sites').bind('@', choose, 'switch Jira site', { group: 'Global' });
  app.commands.register({ id: 'site', title: 'Switch Jira site', group: 'App', run: choose });
  for (const s of sites) if (s !== cur) app.commands.register({ id: 'site:' + s, title: 'Site: ' + label(s), group: 'App', run: () => go(s) });
}
