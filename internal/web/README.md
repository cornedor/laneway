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

## Keys

Scopes stack: global < view < panel (beside a board the panel's keys fire only with focus in it: `Tab`/click; `esc` closes) < modal. Where the TUI has a key (`internal/ui/keys.go`) the web uses the same; `?` lists what applies right now. Clashes were resolved like this: the TUI meaning stays, the other action moves.

| Scope | Keys |
| --- | --- |
| Global | `:` or `ctrl+k` palette, `/` search, `Q` JQL, `g g` jump to key, `n` new issue, `T` start/stop timer, `w` log work, `u` undo last edit, `?` help, `,` settings, `tab` panel/view focus, `esc` close panel |
| Go | `g b` board, `g p` planning, `g r` reports, `g m` roadmap, `g w` my work, `g i` inbox, `g s` standup, `g ,` settings, `g t` next theme |
| Board | `hjkl`/arrows move, `enter` open, `t` lanes/list, `s` status, `e` summary, `a` assign, `p` priority, `P` points, `H`/`L` move column, `J`/`K` rank, `x` mark, `X` bulk, `o` Jira, `y` copy key, `r` refresh, `O` swimlanes (lanes) / sort (list), `z`/`Z` fold/unfold swimlane, `c` one-line cards, `C` list columns, `F` filter builder, `*` pin, `.` repeat, `alt+j`/`alt+k` rank bottom/top, `ctrl+a` mark all, `ctrl+y` copy branch, `Y` copy link, `alt+t` time machine (`←` `→` `esc`), `alt+o` closed sprints, `B` board/project, `v` view, `[`/`]` previous/next view (sprints, backlog, `ui.views`, starred filters), `f` filter, `m` mine, `A` assignee, `0` clear, `1`-`9` quick filters |
| Issue panel | `j`/`k` comment or scroll, `c` comment, `R` reply, `e` summary (own focused comment), `E` description, `a` `p` `P` `l` assignee, priority, points, labels, `s` status, `L` link issue, `d` delete own comment, `[`/`]` prev/next issue, `1`-`3` tabs, `y`/`Y` copy key/link, `o` Jira, `r` refresh, `ctrl+enter` save |
| Planning | `jk` move, `J`/`K` rank, `m` move to sprint, `[`/`]` previous/next sprint, `x`/`space` select, `z` fold, `enter` open, `P` points, `a` assignee, `N` `S` `C` `E` new, start, complete, edit sprint, `b` board, `f` filter, `R` reload |
| Reports | `h`/`l`/`[`/`]`/arrows previous/next report, `1`-`7` pick, `s` sprint, `b` board, `W` weeks (cycle time), `r` release (releases), `R` reload, `jk` items |
| Roadmap | `jk` rows, `h`/`l` pan, `+`/`-` zoom, `.` today, `space` fold, `enter` open, `b` project, `n` new epic, `R` reload |
| My work | `jk` move, `enter` open, `1` `2` `3` issues/day/week, `W` day/week, `h`/`l` step, `0` today, `a` add worklog, `e` edit, `d` delete, `+` add row (week), `f` filter, `v` group, `r` refresh, `y` copy |
| Inbox | `jk`, `enter`, `e` done, `E` all read done, `s` snooze, `u` unread, `a` toggle read, `o` Jira, `r` refresh, `A` or `tab` inbox/all (`tab` goes to an open panel) |
| Standup | `jk`, `enter`, `[`/`]` day back/forward, `p` or `tab` mine/team, `P` project, `y` copy, `r` refresh |
| Agents (herdr; hidden without it) | `g a` agents, `g R` review (gh/glab), `S` start work on the selected issue (anywhere; focuses its agent if one runs), `ctrl+y` copy branch name; on the agents screen `jk`, `tab` worktrees, `enter` issue, `f` focus in herdr, `p` prompt, `N` new agent, `d` stop (twice), `o` `y` `r`; draft pull request in the palette |
| Settings | `jk`, `enter`/`space` change or edit, `h`/`l` cycle, `del` reset to default, `/` filter, `esc` leave (enter on a Keyboard row captures a new key) |

Moved because of a clash: board sort `S`->`O` (`S` starts work), timer `t`->`T` (board `t` is lanes/list), create `c`->`n` (panel `c` is comment), next theme `T`->`g t`, My work day/week `w`->`W` (`w` logs work), cycle-time weeks `w`->`W`, planning filter `/`->`f` and fold `o`->`z` (`/` is search, `o` is Jira), week-view add row `#`->`+`, issue panel: link `l`->`L` (`l` is labels), reply `r`->`R`, description `e`->`E`, `r` refreshes.

## Conventions

- Performance is a feature: render from cached data first (`api.swr`), patch DOM instead of rebuilding, virtualise lists over ~150 rows, delegate events, no layout thrash, no dependencies. Optimistic updates for moves/edits, rolled back with a toast on error.
- After any write call `app.bus.emit('issue:changed', {key})`.
- Mouse works, keyboard is complete. `j/k` and arrows move, `enter` opens, `esc` backs out, `x` selects, `?` shows keys.
- CSS per area in `css/<area>.css`, loaded with `import { css } from '../lib/css.js'; css('board')`.
- Reference for behaviour: the TUI in `internal/ui` (keys in `internal/ui/keys.go`, guide in `docs/guide`).

## Settings, remapping, phone

- `GET /api/settings` lists every `ui:` option (type, default, value, doc, choices, restart) from `internal/config/schema.go`, the table the terminal's settings screen uses; `PUT /api/settings/{name}` (`{Value}` or `{YAML}`, empty resets) validates like startup and writes through `config.SetUI` (comments kept). Needs `Options.ConfigPath` (demo gets a temp file).
- Key remaps: `lib/keys.js` gives each bind an id `scope:defaultKey`. Precedence: the `keymap` pref (`{id: [keys]}`, Settings > Keyboard), then `ui.keys` of the config for binds with a TUI action (`lib/keymap.js` maps scope+key to the action name), then the default. Binds seen are remembered in localStorage (`lw:keyreg`) so the settings page lists views not opened this session.
- `/sw.js` (from `static/sw.js`, version = hash of the embedded files) precaches the shell; `/api` is never cached. `lib/notify.js` `notify(title, body, onclick)` is a no-op unless the user turned notifications on in Settings.
- Long-press / right-click on a card, row or planning row opens an action menu (`lib/pwa.js`). `css/mobile.css` (bottom sheets, lane snap, safe areas, contrast, forced colours) and `css/print.css` are global.
