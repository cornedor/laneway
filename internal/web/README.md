# laneway web

`laneway web` (alias `serve`) serves the board in a browser. Flags: `-addr 127.0.0.1:8484`, `-remote`, `-no-open`, `-demo`; global `-site` / `-config` as usual.

- Server: Go, `internal/web`. One file per area, `api_<area>.go`, each registering routes in `init()` with `get/post/put/del("/path/{param}", func(ctx, s, r) (any, error))` (see `server.go`). Handlers wrap `s.Client()` (`*jira.Client`) and return its structs as they are: JSON keys are the Go field names (`card.Key`, `issue.Summary`). Zero times arrive as `0001-01-01…` (`fmt.isZero`). Errors become `{error}` with a status. Writes must be POST/PUT/DELETE.
- Frontend: `internal/web/static`, plain ES modules, no build step, no dependencies, embedded in the binary. Go: `go build`. JS: edit and reload (run `go run . web -demo -no-open` and open http://127.0.0.1:8484; the `-demo` project needs no Jira).
- Themes: only CSS variables from `css/themes.css`; never raw colours. Density via `--pad --row --fs`.
- Keyboard first: every action has a key, registered on a `keys.scope` so `?` lists it.

## Shell API (`js/app.js`, `window.laneway` in the console)

```
app.api      get/post/put/del/swr   (lib/api.js; swr(path, onData) answers from cache first)
app.bus      on/emit                issue:changed {key} after ANY write (board/panel refetch), panel, route, focus
app.keys     scope(name) → {bind(spec, fn, desc, {input,hidden,group}), dispose()}   (lib/keys.js)
app.ui       toast errToast modal pick prompt confirm avatar chip statusPill          (lib/ui.js)
app.commands register({id,title,group,run,when}) → unregister                         (palette lists these)
app.go('/board/ABC/1?issue=ABC-1'), app.query(), app.setQuery({issue})
app.panel    open(key) / close()    the right-hand issue panel (js/views/issue.js: mountIssue(el, key, {app, full}) → cleanup)
app.actions  edit(key, field[, anchor]) transition(key) create({project,parent,type}) palette(mode) jump() bulk(keys)
app.prefs    get/set                per-site prefs on the server, mirrored to localStorage
app.session  {site, sites, demo, baseURL, me:{AccountID,DisplayName}, projects, ui}
```

A view is `export default function mount(el, {app, params, query, scope, toolbar}) → cleanup?`; `scope` is its key scope (auto-disposed), `toolbar` the header slot for filters/buttons. Routes: `js/views/index.js`.

## Conventions

- Performance is a feature: render from cached data first (`api.swr`), patch DOM instead of rebuilding, virtualise lists over ~150 rows, delegate events, no layout thrash, no dependencies. Optimistic updates for moves/edits, rolled back with a toast on error.
- After any write call `app.bus.emit('issue:changed', {key})`.
- Mouse works, keyboard is complete. `j/k` and arrows move, `enter` opens, `esc` backs out, `x` selects, `?` shows keys.
- CSS per area in `css/<area>.css`, loaded with `import { css } from '../lib/css.js'; css('board')`.
- Reference for behaviour: the TUI in `internal/ui` (keys in `internal/ui/keys.go`, guide in `docs/guide`).
