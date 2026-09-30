# 9. In the browser

**In this chapter:** the same board in a browser tab. Start it, find your
way around, make it look right, and know what it leaves to the terminal.

## Start it

```sh
laneway web                  # http://127.0.0.1:8484, opens your browser
laneway web -demo            # a generated project, no Jira needed
laneway -site club web       # another site from sites:
laneway web -addr 127.0.0.1:9000 -no-open
```

`serve` is an alias. It is the same binary and the same config as the
terminal version: nothing to install, no second login. `ctrl+c` in the
terminal stops it.

| Flag | |
| --- | --- |
| `-addr` | address to listen on, `127.0.0.1:8484` by default |
| `-remote` | allow an address that is not loopback (see Security) |
| `-no-open` | don't open the browser |
| `-demo` | serve a generated project instead of Jira |

`-site` and `-config` are the global flags and go before `web`.

## The layout

A header with the views, the issue panel on the right.

| View | Key | |
| --- | --- | --- |
| Board | `g b` | lanes or list, filters, drag and drop |
| Planning | `g p` | backlog and sprints, start and complete |
| Reports | `g r` | burndown, burnup, cumulative flow, velocity, cycle time, retro, releases |
| Roadmap | `g m` | epics on a timeline |
| My work | `g w` | your issues, worklogs of a day or week |
| Inbox | `g i` | threads on issues others changed |
| Standup | `g s` | yours or the team's |
| Settings | `g ,` | appearance, board defaults |

`enter` on a card opens the panel: details, comments, history. It works
like the terminal's: `e` edits the description, `c` comments, `r` replies,
`[` `]` step through the list you came from. Editing is the same too:
`s` status, `a` assignee, `p` priority, `P` points, `H` `L` move a card a
lane over, `x` marks, `X` edits the marked ones, `c` creates. The timer is
`t`, `w` logs work.

> **Try it:** `laneway web -demo`, then `g p`, `j` to a card, `m` to send it
> to a sprint.

## Keyboard

Everything has a key, the mouse works as well.

- `?` lists the keys of the view you are in, as bound.
- `:` (or `ctrl+k`) is the palette: actions, views, themes, issues. Start
  with `/` to search issues, `g` to jump to a key, `#` for JQL.
- `g` then a letter goes to a view; `g g` jumps to an issue by key.
- `tab` moves focus between the view and the panel. Panel keys apply only
  while the panel has focus; `esc` closes it.
- `j` `k` `h` `l` or the arrows move, `enter` opens, `esc` backs out,
  `f` filters, `r` refreshes, `o` opens the issue in Jira, `y` copies its key.
- `T` cycles the theme.

## Make it look right

`g ,` opens Settings; `/` filters its options.

- **Theme:** system, light, dark, nord, gruvbox, solarized, mono. The
  palette lists them as `Theme: …`.
- **Accent, density** (compact, normal, roomy), **font size**, and
  **motion** (reduced turns animations off).
- **Custom tokens:** CSS variables as JSON, applied with `ctrl+enter`:

  ```json
  {"--bg": "#101010", "--radius": "2px"}
  ```

- **Board:** default mode, card fields, auto refresh, card limit.

These are kept in the browser. Board and sort choices are kept by laneway
per site.

## What differs from the terminal

Not in the browser:

- coding agents and herdr worktrees
- git helpers: branches, commit keys, pull requests
- kitty image rendering
- rules
- the offline queue and the local index; the browser needs the server
  running and Jira reachable
- scripts and custom actions
- remapping keys

Use the terminal for those. Both can run at once; a change made in one
shows in the other on the next refresh.

## Security

- It listens on `127.0.0.1` only. `-addr` with another address is refused
  unless you pass `-remote`.
- There is no login. Whoever reaches the port acts as you on Jira. With
  `-remote`, put it behind something that authenticates, or don't.
- A write (move, edit, comment) must come from the page's own origin;
  requests with another `Origin` are refused, so a website you visit can't
  drive it.

## Tips

- Views draw from the last answer at once and refresh behind it.
- Long lists are virtual: only what you see is drawn, so a 1000-card limit
  is fine.
- Moves and edits show at once and roll back with a message if Jira says no.
- `r` in a view or `Reload data` in the palette drops cached answers.
- `laneway web -demo` is a safe place to learn the keys.

Previous: [Make it yours](08-make-it-yours.md) ·
Back to the [start](README.md)
