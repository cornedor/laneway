# Roadmap

## Next
- Images follow-ups: verify in real kitty/ghostty;
  query real cell pixel size (CSI 16 t); tmux passthrough;
  `enter` on an image for a full-size view
- `n` create issue from board (`jira.CreateIssue` exists, unused in UI)
- Lanes render ~0.7ms / View ~1ms at 600 cards: fine for now, revisit if boards grow

- Panel: `backspace` back to the previous issue (history stack)
- Cards/panel: show parent / epic
- Panel: list attachments not embedded in the body (names, `o` to open)
- Panel: linked issues section, `enter` to follow
- Board: WIP limit per column (board config has min/max), mark over-limit lanes
- Startup: show the cached board instantly even before config loads (measure cold start)

## Done
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
