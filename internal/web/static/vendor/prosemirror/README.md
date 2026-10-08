# ProseMirror

The visual editor (`js/lib/rte.js`, schema in `js/lib/adf.js`), loaded with the first editor that shows it. MIT (`LICENSE-*`). From npm, each package's ESM build (`dist/index.js`, renamed to the package's name), its bare imports made relative (`from './prosemirror-model.js'`), `//# sourceMappingURL` lines dropped; the CSS from `style/`:

| File | Package |
| --- | --- |
| prosemirror-model.js | prosemirror-model 1.25.12 |
| prosemirror-state.js | prosemirror-state 1.4.4 |
| prosemirror-view.js, prosemirror.css | prosemirror-view 1.42.6 |
| prosemirror-transform.js | prosemirror-transform 1.12.2 |
| prosemirror-commands.js | prosemirror-commands 1.7.2 |
| prosemirror-keymap.js | prosemirror-keymap 1.2.3 |
| prosemirror-history.js | prosemirror-history 1.5.1 |
| prosemirror-inputrules.js | prosemirror-inputrules 1.5.1 |
| prosemirror-schema-list.js | prosemirror-schema-list 1.5.1 |
| prosemirror-dropcursor.js | prosemirror-dropcursor 1.8.4 |
| prosemirror-gapcursor.js, gapcursor.css | prosemirror-gapcursor 1.4.1 |
| prosemirror-tables.js, tables.css | prosemirror-tables 1.8.5 |
| orderedmap.js | orderedmap 2.1.1 |
| rope-sequence.js | rope-sequence 1.3.4 |
| w3c-keyname.js | w3c-keyname 2.2.8 |

Update: `npm pack <package>`, copy `package/dist/index.js` as above, then `sed -i -E "s#(from|import) ['\"](prosemirror-[a-z-]+|orderedmap|rope-sequence|w3c-keyname)['\"]#\1 './\2.js'#g" *.js`.
