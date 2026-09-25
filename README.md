# laneway

A terminal board for Jira. One board of one project as swim lanes or a list,
the selected issue in a panel on the right. Move cards, change status,
priority, points, assignee, summary and labels, comment and reply, all without leaving the
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
  card_limit: 500    # most cards one view fetches (50–5000)
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
create project board next_view prev_view toggle_mode sort move_sprint assignee_filter mine
clear_filters · panel: status priority points summary labels assign comment reply start_work
linked_issue back image.

Colours: accent dim selection_fg selection_bg selection_idle error mention link
code attachment over_limit drop_fg priority_highest priority_high priority_low
priority_lowest type_bug type_story type_epic type_subtask type_other
highlight.

### Rules

Top-level `rules:` fire on what a board refresh shows changed since the last
refresh of the same view and filters: a new issue, or a status, assignee,
priority, points or summary change, yours included unless `by_me: false`.

```yaml
rules:
  - name: done-bugs
    on: status                  # new status assignee priority points summary; default all
    match:                      # all must hold; globs, case-insensitive, one or a list
      type: Bug
      status: [Done, "Won*"]
      from_status: "In *"
      by_me: false              # skip your own edits (reads the issue's changelog)
      # key, assignee ("none" = unassigned), priority, summary (regexp), not: {…}
    actions:
      - type: log               # appends to ~/.config/laneway/rules.log
        text: "{{.Key}} {{.OldStatus}} → {{.Status}}"   # empty: a default line
      - type: notify            # desktop notification via the terminal (OSC 777:
        title: "{{.Key}} done"  # kitty, Ghostty, WezTerm, foot)
      - type: exec              # argv; the issue as JSON on stdin and LANEWAY_*
        command: [notify-send, "{{.Key}}", "{{.Summary}}"]   # env; 30s timeout
      - type: highlight         # a ● on the card until you open it
        color: "#e0af68"        # optional, else the theme's highlight
```

A rule with `watch:` fires on the changes of its own JQL search instead,
polled every `every:` (default 5m, at least 1m) while laneway runs, whichever
board is open. `laneway rules watch` polls them without the board, printing
what fires; `highlight` is skipped there and `notify` needs a terminal:

```yaml
  - name: mine-moved
    watch: assignee = currentUser() AND updated >= -1d
    every: 2m
    on: status
    actions: [{type: notify}]
  - name: stuck-in-review       # a time trigger: an issue enters the search
    watch: status = "In review" AND NOT status CHANGED AFTER -3d
    every: 1h
    on: new                     # issues already in it at start are the baseline
    actions: [{type: log, text: "{{.Key}} in review 3 days"}]
```

Template fields: Kind Key Summary Type Status Assignee Priority Points Parent
OldStatus OldAssignee OldPriority OldPoints Describe.

A bad rule is skipped and reported on the status line. `laneway rules list`
shows what loaded; `laneway rules test -on status -type Bug -status Done
-from-status "In review"` (`-watch JQL` for a watch rule) says which rules
that change fires and what stopped the rest, without running anything. A matterbox config's `rules:` are ignored.

State (last project, board, view, filters, cached boards) lives in
`~/.config/laneway/state.json`. An existing `~/.config/jiratui` or matterbox
config is picked up as a fallback.

## Keys

`?` shows every key as bound. `:` opens the command palette: every action
of the focused pane, the board's views, quick filters and boards, and the
loaded issues, filtered by every word you type.

Board: `p` project · `b` board · `[` `]` view · `t` lanes/list · `s` sort list ·
`a` assignee · `m` mine · `1-9` quick filters · `0` clear · `/` search ·
`R` roadmap · `P` planning · `C` charts · `T` timer · `W` today's worklogs · `x` mark · `B` edit marked · `H`/`L` move card · `M` to sprint/backlog · `enter` open · `#` go to key ·
`n` new issue · `o` browser · `y`/`Y` copy key/URL · `r` refresh · `tab` panel ·
`q` quit. Cards drag between lanes with the mouse.

Panel: `tab`/`shift+tab` walk the fields (custom ones too), `enter` edits one (dates take `2026-10-01`, `today`, `+3d`, `fri`) · `s` status · `p` priority · `P` points · `e` summary · `l` labels ·
`a` assignee · `w` log work · `T` timer · `c` comment · `R` reply · `L` linked issue · `i` images full
size (← →) · `backspace` previous issue · `S` start work · `o` browser ·
`y`/`Y` copy · `r` refresh · `esc` drop field, close.

## Bulk edit

`x` marks the card under the cursor (marks survive switching views), `B`
changes every marked card: status (each along its own workflow move, no
transition form), priority, assignee, labels (`ui -old` adds ui, removes
old), story points, or sprint. Cards that fail stay marked with the reason
in the status bar. `esc` clears the marks.

## Time tracking

`w` in the panel logs work: `1h 30m what you did` (also `1.5h`, `45m`,
`2d` of 8h), ending now. `T` starts a timer on the card or panel issue,
shown in the header and kept across restarts; `T` again stops it into the
same input, filled with the time and started when the timer did. `W` lists
what you logged today; `enter` opens the issue.

## Planning

`P` on a scrum board shows the backlog beside a sprint (the first future
one; `[` `]` pick another), each with its card count and points, the sprint
also per assignee. `← →` switch side, `M` or `space` moves a card across,
`K`/`J` rank it up or down. Changes show at once and are written behind;
a failed write reloads both sides.

## Charts

`C` on a scrum board: the active sprint's burndown (points left per day by
resolution date, against the dotted ideal) and, on `tab`, the velocity of
the last 8 closed sprints (points done by the sprint's end over points in
it). Both count an issue's points in the sprint it sits in now; scope
changes during a sprint don't show.

## Roadmap

`R` on the board shows the project's epics on a timeline: open ones and
those done in the last 90 days, in rank order. A bar runs from the epic's
Start date (or Plans' Target start) to its Due date (or Target end); an
epic without them spans its children's sprints, drawn fainter. The bar
fills by points done, else by children done. `← →` scroll, `+ -` zoom
(day to 2 weeks per column), `.` back to today, `space` folds out the
epic's issues, `enter` opens the row's issue, `esc` back to the board.
`H`/`L` move an epic's bar a column, `<`/`>` move its end; the dates are
written to Jira once you pause.

## Images

In kitty or Ghostty, images embedded in an issue's description and comments
are drawn inline in the panel; `i` shows them full size. Inside tmux they
need `set -g allow-passthrough on`. Elsewhere they show as a caption.
`ui.images: off` or `LANEWAY_IMAGES=0` turns them off.

## Build

`make` · `make test` · `make install`. See [CONTRIBUTING.md](CONTRIBUTING.md).

## License

[MIT](LICENSE)
