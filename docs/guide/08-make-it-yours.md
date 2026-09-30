# 8. Make it yours

**In this chapter:** make laneway work for you. Search all of Jira with
JQL, keep your favourite searches one key away, let rules watch for
changes, and bend the cards, keys and colours to your taste. It ends with
what to check when something looks off.

## JQL: `Q`

`/` only narrows the cards already loaded. `Q` asks Jira itself: type JQL,
`enter`, and the results show as a view like any other.

![JQL completing a status](../screenshots/jql.png)

- It completes fields, functions and keywords as you type, and a field's
  values after an operator. `tab` takes a suggestion, `↑` `↓` choose.
- An empty input offers your past searches.
- `ctrl+s` stars the query: it becomes a view (`★ …`) on every board.
  `ctrl+s` on a starred one unstars it.
- `ctrl+f` saves it as a real Jira filter under a name you type, to share
  or subscribe to in Jira. `ui.saved_filters: on` shows all your starred
  Jira filters as views.

> **Try it:** `Q`, then `assignee = currentUser() AND updated >= -7d`,
> `enter`. Everything of yours that moved this week, on one board. Like it?
> `Q` again, pick it from the past searches, `ctrl+s`.

> **Tip:** views you always want can live in the config too:
>
> ```yaml
> ui:
>   views:
>     - {name: Mine, jql: "assignee = currentUser()"}
>   filters:                      # named / queries, found in the : palette
>     - {name: Stale review, query: "status:review age>3d"}
> ```

## Rules

Rules react to what changed on the board since the last refresh: a new
issue, or a status, assignee, priority, points or summary change. They
live under `rules:` at the top of the config.

A rule that tells you when a bug is done:

```yaml
rules:
  - name: done-bugs
    on: status
    match:
      type: Bug
      status: Done
    actions:
      - type: notify
        title: "{{.Key}} done"
```

What a rule can do:

| Action | Does |
| --- | --- |
| `notify` | a desktop notification (kitty, Ghostty, WezTerm, foot) |
| `highlight` | a ● on the card until you open it |
| `log` | a line in `~/.config/laneway/rules.log` |
| `exec` | runs a command, with the issue as JSON on stdin |
| `transition` | moves the issue along its workflow |
| `comment` | posts a comment |

> **Tip:** `transition` and `comment` write to Jira, so they only fire on
> changes someone else made, never on yours and never on their own writes.

### Test before you trust

Rules can be tried without waiting for a real change:

```sh
laneway rules list                                  # what loaded
laneway rules test -on status -type Bug -status Done  # what would fire, and why not
```

> **Try it:** add the `done-bugs` rule above, then run the `rules test`
> line. It shows `done-bugs` firing, and for every other rule what stopped
> it.

### Watch a search

A rule with `watch:` follows its own JQL instead of the board, polled every
`every:` while laneway runs, whichever board is open:

```yaml
  - name: stuck-in-review
    watch: status = "In review" AND NOT status CHANGED AFTER -3d
    every: 1h
    on: new
    actions: [{type: log, text: "{{.Key}} in review 3 days"}]
```

`laneway rules watch` runs these without the board, printing what fires.

## Keys

Every action can be rebound, to one key or several:

```yaml
ui:
  keys:
    search: ctrl+f
    mine: [m, M]
```

The names are in the [reference](../reference.md#keys). laneway warns when one key does two things on
the same screen, and `?` always shows the keys as you bound them.

## Your own actions

A command of yours can sit in the palette, and on a key:

```yaml
ui:
  actions:
    - {name: copy for the changelog, key: "!", command: [sh, -c, 'jq -r "\(.key) \(.summary)" | wl-copy']}
```

It gets the issue (or the marked cards) as JSON on stdin and
`LANEWAY_KEY` in its environment; its last line shows on the status line.

## Your cards

`ui.card_fields` picks what a card shows and in what order; leave out what
you don't use. Your own Jira fields join by name:

```yaml
ui:
  card_fields: [type, priority, points, assignee, due, age]
  custom_fields: [Team, Test type]
```

Custom fields show on cards and rows and are searchable:
`/"test type":e2e`.

## Settings: `,`

`,` lists every `ui:` option by topic with its value and default, and says
what the selected one does; `/` filters them by name and description. `enter` edits one, or picks from its values when it has
a fixed set (`icons`, `theme`, …): checked, written back to the config file
(your comments kept) and applied at once.

![Every ui: option with its value and default](../screenshots/settings.png)

> **Try it:** `,`, `/panel_width` `enter`, then `enter` again and set it to `40`. The panel resizes
> as you press `enter`.

## Themes

Start from a preset and change what you like:

```yaml
ui:
  theme:
    preset: tokyonight   # or catppuccin, gruvbox
    accent: "#7aa2f7"
    dim: "244"
```

Colours take ANSI numbers (`0`–`255`) or `#rrggbb`. On a font without Nerd
Font glyphs, `ui.icons: plain` draws issue types as letters. `theme: mono`,
or `NO_COLOR` in your environment, uses no colour: reverse video shows the
cursor.

## When something looks off

| You see | Try |
| --- | --- |
| Boxes or `?` for issue types | `ui.icons: plain`, or a Nerd Font |
| No images in tmux | `set -g allow-passthrough on` in `tmux.conf` |
| Your terminal can't select text | `ui.mouse: off` leaves the mouse to it |
| A message flashed by | the palette's `messages` row lists the last ones |
| Sign-in fails after months | the token expired: `laneway setup` again |
| Requests time out on a slow site | raise `jira.timeout` (`20s` by default) |
| A rule doesn't fire | `laneway rules test` says what stopped it |

## Where to next

You've seen it all. From here:

- `?` in any screen for its keys, `:` for anything you can't find.
- The [reference](../reference.md) for every option, search term and key.
- The [releases](https://github.com/cornedor/laneway/releases) for what just landed.

Previous: [From the shell](07-from-the-shell.md) ·
Back to the [start](README.md)
