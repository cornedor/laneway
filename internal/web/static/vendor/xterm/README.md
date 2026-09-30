# xterm.js

The browser terminal (`js/lib/term.js`), loaded only when one opens. MIT (`LICENSE`, `LICENSE-addon-*`). From npm, the ESM builds (`lib/*.mjs`, renamed `.js` so they are served gzipped), `//# sourceMappingURL` lines dropped:

| File | Package |
| --- | --- |
| xterm.js, xterm.css | @xterm/xterm 6.0.0 |
| addon-fit.js | @xterm/addon-fit 0.11.0 |
| addon-webgl.js | @xterm/addon-webgl 0.19.0 |
| addon-unicode11.js | @xterm/addon-unicode11 0.9.0 |
| addon-web-links.js | @xterm/addon-web-links 0.12.0 |

Update: `npm pack @xterm/xterm @xterm/addon-…`, copy the same files.
