# Roadmap

## Next
- Images via kitty graphics protocol: inline attachments/media in description and
  comments (now `_[attachment]_`); fetch via authed REST, fall back to a link
  outside kitty-capable terminals
- `n` create issue from board (`jira.CreateIssue` exists, unused in UI)
- List mode: sort by priority / points / assignee
- Priority marker on cards
- Lanes render ~0.7ms / View ~1ms at 600 cards: fine for now, revisit if boards grow

## Done
- Idle auto-refresh every 2m (skips modals, search, drag, loading)
- List mode cursor move 17ms → 0.24ms at 600 cards (row cache, no re-measure)
- `#` go to issue by key (bare number uses board project)
- `y` / `Y` copy issue key / URL (OSC 52), board and panel
- `?` help overlay for board and panel keys
- `/` search: filter loaded cards by key, summary, assignee
