# Roadmap

## Next
- `n` create issue from board (`jira.CreateIssue` exists, unused in UI)
- Auto-refresh board in the background when idle
- List mode: sort by priority / points / assignee
- Priority marker on cards
- Lanes render ~0.7ms / View ~1ms at 600 cards: fine for now, revisit if boards grow

## Done
- List mode cursor move 17ms → 0.24ms at 600 cards (row cache, no re-measure)
- `#` go to issue by key (bare number uses board project)
- `y` / `Y` copy issue key / URL (OSC 52), board and panel
- `?` help overlay for board and panel keys
- `/` search: filter loaded cards by key, summary, assignee
