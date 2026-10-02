// Run on each page load. The page's Notification and app badge post to the
// desktop app, which shows native ones and calls back into __lanewayDesktop.
// Links that would open a tab go to the default browser. On macOS the header
// sits beside the window buttons and drags the window.
(() => {
  if (window.__lanewayDesktop) return;
  const post = (m) => {
    const s = typeof m === 'string' ? m : JSON.stringify(m);
    if (window.chrome?.webview) window.chrome.webview.postMessage(s);
    else window.webkit?.messageHandlers?.external?.postMessage(s);
  };
  const mac = /Mac/.test(navigator.platform);
  const root = document.documentElement;
  root.dataset.desktop = mac ? 'mac' : 'other';

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
  window.Notification = DesktopNotification;
  navigator.setAppBadge = (n) => { post({ type: 'badge', n: n ?? 0 }); return Promise.resolve(); };
  navigator.clearAppBadge = () => navigator.setAppBadge(0);

  // An http(s) or mailto URL, else null.
  const outward = (href) => {
    try {
      const u = new URL(href, location.href);
      return /^(https?|mailto):$/.test(u.protocol) ? u : null;
    } catch { return null; }
  };
  const open = (u) => post({ type: 'open', url: u.href });
  window.open = (href) => {
    if (href) { const u = outward(href); if (u) open(u); return null; }
    // open() first, location after (xterm does): a window that only takes a location.
    return { opener: null, close() {}, location: { set href(v) { const u = outward(v); if (u) open(u); } } };
  };
  // Last in line, so links the page handles itself (preventDefault) stay in.
  const onLink = (e) => {
    if (e.defaultPrevented || e.button > 1) return;
    const a = e.target.closest?.('a[href]'), u = a && outward(a.href);
    if (!u) return;
    const tab = a.target === '_blank' || e.button === 1 || e.metaKey || e.ctrlKey || e.shiftKey;
    if (u.origin === location.origin && !tab) return;
    e.preventDefault();
    open(u);
  };
  window.addEventListener('click', onLink);
  window.addEventListener('auxclick', onLink);

  if (mac) {
    const css = document.createElement('style');
    css.textContent = `
      :root[data-desktop=mac] { --top-h: 52px; } /* a macOS toolbar's height: the window buttons centre in it */
      :root[data-desktop=mac] .pal-btn { height: 28px; }
      :root[data-desktop=mac] #top { padding-top: 2px; } /* the logo's middle on the buttons' */
      :root[data-desktop=mac]:not([data-fullscreen]) #top { padding-left: 96px; }
      :root[data-desktop=mac] #top { -webkit-user-select: none; user-select: none; }`;
    root.append(css);
    // As the Wails runtime does: a press on bare header, then a move, drags.
    const bare = (e) => !!e.target.closest?.('#top') && !e.target.closest('a,button,input,select,textarea,summary,[role],[tabindex]');
    let armed = false;
    window.addEventListener('mousedown', (e) => { armed = e.button === 0 && e.detail === 1 && bare(e); }, true);
    window.addEventListener('mousemove', () => { if (armed) { armed = false; post('wails:drag'); } }, true);
    window.addEventListener('mouseup', () => { armed = false; }, true);
    window.addEventListener('dblclick', (e) => { if (bare(e)) post('wails:drag:doubleclick'); }, true);
  }

  window.__lanewayDesktop = {
    permission(p) { perm = p; waiting.splice(0).forEach((r) => r(p)); },
    click(id) { const n = shown.get(id); shown.delete(id); n?.onclick?.(); },
    update() { post({ type: 'update' }); },
    fullscreen(on) { if (on) root.dataset.fullscreen = ''; else delete root.dataset.fullscreen; },
  };
  // The page has no Wails runtime; without this, ExecJS (our answers) waits for it.
  post('wails:runtime:ready');
  post({ type: 'check' });
})();
