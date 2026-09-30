// Browser notifications, opt-in (Settings > Notifications). notify(title, body, onclick) does nothing
// unless the user turned them on and the browser allows them; callers decide when it is worth a ping.
const app = () => window.laneway;
export const supported = () => typeof Notification !== 'undefined';
export const permission = () => (supported() ? Notification.permission : 'unsupported');
export const enabled = () => supported() && Notification.permission === 'granted' && !!app() && app().prefs.get('notify', 'off') === 'on';

// Turn them on or off; turning on asks the browser. Resolves to whether they are on.
export async function setEnabled(on) {
  if (!on) { app().prefs.set('notify', 'off'); return false; }
  if (!supported()) return false;
  const p = Notification.permission === 'default' ? await Notification.requestPermission() : Notification.permission;
  app().prefs.set('notify', p === 'granted' ? 'on' : 'off');
  return p === 'granted';
}

export function notify(title, body, onclick, tag) {
  if (!enabled()) return false;
  try {
    const n = new Notification(title, { body, tag: tag || title, icon: '/icon-192.png' });
    n.onclick = () => { window.focus(); n.close(); if (onclick) onclick(); };
    return true;
  } catch (e) { return false; }
}
