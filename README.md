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

Optional `ui:` section (defaults shown):

```yaml
ui:
  auto_refresh: 2m   # idle board refetch; "off" disables
  stale_after: 1m    # older boards refetch on focus/tick
  images: auto       # kitty/Ghostty inline images; "off"
  image_max_rows: 16
  panel_width: 50    # issue panel, percent of the width

  keys:              # rebind any action: one key or a list
    search: f
    mine: [m, M]
```

Actions: up down left right top bottom page_up page_down open toggle_panel
browser refresh quit help search goto copy_key copy_url move_left move_right
project board next_view prev_view toggle_mode sort assignee_filter mine
clear_filters · panel: status priority points assign comment reply start_work
linked_issue back. The `?` overlay shows the live bindings.

A bad value keeps its default and is reported on the status line.

State (last project/board/view, filters, cached boards) lives in
`~/.config/jiratui/state.json`.

## Keys

Board: `p` project · `b` board · `[` `]` view · `t` lanes/list · `s` sort list · `a` assignee · `m` mine ·
`1-9` quick filters · `0` clear · `/` search (esc clears) · `H`/`L` move card · `enter` open · `#` go to key · `o` browser · `y`/`Y` copy key/URL ·
`r` refresh · `tab` panel · `?` help · `q` quit. Cards drag between lanes with the mouse.

Panel: `s` status · `p` priority · `P` points · `a` assignee · `m` mine · `c` comment ·
`R` reply · `backspace` previous issue · `L` linked issue · `S` start work · `o` browser · `y`/`Y` copy · `r` refresh · `esc` close.

## Build

`make` · `make test` · `make install`

## Images

In kitty or Ghostty, images embedded in an issue's description and comments are
drawn inline in the panel. Elsewhere (and inside tmux) they show as a caption.
`JIRATUI_IMAGES=0` turns them off.
