# Roadmap

## Next
- Panel field cursor step 3: sprint, parent/epic, datetime, multi-line text
  fields in `$EDITOR`
- Roadmap step 3: plan-level parents above epics (hierarchy > 1), `n` new
  epic, a board filter for "cards in this epic", child bars movable too
- `/` over every stored issue of the project, not just the loaded view
- JQL step 2: save a search as a `ui.views` entry (writes the config), a
  history of past searches (↑ in an empty input)
- Bulk step 3: required transition fields asked once for all marked
- Planning step 3: start / complete a sprint from here (unfinished issues to the next)
- Charts step 2: scope changes from the changelog (added/removed mid
  sprint), cumulative flow by status, burnup; `ui.charts.sprints`
- Worklogs step 2: timesheet by week (`[` `]` days), edit/delete an entry,
  `S` start work also starts the timer (`ui.timer_on_start`)
- Description step 2: keep unknown nodes as placeholders (`<!-- adf:1 -->`)
  so tables and mentions survive an edit; escape markdown-like text; `E` for
  comments too
- Inbox step 2: an unread count in the header, polled with the idle
  refresh; notify (OSC 777) on a new mention
- Links step 2: remove a link from the Links section, vote, move a subtask
  to another parent
- Dev info step 2: an open PR marked on its board card (summary counts,
  fetched with the prefetch), commits
- Attachments step 2: path completion in the upload input; paste an image from the clipboard
- Standup step 2: pick the day range, `ui.standup` template for the text
- Lanes render ~0.7ms / View ~1ms at 600 cards: fine for now, revisit if boards grow

## Done
- Planning: `ui.capacity` per person (red when over), marked cards move together
- `X` marks the lane or every shown row (toggle)
- `Q` JQL search: field / keyword / value completion from Jira, results as a view
- Idle refresh fetches only issues updated since the last fetch (merged by key); whole again every 10m or on a filter change
- Panel `A`: upload a file, download an attachment to ~/Downloads (never overwrites)
- Panel `D`: pull requests (open first) and branches from the dev-status API, enter opens
- Panel `H` history: every change and comment, newest first, filterable
- `U` standup: your moves, edits, comments and worklogs since the previous workday, copy as text
- Cursor prefetch: the card and 2 either side load once it rests; issue cache lives 2m
- `rules:` Jira actions `transition` (to:) and `comment`, only on others' changes (no loops)
- `sites:` more Jira instances: `-site name` or `@`, each with its own state file
- Starred Jira filters as views (their own search, ORDER BY kept); `ui.saved_filters: off`
- Panel `A`: new subtask (epic: child issue), link to an issue (either direction), clone, watch
- `I` inbox: others' changes, comments and mentions on your issues since last read
- `E` description in `$EDITOR`: markdown ⇄ ADF, offered only when the round trip is exact
- Worklogs: `w` logs (1h 30m …), `T` timer kept across restarts, `W` today's timesheet
- `C` charts: burndown (braille, ideal line) and velocity of the last 8 sprints
- `P` sprint planning: backlog beside a sprint, points per assignee, move across, `K`/`J` rank
- Bulk edit: `x` marks, `B` sets status / priority / assignee / labels / points / sprint on all, 4 at a time
- `:` command palette: pane actions, views, quick filters, boards, loaded issues; pickers match every word
- Roadmap: `space` folds out an epic's issues, `H`/`L` move a bar, `<`/`>` its end, written after a pause
- `R` roadmap: epics on a timeline (own dates, else children's sprints), filled by points done, zoom and scroll
- Date fields (panel and transition form): 2026-10-01, today, +3d, -1w, fri
- Panel shows every other editable field (editmeta); the cursor edits them: text, number, people, options
- Panel field cursor: tab / shift-tab walk summary…labels, enter edits, esc drops
- Images checked by the user in real terminals (2026-09-25)
- Lane heads sum their story points (`· 8p`), hidden with points
- Sprint views show days left (or start/end date) and the goal
- Board `M`: move the card to a sprint or the backlog
- Panel `l`: edit labels, space separated
- Panel `e`: edit the summary
- `rules:` time triggers: a watch JQL (`NOT status CHANGED AFTER -3d`) + `on: new`, documented
- `laneway rules watch`: watches without the TUI (log, notify, exec)
- `rules:` `watch:` JQL + `every:`: rules fire on a polled search, any board open
- Images inside tmux: passthrough DCS per APC when allow-passthrough is on
- Cold start measured: process 10ms, cached board to first frame 1.6ms (137k state); nothing to do
- `i` in the panel: images full size across the body, ← → between them
- Images fit the terminal's real cell size (CSI 16 t), 10×20 until it answers
- `n` new issue: type, summary; lands in the shown sprint, opens in the panel
- `rules:` `by_me` condition: change author from the changelog, creator for new; `rules test -by-me`
- `laneway rules list` / `rules test`; matterbox config's chat `rules:` no longer read as ours
- `rules:` `highlight` action: ● on the card until opened (theme `highlight`)
- `rules:` `notify` (OSC 777) and `exec` (argv templates, JSON stdin, LANEWAY_* env) actions
- `rules:` engine: diff on refresh (new/status/assignee/priority/points/summary), globs + regexp + not, `log` action
- `ui.card_limit`: cards per view fetch (default 500)
- `ui.views`: JQL views of every board after sprint/backlog, lanes or list
- `ui.theme` presets: tokyonight, catppuccin, gruvbox (`theme: name` or `preset:` + overrides)
- `ui.theme`: issue type icon colours (type_bug … type_other)
- `ui.keys`: warn when a key does two things on the board or in the panel
- Renamed to laneway (module github.com/cornedor/laneway); jiratui config/state migrate
- `ui.quick_filters`: JQL presets before the board's own
- `ui.default_mode`, `ui.date_format`, `ui.card_fields`
- `ui.theme`: 16 named colours (ANSI or hex), validated
- `ui.keys`: rebind any of 39 actions; help overlay, header and filter hints follow
- `ui:` config: auto_refresh, stale_after, images, image_max_rows, panel_width (validated, warns)
- Cards show parent/epic (`⌃ Epic`), searchable
- Lane heads show `n/max` WIP limit, red when over (unfiltered board only)
- Panel Links section (parent, issue links, subtasks), `L` picks one to open
- Panel lists attachments not embedded in the body (linked, with size)
- Panel `backspace`: back to the previous issue
- `m` toggles assignee = me
- `s` cycles list sort: rank, priority, points, assignee, key (stable)
- Images re-fit to panel width (re-place without re-sending data)
- Free this session's kitty images on exit (by id)
- Priority marks (⇈ ↑ ↓ ⇊, medium silent) on lane cards and list rows
- Images step B: attachment images drawn inline in the panel (kitty Unicode placeholders, kitty/ghostty auto-detected, `LANEWAY_IMAGES=0` off)
- Images step A: issue attachments, ADF media → `![name](attachment:<id>)`, `AttachmentContent` download
- Idle auto-refresh every 2m (skips modals, search, drag, loading)
- List mode cursor move 17ms → 0.24ms at 600 cards (row cache, no re-measure)
- `#` go to issue by key (bare number uses board project)
- `y` / `Y` copy issue key / URL (OSC 52), board and panel
- `?` help overlay for board and panel keys
- `/` search: filter loaded cards by key, summary, assignee
