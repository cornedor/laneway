// Keeps the redirect rules in step with the options and the toolbar toggle.
// A Jira page opened from laneway, from Jira itself or from an Atlassian login
// stays in Jira: the rule skips those initiators.
import { PATHS, BYPASS, parseHosts } from './map.js';
import { DEFAULTS } from './settings.js';

const ALLOW = 1, REDIRECT = 2;

async function sync() {
  const o = await chrome.storage.sync.get(DEFAULTS);
  const hosts = parseHosts(o.hosts).map(h => h.host);
  let laneway = '';
  try { laneway = new URL(o.laneway).hostname; } catch {}
  const addRules = !o.enabled || !laneway ? [] : [
    { id: ALLOW, priority: 2, action: { type: 'allow' }, condition: { urlFilter: BYPASS, resourceTypes: ['main_frame'] } },
    {
      id: REDIRECT, priority: 1,
      action: { type: 'redirect', redirect: { regexSubstitution: chrome.runtime.getURL('go.html') + '#\\0' } },
      condition: {
        regexFilter: '^https?://[^/]+/' + PATHS + '.*',
        requestDomains: hosts.length ? hosts : ['atlassian.net'],
        resourceTypes: ['main_frame'],
        excludedInitiatorDomains: [laneway, 'atlassian.net', 'atlassian.com', ...hosts],
      },
    },
  ];
  await chrome.declarativeNetRequest.updateDynamicRules({ removeRuleIds: [ALLOW, REDIRECT], addRules });
  await chrome.action.setBadgeText({ text: o.enabled ? '' : 'off' });
  await chrome.action.setTitle({ title: o.enabled ? 'Jira opens in laneway (click: off)' : 'Jira opens in Jira (click: on)' });
}

chrome.runtime.onInstalled.addListener(sync);
chrome.runtime.onStartup.addListener(sync);
chrome.storage.onChanged.addListener(sync);
chrome.action.onClicked.addListener(async () => {
  const { enabled } = await chrome.storage.sync.get({ enabled: DEFAULTS.enabled });
  await chrome.storage.sync.set({ enabled: !enabled });
});
