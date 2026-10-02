// Run on each page load: the page's Notification posts to the desktop app,
// which shows a native one and calls click() when it is clicked.
(() => {
  if (window.__lanewayDesktop) return;
  const post = (m) => {
    const s = typeof m === 'string' ? m : JSON.stringify(m);
    if (window.chrome?.webview) window.chrome.webview.postMessage(s);
    else window.webkit?.messageHandlers?.external?.postMessage(s);
  };
  let perm = 'default', seq = 0;
  const waiting = [], shown = new Map();
  class DesktopNotification {
    constructor(title, o = {}) {
      this.id = 'n' + ++seq;
      this.onclick = null;
      shown.set(this.id, this);
      post({ type: 'notify', id: this.id, title, body: o.body || '', tag: o.tag || '' });
    }
    static get permission() { return perm; }
    static requestPermission() {
      return new Promise((r) => { waiting.push(r); post({ type: 'permission' }); });
    }
    close() {}
  }
  window.__lanewayDesktop = {
    permission(p) { perm = p; waiting.splice(0).forEach((r) => r(p)); },
    click(id) { const n = shown.get(id); shown.delete(id); n?.onclick?.(); },
  };
  window.Notification = DesktopNotification;
  // The page has no Wails runtime; without this, ExecJS (our answers) waits for it.
  post('wails:runtime:ready');
  post({ type: 'check' });
})();
