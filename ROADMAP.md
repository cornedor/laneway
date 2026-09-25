# Roadmap

## Next
- Configurable (user ask, 2026-09-25) — `ui:` section, one step per iteration:
  - warn when two actions in one scope share a key
  - theme: issue type icon colours; named presets (`theme: tokyonight`)
  - `views:` extra JQL-backed views next to sprint/backlog
  - `quick_filters:` local JQL presets alongside the board's
  - `card_limit` (now fixed CardLimit)
- Images follow-ups: verify in real kitty/ghostty;
  query real cell pixel size (CSI 16 t); tmux passthrough;
  `enter` on an image for a full-size view
- `n` create issue from board (`jira.CreateIssue` exists, unused in UI)
- Lanes render ~0.7ms / View ~1ms at 600 cards: fine for now, revisit if boards grow

- Startup: show the cached board instantly even before config loads (measure cold start)

## Done
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
- Images step B: attachment images drawn inline in the panel (kitty Unicode placeholders, kitty/ghostty auto-detected, `JIRATUI_IMAGES=0` off)
- Images step A: issue attachments, ADF media → `![name](attachment:<id>)`, `AttachmentContent` download
- Idle auto-refresh every 2m (skips modals, search, drag, loading)
- List mode cursor move 17ms → 0.24ms at 600 cards (row cache, no re-measure)
- `#` go to issue by key (bare number uses board project)
- `y` / `Y` copy issue key / URL (OSC 52), board and panel
- `?` help overlay for board and panel keys
- `/` search: filter loaded cards by key, summary, assignee
