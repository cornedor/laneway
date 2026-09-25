# laneway

A terminal board for Jira. One board of one project as swim lanes or a list,
the selected issue in a panel on the right. Move cards, change status,
priority, points and assignee, comment and reply, all without leaving the
terminal.

- Swim lanes or a sortable list, with drag and drop between lanes
- Sprints, backlog and kanban boards; the board's quick filters plus your own
- Local search, "only mine", jump to any issue by key
- Issue panel with description, comments, links, subtasks and attachments
- Inline images in kitty and Ghostty
- Idle auto-refresh, cached boards for instant startup
- Every key and colour configurable

## Install

```sh
go install github.com/cornedor/laneway@latest
```

Or grab a binary from [Releases](https://github.com/cornedor/laneway/releases).

## Config

`~/.config/laneway/config.yaml`:

```yaml
jira:
  base_url: https://your-instance.atlassian.net
  email: you@example.com
  api_token: ...          # or JIRA_API_TOKEN
  projects: [ABC]         # listed first in the project picker
  repos: {ABC: ~/src/abc} # for S (start work in a herdr worktree)
```

Create a token at <https://id.atlassian.com/manage-profile/security/api-tokens>.
laneway only reads your Jira until you act: moving a card, editing a field or
commenting.

The optional `ui:` section (defaults shown):

```yaml
ui:
  auto_refresh: 2m   # idle board refetch; "off" disables
  stale_after: 1m    # older boards refetch on focus/tick
  images: auto       # kitty/Ghostty inline images; "off"
  image_max_rows: 16
  panel_width: 50    # issue panel, percent of the width
  default_mode: lanes           # or list; the last used mode wins after that
  date_format: 2006-01-02 15:04 # Go time layout
  card_fields: [type, priority, status, points, assignee, parent]
  quick_filters:                # JQL presets before the board's own (1-9)
    - {name: Bugs, jql: "type = Bug"}
  views:                        # extra views of every board, after its own ([ ])
    - {name: Mine, jql: "assignee = currentUser()"}
  keys:              # rebind any action: one key or a list
    search: f
    mine: [m, M]
  theme:             # colours: ANSI 0–255 or #rrggbb
    preset: tokyonight  # or catppuccin, gruvbox; `theme: gruvbox` alone works too
    accent: "#7aa2f7"
    dim: "244"
```

A bad value keeps its default and is reported on the status line, as is a key
bound to two actions on the board or in the panel.

Actions: up down left right top bottom page_up page_down open toggle_panel
browser refresh quit help search goto copy_key copy_url move_left move_right
project board next_view prev_view toggle_mode sort assignee_filter mine
clear_filters · panel: status priority points assign comment reply start_work
linked_issue back.

Colours: accent dim selection_fg selection_bg selection_idle error mention link
code attachment over_limit drop_fg priority_highest priority_high priority_low
priority_lowest type_bug type_story type_epic type_subtask type_other.

State (last project, board, view, filters, cached boards) lives in
`~/.config/laneway/state.json`. An existing `~/.config/jiratui` or matterbox
config is picked up as a fallback.

## Keys

`?` shows every key as bound.

Board: `p` project · `b` board · `[` `]` view · `t` lanes/list · `s` sort list ·
`a` assignee · `m` mine · `1-9` quick filters · `0` clear · `/` search ·
`H`/`L` move card · `enter` open · `#` go to key · `o` browser ·
`y`/`Y` copy key/URL · `r` refresh · `tab` panel · `q` quit. Cards drag
between lanes with the mouse.

Panel: `s` status · `p` priority · `P` points · `a` assignee · `c` comment ·
`R` reply · `L` linked issue · `backspace` previous issue · `S` start work ·
`o` browser · `y`/`Y` copy · `r` refresh · `esc` close.

## Images

In kitty or Ghostty, images embedded in an issue's description and comments
are drawn inline in the panel. Elsewhere (and inside tmux) they show as a
caption. `ui.images: off` or `LANEWAY_IMAGES=0` turns them off.

## Build

`make` · `make test` · `make install`. See [CONTRIBUTING.md](CONTRIBUTING.md).

## License

[MIT](LICENSE)
