# Roadmap

## Next
- Images via kitty graphics protocol (Unicode placeholders, as matterbox's
  inlineimg/emojiimg): step B = probe support at startup, fetch + downscale
  `attachment:<id>` images, transmit via tea.Raw, draw placeholder rows in the
  panel; strip `imgIndicatorMark` (NULs currently reach the terminal)
- `n` create issue from board (`jira.CreateIssue` exists, unused in UI)
- List mode: sort by priority / points / assignee
- Priority marker on cards
- Lanes render ~0.7ms / View ~1ms at 600 cards: fine for now, revisit if boards grow

## Done
- Images step A: issue attachments, ADF media → `![name](attachment:<id>)`, `AttachmentContent` download
- Idle auto-refresh every 2m (skips modals, search, drag, loading)
- List mode cursor move 17ms → 0.24ms at 600 cards (row cache, no re-measure)
- `#` go to issue by key (bare number uses board project)
- `y` / `Y` copy issue key / URL (OSC 52), board and panel
- `?` help overlay for board and panel keys
- `/` search: filter loaded cards by key, summary, assignee
