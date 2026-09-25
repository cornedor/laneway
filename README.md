# jiratui

matterbox's Jira tab and issue panel as a standalone TUI: one board of one
project as swim lanes or a list, the selected issue on the right.

## Config

`~/.config/jiratui/config.yaml`, falling back to matterbox's
`~/.config/matterbox/config.yaml`. Same `jira:` section:

```yaml
jira:
  base_url: https://your-instance.atlassian.net
  email: you@example.com
  api_token: ...          # or JIRA_API_TOKEN
  projects: [ABC]         # listed first in the project picker
  repos: {ABC: ~/src/abc} # for S (start work in a herdr worktree)
```

State (last project/board/view, filters, cached boards) lives in
`~/.config/jiratui/state.json`.

## Keys

Board: `p` project · `b` board · `[` `]` view · `t` lanes/list · `a` assignee ·
`1-9` quick filters · `0` clear · `H`/`L` move card · `enter` open · `o` browser ·
`r` refresh · `tab` panel · `q` quit. Cards drag between lanes with the mouse.

Panel: `s` status · `p` priority · `P` points · `a` assignee · `c` comment ·
`R` reply · `S` start work · `o` browser · `r` refresh · `esc` close.

## Build

`make` · `make test` · `make install`
