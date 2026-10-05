import { DEFAULTS } from './settings.js';
import { parseHosts } from './map.js';

const $ = id => document.getElementById(id);
const o = await chrome.storage.sync.get(DEFAULTS);
$('laneway').value = o.laneway;
$('hosts').value = o.hosts;
$('enabled').checked = o.enabled;

$('save').onclick = () => {
  const msg = t => { $('msg').textContent = t; };
  let laneway;
  try { laneway = new URL($('laneway').value.trim() || DEFAULTS.laneway); } catch { return msg('laneway: not a URL'); }
  const hosts = parseHosts($('hosts').value);
  // Bundled: *.atlassian.net and a loopback laneway. Anything else is asked for now, inside the click.
  const origins = [laneway, ...hosts.map(h => new URL('https://' + h.host))]
    .filter(u => !/(^|\.)atlassian\.net$/.test(u.hostname) && !['127.0.0.1', 'localhost'].includes(u.hostname))
    .map(u => u.protocol + '//' + u.hostname + '/*');
  const granted = origins.length ? chrome.permissions.request({ origins }) : Promise.resolve(true);
  granted.then(async ok => {
    if (!ok) return msg('Not saved: the browser needs access to ' + origins.join(', '));
    await chrome.storage.sync.set({ laneway: laneway.origin, hosts: $('hosts').value, enabled: $('enabled').checked });
    msg('Saved');
  }, e => msg(String(e)));
};
