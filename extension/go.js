// The redirect rule lands here with the Jira URL after #. Off to laneway, or
// back to Jira (past the rule) when laneway has no view for it. Anything but http(s) stays here.
import { toLaneway, parseHosts, bypass } from './map.js';
import { DEFAULTS } from './settings.js';

const src = location.hash.slice(1);
const o = await chrome.storage.sync.get(DEFAULTS);
const to = toLaneway(src, o.laneway);
if (!to) {
  const back = bypass(src);
  if (back) location.replace(back);
} else {
  // laneway follows the lw_site cookie (lib/sites.js); a host without "= site" keeps the current one.
  const host = new URL(src).hostname, h = parseHosts(o.hosts).find(x => x.host === host);
  if (h && h.site !== null) {
    try {
      await chrome.cookies.set({ url: o.laneway, name: 'lw_site', value: 's:' + encodeURIComponent(h.site), path: '/', sameSite: 'strict', expirationDate: Date.now() / 1000 + 31536000 });
    } catch (e) { console.warn('laneway: site cookie', e); }
  }
  location.replace(to);
}
