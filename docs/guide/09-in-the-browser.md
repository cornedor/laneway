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

Two bars on top and the issue panel on the right.

- **App bar:** brand and site (`@` switches with several `sites:`), the
  views, the tray, search. Views that don't fit fold into **More** (`M`
  lists them all with their keys); the open one always stays.
- **Tray:** only what needs you: offline, queued writes (`⇡3`, click to send
  or drop one), agents waiting or working, the running timer.
- **Context bar** (under it): where you are (project, board, sprint) and
  how you look at it (filters, modes). Views without controls have none.
- After `g` a hint lists the keys that can follow.

| View | Key | |
| --- | --- | --- |
| Home | `g h` | your day on one screen, as the terminal's `~`; the start with `ui.home` |
| Board | `g b` | lanes or list, filters, drag and drop |
| Planning | `g p` | backlog and sprints, start and complete |
| Reports | `g r` | burndown, burnup, cumulative flow, velocity, cycle time, retro, releases |
| Roadmap | `g m` | epics on a timeline |
| My work | `g w` | your issues, worklogs of a day or week, proposals from git, `ui.activity` and your calendar |
| Inbox | `g i` | threads on issues others changed, mentions |
| Standup | `g s` | yours or the team's, one card at a time |
| Review | `g R` | pull requests to review (needs `gh` or `glab`; hidden without both) |
| Agents | `g a` | coding agents and worktrees, each agent's live terminal (needs herdr; hidden without it) |
| Rules | `g l` | your rules, a live feed, try a change |
| Settings | `g ,` | every `ui:` option, appearance, keys |

`enter` on a card opens the panel: details, comments, history. It works
like the terminal's: `E` edits the description, `e` the summary, `c`
comments, `R` replies, `N` keeps private notes, `[` `]` step through the
list you came from. Editing is the same too: `s` status, `a` assignee,
`p` priority, `P` points, `H` `L` move a card a lane over, `x` marks, `X`
edits the marked ones. `n` creates an issue, `T` starts or stops the timer,
`w` logs work, `S` starts work on the issue in an agent, `ctrl+a` asks the
LLM about it (`ui.llm`), and your `ui.actions` run on their own keys and
in the palette.

Where the terminal and the browser clash, the terminal keeps its key and
the browser's action moves: timer `t`->`T`, create `c`->`n`, next theme
`T`->`g t`, reply `r`->`R`, description `e`->`E`. `?` shows what applies.

> **Try it:** `laneway web -demo`, then `g p`, `j` to a card, `m` to send it
> to a sprint.

## Keyboard

Everything has a key, the mouse works as well.

- `?` lists the keys of the view you are in, as bound.
- `:` (or `ctrl+k`) is the palette: actions, views, themes, issues. Start
  with `/` to search issues, `g` to jump to a key, `#` for JQL.
- `g` then a letter goes to a view; `g g` jumps to an issue by key or a
  pasted Jira link.
- `tab` moves focus between the view and the panel. Panel keys apply only
  while the panel has focus; `esc` closes it.
- `j` `k` `h` `l` or the arrows move, `enter` opens, `esc` backs out,
  `f` filters, `r` refreshes, `o` opens the issue in Jira, `y` copies its key.
- `g t` cycles the theme.

## Make it look right

`g ,` opens Settings; `/` filters its options.

- **Theme:** system, light, dark, nord, gruvbox, solarized, tokyonight, catppuccin, dracula, onedark, rosepine, kanagawa, monokai (most with light or dark variants), mono. The
  palette lists them as `Theme: …`.
- **Accent, density** (compact, normal, roomy), **font size**, and
  **motion** (reduced turns animations off).
- **Custom tokens:** CSS variables as JSON, applied with `ctrl+enter`:

  ```json
  {"--bg": "#101010", "--radius": "2px"}
  ```

- **Board:** default mode, card fields, auto refresh, card limit.

Theme, accent, density, font size, motion and custom tokens are kept in
the browser and the server's per-site store. The `ui:` options (board
defaults, capacity, `ui.actions`, `ui.keys` and the rest) are the terminal's
own: changing one here writes it to `config.yaml`, comments kept, and the
terminal sees it on its next start.

**Keys:** `enter` on a Keyboard row captures a new key. Remaps are kept per
site; `ui.keys` from the config applies to binds that have a terminal
action.

## Phone and offline

The page is an installable app (PWA): the shell is cached by a service
worker, the API never is. A right-click on a card opens its menu at the
pointer, with submenus for status, assignee, priority and sprint; a long
press on a phone opens it as a sheet. Browser notifications for rules are off until you turn them on in
Settings or with `N` in Rules. Writes that can't reach Jira wait in the
queue (the `⇡` chip) and are retried; the server must still be running.

## What differs from the terminal

- The agent's terminal is the same `herdr agent attach` the terminal's
  panel runs, drawn by xterm.js: on the agents screen it sits beside the
  list and attaches once the cursor rests on an agent. `enter` or a click
  types into it (every key goes to the agent), `ctrl+\` goes back to the
  list, `z` makes it full size, `t` takes input over from another attach
  (herdr's `--takeover`), `ctrl+shift+c` copies the selection, shift+drag
  selects while the program takes the mouse. `ctrl+\` on an issue elsewhere
  opens its agent's terminal. A second window on the same agent takes it
  over; the first says so. The browser keeps `ctrl+w`, `ctrl+t` and
  `ctrl+n` unless laneway runs as an installed app. Font and size: Settings
  › Appearance (JetBrainsMono Nerd Font by default; Symbols Nerd Font
  always follows, so any font shows icons).
- No kitty image rendering; images open in an in-page viewer (`i`).
- The inbox shows the current site only; switch with `@`.
- No local index: the browser needs the server running and Jira reachable,
  apart from the queue above.
- Agents and review need herdr, `gh` or `glab` on the server's machine, as
  in the terminal.

Both can run at once; a change made in one shows in the other on the next
refresh.

## Security

- It listens on `127.0.0.1` only. `-addr` with another address is refused
  unless you pass `-remote`.
- There is no login. Whoever reaches the port acts as you on Jira.
- It is also a shell: `ui.actions` and `ui.llm` run commands on the
  machine, and so can whoever drives the page. Never expose it publicly.
- The agents' terminal is shell access: typing into a coding agent is
  running commands as you. It attaches to herdr agent panes only, never an
  arbitrary shell. The WebSocket upgrade passes the same checks as the API
  and must carry the page's own `Origin`; `-remote` needs the token cookie
  too, `-demo` has no terminal. A terminal closes after 30 minutes with
  nothing either way and reconnects after 8 hours (access checked again);
  closing it detaches, the agent keeps running.
- The `Host` header must be a loopback name (or one the server was told to
  allow), so a DNS-rebound page can't reach it. A write, and any `/api/`
  request with an `Origin`, must come from the page's own origin;
  cross-site requests are refused.
- `-remote` also needs the launch token: open the URL with `?token=`, which
  sets a cookie; requests without it get 401. Use it on a network you
  trust, or behind a tunnel that authenticates.

## Tips

- Views draw from the last answer at once and refresh behind it.
- Long lists are virtual: only what you see is drawn, so a 1000-card limit
  is fine.
- Moves and edits show at once and roll back with a message if Jira says no.
- `r` in a view or `Reload data` in the palette drops cached answers.
- `laneway web -demo` is a safe place to learn the keys.

Previous: [Make it yours](08-make-it-yours.md) ·
Back to the [start](README.md)
