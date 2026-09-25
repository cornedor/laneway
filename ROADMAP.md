# Roadmap

## Next
- `rules:` next steps (user ask 2026-09-25, like matterbox's docs/rules.md):
  triggers beyond refresh (a schedule; boards not open).
  Open: may rules write to Jira (transition, comment)? Ask first.
- Images follow-ups: verify in real kitty/ghostty (cell size query, full view); tmux passthrough
- Lanes render ~0.7ms / View ~1ms at 600 cards: fine for now, revisit if boards grow

- Startup: show the cached board instantly even before config loads (measure cold start)

## Done
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
