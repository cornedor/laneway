# Roadmap

## Next
- Images follow-ups: verify in real kitty/ghostty; free images on quit (`a=d`);
  re-fit on panel resize; query real cell pixel size (CSI 16 t); tmux passthrough;
  `enter` on an image for a full-size view
- `n` create issue from board (`jira.CreateIssue` exists, unused in UI)
- List mode: sort by priority / points / assignee
- Lanes render ~0.7ms / View ~1ms at 600 cards: fine for now, revisit if boards grow

## Done
- Priority marks (⇈ ↑ ↓ ⇊, medium silent) on lane cards and list rows
- Images step B: attachment images drawn inline in the panel (kitty Unicode placeholders, kitty/ghostty auto-detected, `JIRATUI_IMAGES=0` off)
- Images step A: issue attachments, ADF media → `![name](attachment:<id>)`, `AttachmentContent` download
- Idle auto-refresh every 2m (skips modals, search, drag, loading)
- List mode cursor move 17ms → 0.24ms at 600 cards (row cache, no re-measure)
- `#` go to issue by key (bare number uses board project)
- `y` / `Y` copy issue key / URL (OSC 52), board and panel
- `?` help overlay for board and panel keys
- `/` search: filter loaded cards by key, summary, assignee
